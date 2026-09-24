package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Water flowing onto a crop washes it out — the cell floods and the crop's
// loot drops (FlowingFluid.spreadTo → WaterFluid.beforeDestroyingBlock).
func TestWaterWashesCropAndDropsIt(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 900, 180, 900
	h.world.ForceLoad(x, z, 1)
	for dx := -6; dx <= 6; dx++ { // wide enough that no drop lies within slope reach on any side
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 2; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(x+1, y-1, z, worldgen.BlockID("farmland"))
	h.world.SetBlock(x+1, y, z, worldgen.BlockID("wheat"))
	h.world.SetBlock(x+2, y, z, worldgen.BlockID("torch"))
	h.world.SetBlock(x+2, y, z+1, worldgen.Stone)
	h.world.SetBlock(x, y, z, worldgen.WaterBase)
	h.scheduleIn(0, blockPos{x, y, z}, 1)
	runTicks(h, players, 1, 60)
	if got := h.world.At(x+1, y, z); !worldgen.IsWater(got) {
		t.Fatalf("water should have flooded the crop cell, got %d", got)
	}
	if got := h.world.At(x+2, y, z); !worldgen.IsWater(got) {
		t.Fatalf("water should have washed the torch too, got %d", got)
	}
	if got := h.world.At(x+2, y, z+1); got != worldgen.Stone {
		t.Fatal("stone must stop water")
	}
	seeds, torch := 0, 0
	for _, it := range h.items {
		switch it.item {
		case itemByName["wheat_seeds"]:
			seeds += it.count
		case itemByName["torch"]:
			torch += it.count
		}
	}
	if seeds == 0 || torch == 0 {
		t.Fatalf("washed blocks should drop their loot: seeds=%d torch=%d", seeds, torch)
	}
}

// The washing rule follows vanilla's motion-blocking test, not collision.
// TestFluidWashesByTheTag: 26.3 decides by #washed_away_by_fluids, not by
// shape — a pressure plate or a banner holds water back, a chorus flower or
// a full stack of snow layers is washed; waterloggables (rails, lanterns,
// corals) and kelp take the water in and stay.
func TestFluidWashesByTheTag(t *testing.T) {
	washed := []string{"wheat", "torch", "redstone_wire", "white_carpet", "flower_pot", "repeater", "cobweb", "skeleton_skull",
		"stone_button", "lever", "chorus_flower", "end_rod", "cocoa"}
	kept := []string{"stone", "oak_slab", "oak_fence", "oak_door", "sugar_cane", "oak_sign", "rail", "ladder", "iron_chain", "lantern", "anvil", "cake", "oak_trapdoor",
		"oak_pressure_plate", "white_banner", "moving_piston", "kelp", "seagrass", "tube_coral"}
	for _, n := range washed {
		if !fluidWashes(worldgen.BlockID(n)) {
			t.Errorf("%s should be washed away by flowing fluid", n)
		}
	}
	for _, n := range kept {
		if fluidWashes(worldgen.BlockID(n)) {
			t.Errorf("%s should stop (or hold) flowing fluid", n)
		}
	}
	if deep := worldgen.BlockBase("snow") + 7; !fluidWashes(deep) { // layers=8
		t.Error("eight snow layers are still in the tag: washed")
	}
	if !worldgen.IsReplaceable(worldgen.BlockID("vine")) || !worldgen.IsReplaceable(worldgen.BlockID("dead_bush")) {
		t.Error("vines and dead bushes are replaceable")
	}
	if worldgen.IsReplaceable(worldgen.BlockID("wheat")) {
		t.Error("crops are not replaceable")
	}
}
