package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// SegmentableBlock through the real placement path: a second pink petal
// clicked onto the first adds a flower and keeps the first one's facing;
// leaf litter stacks a segment the same way, and a sneaking click leaves it
// as it was (it is never swapped for a fresh litter, since a replaceable
// block does not give way to its own item).
func TestFlowerBedAndLeafLitterStack(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1300, 180, 1300
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.BlockID("grass_block"))

	p.setHotbarSlot(0, itemByName["pink_petals"])
	selectSlot(p, 0)
	p.yaw = 0 // looking south: the bed faces north
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got := w.Block(x, y, z); !isSameBlock(got, worldgen.BlockID("pink_petals")) || propOf(t, got, "flower_amount") != "1" {
		t.Fatalf("first petal: %d", got)
	}
	p.yaw = 90
	s.handlePlace(p, placeBody(x, y, z, 1)) // click the petals themselves
	got := w.Block(x, y, z)
	if propOf(t, got, "flower_amount") != "2" || propOf(t, got, "facing") != "north" {
		t.Fatalf("second petal: amount %s facing %s, want 2 north",
			propOf(t, got, "flower_amount"), propOf(t, got, "facing"))
	}

	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	p.setHotbarSlot(0, itemByName["leaf_litter"])
	p.yaw = 0
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	s.handlePlace(p, placeBody(x, y, z, 1))
	got = w.Block(x, y, z)
	if !isSameBlock(got, worldgen.BlockID("leaf_litter")) || propOf(t, got, "segment_amount") != "2" {
		t.Fatalf("leaf litter clicked with litter: %d, want 2 segments", got)
	}
	p.sneaking, p.yaw = true, 90
	s.handlePlace(p, placeBody(x, y, z, 1))
	if after := w.Block(x, y, z); after != got {
		t.Fatalf("a sneaking click replaced the litter: %d -> %d", got, after)
	}
}
