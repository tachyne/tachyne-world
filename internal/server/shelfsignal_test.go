package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestShelfSignalReadFromBehind: ShelfBlock.getAnalogOutputSignal answers
// only a comparator reading it from behind (direction == FACING's opposite);
// from the front or a side it gives nothing.
func TestShelfSignalReadFromBehind(t *testing.T) {
	h := newHub(world.New(1))
	pos := simPos{blockPos: blockPos{3, 180, 3}}
	shelf := worldgen.BlockBase("oak_shelf")
	info, _ := worldgen.InfoForState(shelf)
	shelf = worldgen.SetProperty(info, shelf, "facing", "north")
	h.world.SetBlock(pos.x, pos.y, pos.z, shelf)
	sh := h.woodShelves[pos]
	if sh == nil {
		sh = &[3]invStack{}
		h.woodShelves[pos] = sh
	}
	sh[0] = invStack{item: int32(itemByName["stick"]), count: 1}
	if got := h.analogSignalFrom(pos, 0, 1); got != 1 { // the comparator south of it: behind
		t.Fatalf("read from behind: %d, want 1", got)
	}
	if got := h.analogSignalFrom(pos, 0, -1); got != 0 { // north: in front
		t.Fatalf("read from the front: %d, want 0", got)
	}
	if got := h.analogSignalFrom(pos, 1, 0); got != 0 {
		t.Fatalf("read from the side: %d, want 0", got)
	}
}
