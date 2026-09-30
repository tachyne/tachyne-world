package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// scanHeight is the heightmap rule read cell by cell from the sky: the
// reference the stored heightmaps must always agree with.
func scanHeight(w *World, t HeightmapType, x, z int) int {
	for y := w.Ceiling() - 1; y >= worldgen.MinY; y-- {
		if worldgen.HeightmapFlags(w.At(x, y, z))&hmBit[t] != 0 {
			return y + 1
		}
	}
	return worldgen.MinY
}

func checkColumn(t *testing.T, w *World, x, z int, step string) {
	t.Helper()
	for ty := HeightmapType(0); ty < numHeightmaps; ty++ {
		got, ok := w.Heightmap(ty, x, z)
		if !ok {
			t.Fatalf("%s: heightmap %d not available for a loaded chunk", step, ty)
		}
		if want := scanHeight(w, ty, x, z); got != want {
			t.Errorf("%s: heightmap %d at (%d,%d) = %d, a scan says %d", step, ty, x, z, got, want)
		}
	}
}

// The four stored heightmaps follow SetBlock (Heightmap.update): a block
// raises the kinds it counts toward, and removing the top block drops each
// to the next matching block below — leaves count for MOTION_BLOCKING and
// OCEAN_FLOOR but not NO_LEAVES, water for the motion maps only, a torch
// for WORLD_SURFACE only.
func TestStoredHeightmapsFollowEdits(t *testing.T) {
	w := New(1)
	w.ForceLoad(5, 5, 0)
	x, z := 5, 5
	checkColumn(t, w, x, z, "generated")

	// BlockBase is the first state, which for leaves is waterlogged — and a
	// block holding water counts toward MOTION_BLOCKING_NO_LEAVES in 26.3.
	leaves := worldgen.BlockBase("oak_leaves")
	if info, ok := worldgen.InfoForState(leaves); ok {
		leaves = worldgen.SetProperty(info, leaves, "waterlogged", "false")
	}
	torch := worldgen.BlockBase("torch")
	w.SetBlock(x, 300, z, worldgen.Stone)
	checkColumn(t, w, x, z, "stone at 300")
	if y, _ := w.Heightmap(MotionBlocking, x, z); y != 301 {
		t.Errorf("MOTION_BLOCKING over a stone at 300 = %d, want 301", y)
	}
	w.SetBlock(x, 305, z, leaves)
	checkColumn(t, w, x, z, "leaves at 305")
	if y, _ := w.Heightmap(MotionBlockingNoLeaves, x, z); y != 301 {
		t.Errorf("NO_LEAVES must look through leaves: %d, want 301", y)
	}
	if y, _ := w.Heightmap(OceanFloor, x, z); y != 306 {
		t.Errorf("OCEAN_FLOOR counts leaves (#blocks_motion_in_heightmap): %d, want 306", y)
	}
	w.SetBlock(x, 310, z, worldgen.WaterBase)
	checkColumn(t, w, x, z, "water at 310")
	if y, _ := w.Heightmap(OceanFloor, x, z); y != 306 {
		t.Errorf("OCEAN_FLOOR must look through water: %d, want 306", y)
	}
	w.SetBlock(x, 315, z, torch)
	checkColumn(t, w, x, z, "torch at 315")
	if y, _ := w.Heightmap(WorldSurface, x, z); y != 316 {
		t.Errorf("WORLD_SURFACE over a torch at 315 = %d, want 316", y)
	}
	if y, _ := w.Heightmap(MotionBlocking, x, z); y != 311 {
		t.Errorf("a torch blocks no motion: MOTION_BLOCKING %d, want 311", y)
	}
	for _, y := range []int{315, 310, 305, 300} { // take the column down again
		w.SetBlock(x, y, z, worldgen.Air)
		checkColumn(t, w, x, z, "cleared")
	}
}

// Edits made before a chunk is loaded are in its heightmaps when they are
// first built, and a chunk that is not loaded has none (and is not loaded
// by asking).
func TestStoredHeightmapsPrimeWithEditsAndNeverLoad(t *testing.T) {
	w := New(1)
	x, z := 1000, 1000
	w.SetBlock(x, 310, z, worldgen.Stone) // not loaded: an edit only
	if _, ok := w.Heightmap(MotionBlocking, x, z); ok {
		t.Fatal("an unloaded chunk reported a heightmap")
	}
	if w.Loaded(int32(x>>4), int32(z>>4)) {
		t.Fatal("asking for a heightmap loaded the chunk")
	}
	if y := w.HeightAt(MotionBlocking, x, z); y != 311 {
		t.Errorf("HeightAt's fallback scan: %d, want 311", y)
	}
	w.ForceLoad(x, z, 0)
	checkColumn(t, w, x, z, "primed")
	if y, _ := w.Heightmap(WorldSurface, x, z); y != 311 {
		t.Errorf("the pre-load edit is missing from WORLD_SURFACE: %d", y)
	}
	w.RevertEdit(x, 310, z) // back to generation
	checkColumn(t, w, x, z, "reverted")
}
