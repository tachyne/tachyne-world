package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// A walker climbs floor(max(1, step_height)) blocks (WalkNodeEvaluator's
// jumpSize): a cow jumps one block but not two, and on a raised
// step_height it walks up the two, in its steps and in its planned route.
func TestStepHeightSetsTheClimb(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -4; x <= 8; x++ {
		for z := -4; z <= 4; z++ {
			for y := 180; y <= 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			for y := worldgen.MinY + 1; y <= 179; y++ { // solid to the floor: the route planner reads the column
				h.world.SetBlock(x, y, z, worldgen.Stone)
			}
		}
	}
	for x := 1; x <= 8; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 180, z, worldgen.Stone)
			h.world.SetBlock(x, 181, z, worldgen.Stone) // a two-block rise
		}
	}
	players := map[int32]*tracked{}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	if cow.climb() != 1 || h.mobStepOK(cow, 1.5, 0.5) {
		t.Fatal("a cow on the default 0.6 does not climb two blocks")
	}
	if path, reached := findPathLimits(h.world, malusFor(cow.etype), 0, 0, 4, 0, pathMaxRange, pathMaxNodes, cow.climb()); reached {
		t.Fatalf("the route should not plan a two-block climb: %v", path)
	}
	cow.attrs.SetBase(attr.StepHeight, 2.5)
	if cow.climb() != 2 || !h.mobStepOK(cow, 1.5, 0.5) {
		t.Fatal("a step_height of 2.5 walks up two blocks")
	}
	if _, reached := findPathLimits(h.world, malusFor(cow.etype), 0, 0, 4, 0, pathMaxRange, pathMaxNodes, cow.climb()); !reached {
		t.Fatal("the route plans the climb a raised step_height allows")
	}
	if horse := h.spawnMob(players, entityHorse, -2.5, 180, 0.5); horse.climb() != 1 {
		t.Fatalf("a horse's 1.0 is a one-block climb: %d", horse.climb())
	}
}
