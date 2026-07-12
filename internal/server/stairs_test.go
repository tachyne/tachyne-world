package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestStairCorners drives vanilla getStairsShape: perpendicular same-half
// stairs form outer corners on the high side and inner corners on the low
// side; a parallel same-facing stair vetoes the corner (canTakeShape).
func TestStairCorners(t *testing.T) {
	def := worldgen.BlockID("oak_stairs") // facing north, half bottom, straight
	info, ok := stairInfo(def)
	if !ok {
		t.Fatal("oak_stairs not classified as a stair")
	}
	east := worldgen.SetProperty(info, def, "facing", "east")
	north := def
	w := world.New(1)

	shapeOf := func(st uint32) string { return worldgen.GetProperty(info, st, "shape") }

	// lone stair stays straight
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "straight" {
		t.Fatalf("lone stair shape %s", s)
	}

	// east-facing stair on the high side (north) → outer_right
	w.SetBlock(0, 200, -1, east)
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "outer_right" {
		t.Fatalf("outer corner shape %s, want outer_right", s)
	}
	// west-facing stair there instead → outer_left
	w.SetBlock(0, 200, -1, worldgen.SetProperty(info, def, "facing", "west"))
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "outer_left" {
		t.Fatalf("outer corner shape %s, want outer_left", s)
	}
	w.SetBlock(0, 200, -1, worldgen.Air)

	// east-facing stair on the low side (south) → inner_right
	w.SetBlock(0, 200, 1, east)
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "inner_right" {
		t.Fatalf("inner corner shape %s, want inner_right", s)
	}

	// a parallel north-facing stair on the veto side keeps it straight
	w.SetBlock(1, 200, 0, north) // canTakeShape side for an east-facing low-side stair
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "straight" {
		t.Fatalf("vetoed corner shape %s, want straight", s)
	}
	w.SetBlock(1, 200, 0, worldgen.Air)

	// different half never corners
	w.SetBlock(0, 200, 1, worldgen.SetProperty(info, east, "half", "top"))
	if s := shapeOf(stairShape(w, 0, 200, 0, info, north)); s != "straight" {
		t.Fatalf("cross-half corner shape %s, want straight", s)
	}
}
