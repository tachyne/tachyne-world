package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The fit gate on the LIVE path — vanilla's getMaxFreeTreeHeight with real
// blocks to measure against. This is the rule Wesley asked about: a tree
// measures the space it needs before placing anything, which is what stops a
// sapling punching its canopy through the house it was planted in.

// stoneCeiling roofs a square over (x,z) at height y.
func stoneCeiling(w *world.World, x, y, z, half int) {
	for dx := -half; dx <= half; dx++ {
		for dz := -half; dz <= half; dz++ {
			w.SetBlock(x+dx, y, z+dz, worldgen.Stone)
		}
	}
}

func TestSaplingRefusesToGrowUnderARoof(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 300, 200, 300
	lo, hi := worldgen.BlockRange("oak_sapling")

	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, lo+1)     // stage 1: the next tick tries to grow
	stoneCeiling(h.world, x, y+3, z, 4) // a roof 3 above the floor

	// Drive the grower DIRECTLY rather than spinning thousands of random
	// ticks: refusal must hold on every attempt, and the random-tick cadence
	// is someone else's test. (The 8000-spin version pushed the whole package
	// past the 600s -race timeout.)
	sp := saplingSpecies[0]
	for i := 0; i < 32; i++ {
		h.growSapling(players, 0, x, y, z, lo+1, sp)
	}
	if s := h.world.At(x, y, z); s < lo || s > hi {
		t.Fatal("a sapling under a 3-high ceiling grew anyway")
	}
	// …and nothing punched through the roof.
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			if got := h.world.At(x+dx, y+3, z+dz); got != worldgen.Stone {
				t.Fatalf("the roof at (%d,%d) became %d — the tree ate the ceiling", dx, dz, got)
			}
		}
	}
	// The sapling is still planted, not consumed.
	if s := h.world.At(x, y, z); s < lo || s > hi {
		t.Errorf("the sapling should survive a refused growth, got state %d", s)
	}
}

// The same sapling in the open grows fine — the gate refuses cramped spots,
// not everything.
func TestSaplingGrowsInTheOpen(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 340, 200, 340
	lo, hi := worldgen.BlockRange("oak_sapling")
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, lo)
	if !growUntilGone(h, players, x, y, z, lo, hi) {
		t.Fatal("an unobstructed sapling never grew")
	}
	if !worldgen.IsLog(h.world.At(x, y, z)) {
		t.Errorf("expected a trunk at the sapling cell, got %d", h.world.At(x, y, z))
	}
}

// A wall beside the sapling is fine — the gate probes the clearance the tree
// NEEDS (minimum_size), not a full canopy box. An oak needs its column plus
// radius 1 near the top; a wall two blocks away does not block it.
func TestSaplingGrowsBesideAWall(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 380, 200, 380
	lo, hi := worldgen.BlockRange("oak_sapling")
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, lo)
	for dy := 0; dy < 12; dy++ { // a wall 2 away on +x
		h.world.SetBlock(x+2, y+dy, z, worldgen.Stone)
	}
	if !growUntilGone(h, players, x, y, z, lo, hi) {
		t.Fatal("a sapling two blocks from a wall should still grow")
	}
	// The wall is intact — leaves stopped at it rather than replacing it.
	for dy := 0; dy < 12; dy++ {
		if h.world.At(x+2, y+dy, z) != worldgen.Stone {
			t.Fatalf("the wall at dy=%d was overwritten", dy)
		}
	}
}

// Planted and generated trees are the same tree: growing a species from its
// sapling produces a shape PlaceTree could have produced — pinned by checking
// the trunk stands on the same species log and leaves carry the vanilla
// distance-7 state, which the old hand-rolled stamper did not set.
func TestPlantedLeavesCarryTheVanillaState(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 420, 200, 420
	lo, hi := worldgen.BlockRange("birch_sapling")
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, lo)
	if !growUntilGone(h, players, x, y, z, lo, hi) {
		t.Fatal("birch never grew")
	}
	// The canopy carries SEEDED distances: every leaf 1..6, none at the
	// decaying 7 — which is what the placement BFS exists to guarantee. (This
	// used to hunt the exact distance-7 state, which is now correctly absent.)
	lo, hiLeaf := worldgen.BlockRange("birch_leaves")
	found, decaying := false, 0
	for dy := 0; dy < 12; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				s := h.world.At(x+dx, y+dy, z+dz)
				if s < lo || s > hiLeaf {
					continue
				}
				found = true
				if _, d, persistent, ok := leafInfo(s); ok && d == 7 && !persistent {
					decaying++
				}
			}
		}
	}
	if !found {
		t.Error("no birch leaves around the planted tree")
	}
	if decaying > 0 {
		t.Errorf("%d planted leaves at distance 7 — they would rot on their first ticks", decaying)
	}
}
