package server

import (
	_ "embed"
	"encoding/json"
	"sort"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Data-driven BLOCK loot: the vanilla loot tables baked into a compact IR
// (scripts/gen_blockloot.py → lootdata/blocks.json) and evaluated here, so a
// broken block yields exactly what vanilla drops — silk-touch alternatives,
// Fortune formulas, per-species leaf saplings, explosion decay. Blocks whose
// table used an unsupported feature (5 of 1083) aren't baked; the caller
// falls back to the legacy rollDrops path for those.

//go:embed lootdata/blocks.json
var blockLootJSON []byte

type lootNP struct {
	T   string  `json:"t"`
	V   float64 `json:"v"`
	Min *lootNP `json:"min"`
	Max *lootNP `json:"max"`
	N   *lootNP `json:"n"`
	P   *lootNP `json:"p"`
}

type lootCond struct {
	C       string            `json:"c"`
	P       float64           `json:"p"`
	Ench    string            `json:"ench"`
	Chances []float64         `json:"chances"`
	Props   map[string]string `json:"props"`
	Silk    bool              `json:"silk"`
	Item    string            `json:"item"`
	Tag     string            `json:"tag"`
	Term    *lootCond         `json:"term"`
	Terms   []lootCond        `json:"terms"`
}

type lootFn struct {
	F       string  `json:"f"`
	NP      *lootNP `json:"np"`
	Add     bool    `json:"add"`
	Min     *int    `json:"min"`
	Max     *int    `json:"max"`
	Ench    string  `json:"ench"`
	Formula string  `json:"formula"`
	Mult    int     `json:"mult"`
	Extra   int     `json:"extra"`
	Prob    float64 `json:"prob"`
}

type lootEntry struct {
	Type       string      `json:"type"`
	ID         int32       `json:"id"`
	Conditions []lootCond  `json:"conditions"`
	Functions  []lootFn    `json:"functions"`
	Children   []lootEntry `json:"children"`
}

type lootPool struct {
	Rolls      lootNP      `json:"rolls"`
	Conditions []lootCond  `json:"conditions"`
	Functions  []lootFn    `json:"functions"`
	Entries    []lootEntry `json:"entries"`
}

type lootTable struct {
	Pools []lootPool `json:"pools"`
}

type lootRow struct {
	Lo    uint32    `json:"lo"`
	Hi    uint32    `json:"hi"`
	Table lootTable `json:"table"`
}

var blockLoot []lootRow

func init() {
	if err := json.Unmarshal(blockLootJSON, &blockLoot); err != nil {
		panic("blockloot: " + err.Error())
	}
}

// lootFor finds the baked table for a block state, or nil.
func lootFor(state uint32) *lootTable {
	i := sort.Search(len(blockLoot), func(i int) bool { return blockLoot[i].Hi >= state })
	if i < len(blockLoot) && blockLoot[i].Lo <= state && state <= blockLoot[i].Hi {
		return &blockLoot[i].Table
	}
	return nil
}

// lootCtx is the evaluation context for one break.
type lootCtx struct {
	state     uint32
	tool      int32 // tool item id (0 = bare hand)
	silk      bool
	fortune   int
	explosion float64 // 0 = not an explosion (no decay/survives roll)
	rng       func(int) int
	randf     func() float64
}

// evalBlockLoot rolls the baked table; returns nil if the block has none.
func (h *hub) evalBlockLoot(ctx lootCtx) []drop {
	tbl := lootFor(ctx.state)
	if tbl == nil {
		return nil
	}
	var out []drop
	for pi := range tbl.Pools {
		p := &tbl.Pools[pi]
		if !ctx.condsPass(p.Conditions) {
			continue
		}
		rolls := int(ctx.np(&p.Rolls))
		for r := 0; r < rolls; r++ {
			e := ctx.pick(p.Entries)
			if e == nil {
				continue
			}
			it, count, ok := ctx.emit(e, p.Functions)
			if ok && it != 0 && count > 0 {
				out = append(out, drop{item: it, count: count})
			}
		}
	}
	return out
}

// pick chooses one entry from a pool by weight (block/mob luck=0 → raw weight
// 1 each), skipping entries whose conditions fail; composite entries resolve
// to their first passing leaf.
func (c *lootCtx) pick(entries []lootEntry) *lootEntry {
	var eligible []*lootEntry
	for i := range entries {
		if leaf := c.resolve(&entries[i]); leaf != nil {
			eligible = append(eligible, leaf)
		}
	}
	if len(eligible) == 0 {
		return nil
	}
	return eligible[c.rng(len(eligible))] // all weights are 1 for block loot
}

// resolve turns an entry into a single droppable leaf (or nil): item entries
// pass through their conditions; alternatives return the first passing child.
func (c *lootCtx) resolve(e *lootEntry) *lootEntry {
	if !c.condsPass(e.Conditions) {
		return nil
	}
	switch e.Type {
	case "item":
		return e
	case "alt", "alternatives", "sequence":
		for i := range e.Children {
			if leaf := c.resolve(&e.Children[i]); leaf != nil {
				return leaf
			}
		}
	case "group":
		for i := range e.Children {
			if leaf := c.resolve(&e.Children[i]); leaf != nil {
				return leaf
			}
		}
	}
	return nil
}

// emit applies the pool + entry functions to produce (item, count).
func (c *lootCtx) emit(e *lootEntry, poolFns []lootFn) (int32, int, bool) {
	count := 1
	apply := func(fns []lootFn) {
		for i := range fns {
			count = c.applyFn(&fns[i], count)
		}
	}
	apply(e.Functions)
	apply(poolFns)
	return e.ID, count, true
}

func (c *lootCtx) applyFn(f *lootFn, count int) int {
	switch f.F {
	case "set_count":
		n := int(c.np(f.NP))
		if f.Add {
			return count + n
		}
		return n
	case "bonus":
		if f.Ench != "fortune" || c.fortune <= 0 {
			return count
		}
		switch f.Formula {
		case "ore_drops": // count * max(1, rand(fortune+2))
			b := c.rng(c.fortune+2) - 1
			if b < 0 {
				b = 0
			}
			return count * (b + 1)
		case "uniform_bonus_count":
			return count + c.rng(f.Mult*c.fortune+1)
		case "binomial_with_bonus_count":
			for i := 0; i < c.fortune+f.Extra; i++ {
				if c.randf() < f.Prob {
					count++
				}
			}
			return count
		}
	case "limit":
		if f.Min != nil && count < *f.Min {
			count = *f.Min
		}
		if f.Max != nil && count > *f.Max {
			count = *f.Max
		}
	case "explosion_decay":
		if c.explosion > 0 {
			kept := 0
			for i := 0; i < count; i++ {
				if c.randf() <= 1.0/c.explosion {
					kept++
				}
			}
			return kept
		}
	}
	return count
}

func (c *lootCtx) condsPass(cs []lootCond) bool {
	for i := range cs {
		if !c.cond(&cs[i]) {
			return false
		}
	}
	return true
}

func (c *lootCtx) cond(cd *lootCond) bool {
	switch cd.C {
	case "survives":
		if c.explosion > 0 {
			return c.randf() <= 1.0/c.explosion
		}
		return true
	case "chance":
		return c.randf() < cd.P
	case "table_bonus":
		lvl := 0
		if cd.Ench == "fortune" {
			lvl = c.fortune
		}
		if lvl >= len(cd.Chances) {
			lvl = len(cd.Chances) - 1
		}
		return c.randf() < cd.Chances[lvl]
	case "state":
		info, ok := worldgen.InfoForState(c.state)
		if !ok {
			return false
		}
		for k, v := range cd.Props {
			if worldgen.GetProperty(info, c.state, k) != v {
				return false
			}
		}
		return true
	case "tool":
		if cd.Silk {
			return c.silk
		}
		if cd.Item != "" {
			return c.tool == int32(itemByName[cd.Item])
		}
		if cd.Tag != "" { // v1: shears is the only tool tag block loot references directly
			return c.tool == int32(itemByName["shears"]) && cd.Tag == "shears"
		}
		return false
	case "not":
		return !c.cond(cd.Term)
	case "any":
		for i := range cd.Terms {
			if c.cond(&cd.Terms[i]) {
				return true
			}
		}
		return false
	case "all":
		for i := range cd.Terms {
			if !c.cond(&cd.Terms[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// np samples a number provider.
func (c *lootCtx) np(n *lootNP) float64 {
	if n == nil {
		return 0
	}
	switch n.T {
	case "const":
		return n.V
	case "uniform":
		lo, hi := int(c.np(n.Min)), int(c.np(n.Max))
		if hi < lo {
			return float64(lo)
		}
		return float64(lo + c.rng(hi-lo+1)) // inclusive
	case "binomial":
		trials, p := int(c.np(n.N)), c.np(n.P)
		s := 0
		for i := 0; i < trials; i++ {
			if c.randf() < p {
				s++
			}
		}
		return float64(s)
	}
	return 0
}
