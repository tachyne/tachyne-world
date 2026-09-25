package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// courtRig lays a small stone pad at y=179 and puts two courting animals of
// one kind seven blocks apart on it.
func courtRig(t *testing.T, etype int) (*hub, map[int32]*tracked, *mob, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	for x := -2; x <= 10; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	a := h.spawnMob(players, etype, 0.5, 180, 0.5)
	b := h.spawnMob(players, etype, 7.5, 180, 0.5)
	if a == nil || b == nil {
		t.Fatalf("%s did not spawn", advEntityName[etype])
	}
	a.loveTicks, b.loveTicks = loveTicks, loveTicks
	h.gridDirty()
	return h, players, a, b
}

// BreedGoal: two fed cows seven blocks apart walk to each other rather than
// milling about until they bump into one another.
func TestCourtingAnimalsWalkToEachOther(t *testing.T) {
	h, players, a, b := courtRig(t, entityCow)
	for i := 0; i < 30 && math.Hypot(a.x-b.x, a.z-b.z) > breedMeetRange; i++ {
		h.updateMobs(players)
		h.gridDirty()
	}
	if d := math.Hypot(a.x-b.x, a.z-b.z); d > breedMeetRange {
		t.Fatalf("courting cows still %.1f blocks apart after 30 updates", d)
	}
}

// A cat's BreedGoal walks at 0.8 of its speed, not a full run.
func TestCatCourtsAtItsGoalSpeed(t *testing.T) {
	h, players, a, _ := courtRig(t, entityCat)
	fastest := 0.0
	for i := 0; i < 8; i++ {
		ox, oz := a.x, a.z
		h.updateMobs(players)
		h.gridDirty()
		fastest = math.Max(fastest, math.Hypot(a.x-ox, a.z-oz))
	}
	sp := a.moveSpeed()
	if fastest > 0.8*sp*1.01 || fastest < 0.5*sp {
		t.Fatalf("courting cat's fastest step %.4f, want about 0.8 × %.4f", fastest, sp)
	}
}

// With no partner in range BreedGoal never starts: a fed animal alone
// strolls and idles like any other, rather than trotting non-stop.
func TestLoneCourtingAnimalIdles(t *testing.T) {
	h, players, a, b := courtRig(t, entityCow)
	h.removeMob(players, b)
	idle := 0
	for i := 0; i < 40; i++ {
		h.updateMobs(players)
		if a.rest > 0 {
			idle++
		}
	}
	if idle == 0 {
		t.Fatal("a lone fed cow never stood idle")
	}
}
