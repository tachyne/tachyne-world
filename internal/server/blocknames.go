package server

import (
	"strings"
	"sync"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Named block entities. A chest, barrel, shulker box, furnace, hopper,
// dispenser, dropper, brewing stand, enchanting table, beacon, copper chest,
// banner, head or copper golem statue placed from a renamed item keeps the
// name (BlockItem.updateCustomBlockEntityTag → the block entity's
// CustomName). The container shows it as its menu title, and breaking the
// block puts it back on the drop: every one of these blocks' loot tables
// copies custom_name (copy_components), however the block was broken.

// nameableBlocks are the blocks whose loot tables copy custom_name. Wall
// banners, heads and skulls share their standing form's table.
var nameableBlocks = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range []string{"chest", "trapped_chest", "barrel", "shulker_box",
		"furnace", "blast_furnace", "smoker", "hopper", "dispenser", "dropper",
		"brewing_stand", "enchanting_table", "beacon",
		"player_head", "zombie_head", "creeper_head", "dragon_head", "piglin_head",
		"skeleton_skull", "wither_skeleton_skull"} {
		m[n] = true
	}
	for _, c := range dyeName {
		m[c+"_shulker_box"] = true
		m[c+"_banner"] = true
	}
	for _, w := range []string{"", "exposed_", "weathered_", "oxidized_"} {
		for _, wax := range []string{"", "waxed_"} {
			m[wax+w+"copper_chest"] = true
			m[wax+w+"copper_golem_statue"] = true
		}
	}
	return m
}()

// nameableBlock reports whether a block state keeps a custom name.
func nameableBlock(state uint32) bool {
	name, ok := worldgen.StateName(state)
	return ok && nameableBlocks[strings.Replace(name, "_wall_", "_", 1)]
}

// blockNameStore holds the custom names of placed blocks, keyed by simKey.
type blockNameStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newBlockNameStore() *blockNameStore { return &blockNameStore{m: map[string]string{}} }

func (s *blockNameStore) get(pos simPos) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[simKey(pos)]
}

func (s *blockNameStore) set(pos simPos, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		delete(s.m, simKey(pos))
		return
	}
	s.m[simKey(pos)] = name
}

// take removes and returns a name.
func (s *blockNameStore) take(pos simPos) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := simKey(pos)
	n := s.m[k]
	delete(s.m, k)
	return n
}

func (s *blockNameStore) snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.m))
	for k, v := range s.m {
		out[k] = v
	}
	return out
}

func (s *blockNameStore) restore(m map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]string, len(m))
	for k, v := range m {
		if v != "" {
			s.m[k] = v
		}
	}
}

// nameBlockFromStack records a renamed stack's name on the block it placed.
func (h *hub) nameBlockFromStack(pos simPos, state uint32, s invStack) {
	if s.name != "" && nameableBlock(state) {
		h.blockNames.set(pos, s.name)
	}
}

// holdBlockName runs as a named block is removed: the name leaves the store
// and is held for the drop that follows the removal (spawnBlockDrop).
func (h *hub) holdBlockName(pos simPos, old, now uint32) {
	if old == worldgen.Air || !nameableBlock(old) || sameBlockKind(old, now) {
		return // a placement (nothing was there), or not a named kind of block
	}
	if n := h.blockNames.take(pos); n != "" {
		h.lastNamedPos, h.lastNamedName = pos, n
	}
}

// takeHeldBlockName is the held name for a drop of item at pos, if the drop
// is the named block's own item.
func (h *hub) takeHeldBlockName(pos simPos, item int32) string {
	if h.lastNamedName == "" || h.lastNamedPos != pos {
		return ""
	}
	name, ok := itemNameOf[item]
	if !ok || !nameableBlocks[name] {
		return ""
	}
	n := h.lastNamedName
	h.lastNamedPos, h.lastNamedName = simPos{}, ""
	return n
}

// containerTitle is a menu's title: the block's custom name, else its own.
func (h *hub) containerTitle(pos simPos, fallback string) string {
	if n := h.blockNames.get(pos); n != "" {
		return n
	}
	return fallback
}

// doubleChestTitle is ChestBlock's combined menu title: the name of the
// chest half whose type is "right" (the combiner's first), else the other
// half's, else "Large Chest".
func (h *hub) doubleChestTitle(dim int, a, b blockPos) string {
	w := h.worldFor(dim)
	halves := []blockPos{a, b}
	if w != nil {
		if _, ct := chestFacingType(w.At(b.x, b.y, b.z)); ct == "right" {
			halves = []blockPos{b, a}
		}
	}
	for _, p := range halves {
		if n := h.blockNames.get(simPos{dim: dim, blockPos: p}); n != "" {
			return n
		}
	}
	return "Large Chest"
}
