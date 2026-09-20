package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A handful of blocks had no survival rule at all, so they simply hung in the
// air when whatever held them went. These are vanilla's own rules.
func TestGrowingPlantsNeedTheirAnchor(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 30, 180, 30
	clear := func() {
		for dy := -4; dy <= 4; dy++ {
			w.SetBlock(x, y+dy, z, worldgen.Air)
		}
	}

	// Kelp grows UP, so it hangs off the block BELOW it.
	clear()
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockBase("kelp"))
	if !supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("kelp on stone should stand")
	}
	w.SetBlock(x, y-1, z, worldgen.Air)
	if supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("kelp with nothing under it should not")
	}
	// …and a stalk hangs off the stalk beneath it.
	w.SetBlock(x, y-1, z, worldgen.BlockBase("kelp_plant"))
	if !supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("kelp on kelp should stand")
	}

	// Cave vines grow DOWN, so they hang off the block ABOVE.
	clear()
	w.SetBlock(x, y+1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockBase("cave_vines"))
	if !supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("cave vines under stone should hang")
	}
	w.SetBlock(x, y+1, z, worldgen.Air)
	if supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("cave vines under nothing should fall")
	}
	// A plant does not hang off the plant it grows AWAY from.
	w.SetBlock(x, y-1, z, worldgen.BlockBase("cave_vines_plant"))
	if supported(w, blockPos{x, y, z}, w.At(x, y, z)) {
		t.Error("cave vines were held up by what is below them")
	}

	// …and the live sweep actually takes them down.
	clear()
	w.SetBlock(x, y+1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockBase("cave_vines"))
	w.SetBlock(x, y+1, z, worldgen.Air)
	h.dropUnsupported(players, 0, blockPos{x, y + 1, z})
	if s := w.At(x, y, z); s != worldgen.Air {
		t.Errorf("the vine is still there (%d) after its anchor went", s)
	}
}

// FrogspawnBlock.mayPlaceOn: water under it, and it floats ON the surface
// rather than under it.
func TestFrogspawnNeedsWaterUnderIt(t *testing.T) {
	w := world.New(1)
	const x, y, z = 34, 180, 34
	spawn := worldgen.BlockBase("frogspawn")
	for _, tc := range []struct {
		name  string
		below uint32
		above uint32
		want  bool
	}{
		{"on water", worldgen.WaterBase, worldgen.Air, true},
		{"on stone", worldgen.Stone, worldgen.Air, false},
		{"on nothing", worldgen.Air, worldgen.Air, false},
		{"under water", worldgen.WaterBase, worldgen.WaterBase, false},
	} {
		w.SetBlock(x, y-1, z, tc.below)
		w.SetBlock(x, y, z, spawn)
		w.SetBlock(x, y+1, z, tc.above)
		if got := supported(w, blockPos{x, y, z}, spawn); got != tc.want {
			t.Errorf("%s: survives=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// DirtPathBlock.canSurvive is about what is ABOVE it, and a path that loses
// the argument turns back into dirt rather than falling.
func TestDirtPathTurnsBackToDirtUnderABlock(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 38, 180, 38
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, dirtPathState)
	w.SetBlock(x, y+1, z, worldgen.Stone)

	h.dropUnsupported(players, 0, blockPos{x, y + 1, z})

	if got := w.At(x, y, z); got != worldgen.Dirt {
		t.Errorf("a covered path is %d, want dirt (%d)", got, worldgen.Dirt)
	}
	// A fence gate is the exception vanilla names, and an open sky is fine.
	w.SetBlock(x, y, z, dirtPathState)
	w.SetBlock(x, y+1, z, worldgen.BlockBase("oak_fence_gate"))
	h.dropUnsupported(players, 0, blockPos{x, y + 1, z})
	if got := w.At(x, y, z); got != dirtPathState {
		t.Errorf("a path under a fence gate became %d, want it left alone", got)
	}
}
