package server

import (
	_ "embed"
	"encoding/json"
	"math"
	"math/rand"
)

// Data-driven CHEST loot — the vanilla chests/*.json tables (baked by
// scripts/gen_chestloot.py into lootdata/chests.json) evaluated when a
// structure chest is first opened. This shares the loot IR + condition/number
// helpers with blockloot.go but runs its OWN weighted, empty-aware, enchant-
// aware selection so the block/entity evaluator is untouched. Each chest's
// contents are seed-deterministic (world seed + position + table name), so a
// given chest always holds the same loot — vanilla's lootTableSeed semantics
// with no extra persistence.

//go:embed lootdata/chests.json
var chestLootJSON []byte

var chestLoot map[string]lootTable

func init() {
	if err := json.Unmarshal(chestLootJSON, &chestLoot); err != nil {
		panic("chestloot: " + err.Error())
	}
}

// lootForChest finds a baked chest table by name ("chests/simple_dungeon").
func lootForChest(name string) (*lootTable, bool) {
	if t, ok := chestLoot[name]; ok {
		return &t, true
	}
	return nil, false
}

// fillChest rolls a table into a chest's 27 slots, scattered the way vanilla's
// LootTable.fill does. Deterministic per (world seed, position, table name).
func (h *hub) fillChest(c *chest, name string, pos blockPos) {
	tbl, ok := lootForChest(name)
	if !ok {
		return
	}
	r := rand.New(rand.NewSource(chestSeed(h.world.Seed(), pos, name)))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
	stacks := h.evalChestStacks(tbl, ctx, 0)

	// Split oversized stacks to their per-item cap (vanilla createStackSplitter).
	var items []invStack
	for _, s := range stacks {
		cap := stackCap(s.item)
		if cap < 1 {
			cap = 1
		}
		for s.count > cap {
			part := s
			part.count = cap
			items = append(items, part)
			s.count -= cap
		}
		if s.count > 0 {
			items = append(items, s)
		}
	}

	free := make([]int, len(c.slots))
	for i := range free {
		free[i] = i
	}
	items = shuffleSplitItems(items, len(free), r)
	for i := len(free) - 1; i > 0; i-- { // shuffle the target slots
		j := r.Intn(i + 1)
		free[i], free[j] = free[j], free[i]
	}
	for _, s := range items {
		if len(free) == 0 {
			break
		}
		slot := free[len(free)-1]
		free = free[:len(free)-1]
		c.slots[slot] = s
	}
}

// evalChestStacks rolls every pool of a chest table into a flat stack list.
func (h *hub) evalChestStacks(tbl *lootTable, ctx *lootCtx, depth int) []invStack {
	var out []invStack
	if depth > 8 { // nested-ref cycle guard
		return out
	}
	for pi := range tbl.Pools {
		p := &tbl.Pools[pi]
		if !ctx.condsPass(p.Conditions) {
			continue
		}
		rolls := int(ctx.np(&p.Rolls))
		if p.Bonus != nil {
			rolls += int(ctx.npFloat(p.Bonus) * ctx.luck)
		}
		for r := 0; r < rolls; r++ {
			e := ctx.pickWeighted(p.Entries)
			if e == nil {
				continue
			}
			out = append(out, h.emitChestEntry(e, p.Functions, ctx, depth)...)
		}
	}
	return out
}

// pickWeighted expands the pool's entries into pickable leaves (items, empties,
// nested refs) and draws one by vanilla weight = max(0, floor(w + q·luck)).
func (c *lootCtx) pickWeighted(entries []lootEntry) *lootEntry {
	var leaves []*lootEntry
	for i := range entries {
		c.collectLeaves(&entries[i], &leaves)
	}
	total := 0
	for _, e := range leaves {
		total += entryWeight(e, c.luck)
	}
	if total <= 0 {
		return nil
	}
	r := c.rng(total)
	for _, e := range leaves {
		r -= entryWeight(e, c.luck)
		if r < 0 {
			return e
		}
	}
	return leaves[len(leaves)-1]
}

