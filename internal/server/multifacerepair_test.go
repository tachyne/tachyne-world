package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The boot sweep trims a multiface block to the faces something actually
// holds, and removes one with none left. Blocks placed before 2026-09-20 were
// built from a state with all six faces already on, so a patch put on a floor
// was a full cube with its other five sides standing in the air.
func TestMultifaceRepairTrimsUnheldFaces(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	lichen := worldgen.BlockBase("glow_lichen") // the old, all-faces-on default
	info, _ := worldgen.InfoForState(lichen)
	for _, f := range []string{"down", "up", "north", "south", "east", "west"} {
		if worldgen.GetProperty(info, lichen, f) != "true" {
			t.Fatalf("this test is about the all-faces-on state; %s is not set", f)
		}
	}

	x, y, z := 20, 180, 20
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	w.SetBlock(x, y-1, z, worldgen.Stone) // one floor beneath it, nothing else
	w.SetBlock(x, y, z, lichen)

	// And one with nothing at all to hold it.
	lone := blockPos{x + 4, y, z}
	w.SetBlock(lone.x, lone.y, lone.z, worldgen.Air)
	w.SetBlock(lone.x, lone.y, lone.z, lichen)

	h.repairMultiface()

	got := w.At(x, y, z)
	if !isMultiface(got) {
		t.Fatalf("the lichen over the floor should survive, got %d", got)
	}
	gi, _ := worldgen.InfoForState(got)
	if worldgen.GetProperty(gi, got, "down") != "true" {
		t.Error("the face the floor holds should stay")
	}
	for _, f := range []string{"up", "north", "south", "east", "west"} {
		if worldgen.GetProperty(gi, got, f) == "true" {
			t.Errorf("%s has nothing behind it and should have been trimmed", f)
		}
	}
	if got := w.At(lone.x, lone.y, lone.z); got != worldgen.Air {
		t.Errorf("a lichen with nothing holding any face should be gone, got %d", got)
	}

	// Idempotent: a second pass finds nothing left to do.
	before := w.At(x, y, z)
	h.repairMultiface()
	if after := w.At(x, y, z); after != before {
		t.Errorf("a second sweep changed %d to %d", before, after)
	}
}
