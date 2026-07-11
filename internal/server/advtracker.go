package server

// The advancement criteria tracker core: pure state + evaluation, no hub
// wiring (advancement_hooks.go plugs the engine's gameplay events in). Grant
// state is per player: advID → criterion name → obtained unix millis.

import (
	"sort"
	"time"

	attach "github.com/tachyne/tachyne-common/attach"

	"tachyne/internal/worldgen"
)

// advByID and advByTrigger index the generated table once at init.
var advByID = func() map[string]*advNode {
	m := make(map[string]*advNode, len(advTable))
	for i := range advTable {
		m[advTable[i].id] = &advTable[i]
	}
	return m
}()

type advRef struct {
	node *advNode
	crit *advCriterion
}

var advByTrigger = func() map[string][]advRef {
	m := map[string][]advRef{}
	for i := range advTable {
		n := &advTable[i]
		for j := range n.criteria {
			c := &n.criteria[j]
			if c.unmatchable {
				continue
			}
			m[c.trigger] = append(m[c.trigger], advRef{n, c})
		}
	}
	return m
}()

// advBlockRanges resolves placed_block criteria block names to state-id ranges
// once. A name the block table doesn't know drops the criterion (rather than
// panicking in BlockRange at match time).
var advBlockRanges = func() map[*advCriterion][2]uint32 {
	m := map[*advCriterion][2]uint32{}
	for _, ref := range advByTrigger["placed_block"] {
		func() {
			defer func() { recover() }()
			lo, hi := worldgen.BlockRange(ref.crit.block)
			m[ref.crit] = [2]uint32{lo, hi}
		}()
	}
	return m
}()

// advTreeFrame is the static MsgAdvTree payload, built once — the tree is the
// same for every player and every join.
var advTreeFrame = func() attach.AdvTree {
	t := attach.AdvTree{Nodes: make([]attach.AdvNode, 0, len(advTable))}
	for i := range advTable {
		n := &advTable[i]
		an := attach.AdvNode{ID: n.id, Parent: n.parent, Reqs: n.reqs}
		if d := n.display; d != nil {
			an.HasDisplay = true
			an.Title = d.title
			an.Desc = d.desc
			an.Icon = attach.ItemStack{ID: d.icon, Count: 1}
			an.Frame = int32(d.frame)
			an.Background = d.background
			an.ShowToast = d.showToast
			an.Announce = d.announceChat
			an.Hidden = d.hidden
			an.X = d.x
			an.Y = d.y
		}
		t.Nodes = append(t.Nodes, an)
	}
	return t
}()

// advState is one player's grant state.
type advState map[string]map[string]int64

// done reports whether the advancement's requirements are satisfied
// (OR-of-ANDs over obtained criteria — vanilla AdvancementRequirements.test).
func (s advState) done(n *advNode) bool {
	if len(n.reqs) == 0 {
		return false
	}
	obtained := s[n.id]
	for _, group := range n.reqs {
		hit := false
		for _, c := range group {
			if _, ok := obtained[c]; ok {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// grant marks one criterion obtained. Returns (criterionWasNew,
// advancementJustCompleted).
func (s advState) grant(n *advNode, crit string) (bool, bool) {
	m := s[n.id]
	if m == nil {
		m = map[string]int64{}
		s[n.id] = m
	}
	if _, ok := m[crit]; ok {
		return false, false
	}
	wasDone := s.done(n)
	m[crit] = time.Now().UnixMilli()
	return true, !wasDone && s.done(n)
}

// snapshot renders the player's full progress (the join-time Reset frame).
// Ordered for deterministic frames.
func (s advState) snapshot() attach.AdvProgress {
	p := attach.AdvProgress{Reset: true}
	ids := make([]string, 0, len(s))
	for id := range s {
		if _, ok := advByID[id]; !ok {
			continue // advancement gone from the table (data upgrade)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p.Entries = append(p.Entries, attach.AdvProgressEntry{ID: id, Done: s[id]})
	}
	return p
}

// advMatch tests one criterion against a trigger payload. Fields unused by a
// trigger stay zero — see the distillation rules in gen_advancements.py.
type advMatch struct {
	entity     string  // species/registry name for entity triggers
	item       int32   // item id for consume_item
	inv        []int32 // full inventory item-id set for inventory_changed
	blockState uint32  // placed block state (matched against advBlockRanges)
	biome      string  // location biome name
	dim        int32   // changed_dimension destination
}

func (m advMatch) criterion(c *advCriterion) bool {
	switch c.trigger {
	case "inventory_changed":
		for _, pred := range c.items {
			if !containsAny(m.inv, pred) {
				return false
			}
		}
		return true
	case "consume_item":
		if len(c.items) == 0 {
			return true
		}
		return containsID(c.items[0], m.item)
	case "player_killed_entity", "entity_killed_player", "bred_animals", "tame_animal":
		return c.entity == "" || c.entity == m.entity
	case "placed_block":
		r, ok := advBlockRanges[c]
		return ok && m.blockState >= r[0] && m.blockState <= r[1]
	case "changed_dimension":
		return !c.hasDim || c.dim == m.dim
	case "location":
		return c.biome == m.biome
	case "slept_in_bed", "villager_trade", "enchanted_item", "brewed_potion",
		"cured_zombie_villager":
		return true
	}
	return false
}

func containsID(set []int32, id int32) bool {
	for _, v := range set {
		if v == id {
			return true
		}
	}
	return false
}

func containsAny(have []int32, want []int32) bool {
	for _, h := range have {
		if containsID(want, h) {
			return true
		}
	}
	return false
}
