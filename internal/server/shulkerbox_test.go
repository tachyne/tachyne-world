package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A shulker box will not go inside a shulker box — not by hand, and not
// through a hopper. It is the rule that stops a box holding a base inside a
// base, and the engine had neither half of it.
func TestShulkerBoxRefusesToNest(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	x, y, z := 6, 70, 6
	h.world.SetBlock(x, y, z, worldgen.BlockBase("shulker_box")+1) // facing up, not waterlogged
	h.openChest(pl, x, y, z)
	if pl.winKind != winChest {
		t.Fatal("the box should have opened")
	}

	box := int32(itemByName["shulker_box"])
	pl.cursor = invStack{item: box, count: 1}
	_ = players
	h.handleClick(players, evClick{eid: pl.p.eid, windowID: int32(pl.winID), slot: 0,
		changed: []slotChange{{slot: 0, st: invStack{item: box, count: 1}}}})
	if c := h.chests[simPos{dim: pl.dim, blockPos: blockPos{x, y, z}}]; c != nil && c.slots[0].item == box {
		t.Fatal("a shulker box must not go inside a shulker box")
	}

	// …and the hopper path refuses too.
	if h.insertByFace(simPos{dim: pl.dim, blockPos: blockPos{x, y, z}}, -1, invStack{item: box, count: 1}) {
		t.Fatal("a hopper must not post a shulker box into a shulker box")
	}
	// Anything else still goes in.
	if !h.insertByFace(simPos{dim: pl.dim, blockPos: blockPos{x, y, z}}, -1, invStack{item: int32(itemByName["stone"]), count: 1}) {
		t.Fatal("an ordinary item should still go in")
	}
}
