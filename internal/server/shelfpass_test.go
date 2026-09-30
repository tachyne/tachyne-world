package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ShelfBlock.useItemOn: on an unpowered shelf an empty hand on an empty
// slot swaps nothing and PASSes; a filled slot, a held item or a powered
// shelf takes the click.
func TestEmptyHandOnAnEmptyShelfSlotPasses(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1370, 180, 1370
	clearAirBox(w, x, y, z, 3)
	shelf := withProps(t, worldgen.BlockBase("oak_shelf"), map[string]string{"facing": "south", "powered": "false", "waterlogged": "false"})
	w.SetBlock(x, y, z, shelf)
	pos := simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}
	selectSlot(p, 0)
	p.setHotbarSlot(0, 0)
	use := func() bool { return s.tryUseBlock(p, false, x, y, z, 0, 3, 0.5, 0.5, 1) } // the south face, middle slot

	if use() {
		t.Fatal("an empty hand on an empty shelf slot was taken; vanilla PASSes")
	}
	h.shelfView.set(pos, shelfView{Items: [3]shelfSlot{1: {Name: "minecraft:stick", Count: 1}}})
	if !use() {
		t.Fatal("an empty hand on a filled slot should take the item")
	}
	h.shelfView.set(pos, shelfView{})
	p.setHotbarSlot(0, itemByName["stick"])
	if !use() {
		t.Fatal("a held item on an empty slot should be put on the shelf")
	}
	p.setHotbarSlot(0, 0)
	w.SetBlock(x, y, z, withProps(t, shelf, map[string]string{"powered": "true"}))
	if !use() {
		t.Fatal("a powered shelf swaps with the hotbar: the click is taken")
	}
}