func entryWeight(e *lootEntry, luck float64) int {
	w := e.W
	if w == 0 {
		w = 1
	}
	if v := w + int(math.Floor(float64(e.Q)*luck)); v > 0 {
		return v
	}
	return 0
}

// collectLeaves appends the pickable leaves of one entry whose conditions pass.
func (c *lootCtx) collectLeaves(e *lootEntry, out *[]*lootEntry) {
	if !c.condsPass(e.Conditions) {
		return
	}
	switch e.Type {
	case "item", "empty", "ref":
		*out = append(*out, e)
	case "alternatives", "sequence":
		for i := range e.Children { // first child whose conditions pass
			if c.condsPass(e.Children[i].Conditions) {
				c.collectLeaves(&e.Children[i], out)
				return
			}
		}
	case "group":
		for i := range e.Children {
			c.collectLeaves(&e.Children[i], out)
		}
	}
}

// emitChestEntry turns a chosen leaf into its stacks: empties yield nothing,
// refs roll the nested table, items apply their + the pool's functions.
func (h *hub) emitChestEntry(e *lootEntry, poolFns []lootFn, ctx *lootCtx, depth int) []invStack {
	switch e.Type {
	case "empty":
		return nil
	case "ref":
		if sub, ok := lootForChest(e.Ref); ok {
			return h.evalChestStacks(sub, ctx, depth+1)
		}
		return nil
	}
	st := invStack{item: e.ID, count: 1}
	for i := range e.Functions {
		st = ctx.applyChestFn(h, &e.Functions[i], st)
	}
	for i := range poolFns {
		st = ctx.applyChestFn(h, &poolFns[i], st)
	}
	if st.item == 0 || st.count <= 0 {
		return nil
	}
	return []invStack{st}
}

// applyChestFn applies one loot function to a chest stack.
func (c *lootCtx) applyChestFn(h *hub, f *lootFn, st invStack) invStack {
	switch f.F {
	case "set_count":
		n := int(c.np(f.NP))
		if f.Add {
			st.count += n
		} else {
			st.count = n
		}
	case "set_damage":
		if maxd, ok := itemMaxDurability[st.item]; ok {
			remain := clamp01(c.npFloat(f.NP)) // fraction of durability to LEAVE
			st.dmg = int(math.Floor((1 - remain) * float64(maxd)))
		}
	case "ench_random":
		if e := h.chestEnchRandom(c.rng, st.item); e != ([2]enchApply{}) {
			if st.item == itemBook {
				st.item = itemEnchantedBook
			}
			st.ench = e
		}
	case "ench_levels":
		if e := h.chestEnchLevels(c.rng, st.item, int(c.np(f.NP))); e != ([2]enchApply{}) {
			if st.item == itemBook {
				st.item = itemEnchantedBook
			}
			st.ench = e
		}
	}
	return st
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// npFloat samples a number provider WITHOUT the integer truncation np() applies
// — set_damage's uniform ranges are fractional (0.0..0.9).
func (c *lootCtx) npFloat(n *lootNP) float64 {
	if n == nil {
		return 0
	}
	switch n.T {
	case "const":
		return n.V
	case "uniform":
		lo, hi := c.npFloat(n.Min), c.npFloat(n.Max)
		if hi < lo {
			return lo
		}
		return lo + c.randf()*(hi-lo)
	}
	return c.np(n) // binomial etc. keep the integer path
}

// chestEnchPool is the engine's simplified candidate enchantment set for an
// item — a stand-in for vanilla's per-enchantment supported-item tables, sized
// to the [2]enchApply stack cap.
func chestEnchPool(item int32) []int8 {
	switch {
	case item == itemBook:
		return []int8{enchSharpness, enchEfficiency, enchProtection, enchUnbreaking,
			enchFortune, enchLooting, enchPower, enchLure, enchLuckOfTheSea, enchMending}
	case item == itemBow:
		return []int8{enchPower, enchPunch, enchFlame, enchInfinity, enchUnbreaking}
	case item == itemFishingRod:
		return []int8{enchLure, enchLuckOfTheSea, enchUnbreaking, enchMending}
	}
	if _, isSword := meleeDamage[item]; isSword {
		if swordPeriod[item] {
			return []int8{enchSharpness, enchLooting, enchUnbreaking, enchMending}
		}
		return []int8{enchEfficiency, enchFortune, enchSilkTouch, enchUnbreaking}
	}
	if _, isArmor := armorInfo[item]; isArmor {
		return []int8{enchProtection, enchUnbreaking, enchMending}
	}
	if _, durable := itemMaxDurability[item]; durable {
		return []int8{enchEfficiency, enchUnbreaking, enchMending}
	}
	return nil
}

// chestEnchRandom applies one uniformly-chosen enchantment at a random valid
// level (vanilla enchant_randomly, single enchant).
func (h *hub) chestEnchRandom(rng func(int) int, item int32) [2]enchApply {
	pool := chestEnchPool(item)
	if len(pool) == 0 {
		return [2]enchApply{}
	}
	id := pool[rng(len(pool))]
	return [2]enchApply{{id: id, lvl: int8(1 + rng(int(enchMaxLvl(id))))}}
}

// chestEnchLevels approximates vanilla enchant_with_levels: a primary enchant
// scaled toward its cap by the level cost (the rollEnchOptions idiom), and a
// second, distinct enchant when rng(50) ≤ cost — capped at 2 by the stack.
func (h *hub) chestEnchLevels(rng func(int) int, item int32, cost int) [2]enchApply {
	pool := chestEnchPool(item)
	if len(pool) == 0 {
		return [2]enchApply{}
	}
	pick := func(c int) enchApply {
		id := pool[rng(len(pool))]
		maxl := int(enchMaxLvl(id))
		lvl := 1 + c*(maxl-1)/30
		if lvl < 1 {
			lvl = 1
		}
		if lvl > maxl {
			lvl = maxl
		}
		return enchApply{id: id, lvl: int8(lvl)}
	}
	out := [2]enchApply{pick(cost)}
	if len(pool) > 1 && rng(50) <= cost {
		for i := 0; i < 5; i++ {
			if e := pick(cost / 2); e.id != out[0].id {
				out[1] = e
				break
			}
		}
	}
	return out
}

// shuffleSplitItems scatters and fragments the rolled stacks to fill more of
// the available slots, matching vanilla LootTable.shuffleAndSplitItems shape.
func shuffleSplitItems(items []invStack, freeSlots int, r *rand.Rand) []invStack {
	var single, split []invStack
	for _, s := range items {
		if s.count > 1 {
			split = append(split, s)
		} else if s.count == 1 {
			single = append(single, s)
		}
	}
	for len(single)+len(split) < freeSlots && len(split) > 0 {
		idx := r.Intn(len(split))
		s := split[idx]
		split = append(split[:idx], split[idx+1:]...)
		half := 1 + r.Intn(s.count/2)
		a, b := s, s
		a.count, b.count = half, s.count-half
		for _, part := range [2]invStack{a, b} {
			if part.count > 1 && len(single)+len(split) < freeSlots {
				split = append(split, part)
			} else {
				single = append(single, part)
			}
		}
	}
	single = append(single, split...)
	for i := len(single) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		single[i], single[j] = single[j], single[i]
	}
	return single
}

// chestSeed mixes the world seed, chest position and table name into a stable
// per-chest RNG seed (vanilla lootTableSeed is fixed at generation time).
func chestSeed(worldSeed int64, pos blockPos, name string) int64 {
	h := uint64(worldSeed) + 0x9e3779b97f4a7c15
	for _, v := range [3]int{pos.x, pos.y, pos.z} {
		h ^= uint64(int64(v)) * 0x9e3779b97f4a7c15
		h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	}
	for _, c := range name {
		h = (h ^ uint64(c)) * 1099511628211
	}
	h ^= h >> 31
	return int64(h)
}
