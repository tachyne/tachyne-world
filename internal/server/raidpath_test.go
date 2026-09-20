package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A raider that has strayed from its raid walks back to it, and picks up any
// idle raider it passes. Before this a stray never returned, and a wave only
// advances when all of its raiders are dead — so one wanderer could stall the
// raid until the no-player timeout ended it.
func TestStrayRaiderWalksBackAndRecruits(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.playersRef = players
	lx, lz := h.findLand(120, 120)
	center := blockPos{lx, h.world.SurfaceFeet(lx, lz), lz}
	h.raids[center] = &raid{center: center, uuid: raidUUID(center),
		alive: map[int32]bool{}, shown: map[int32]bool{}, numGroups: 3}

	// A pillager eighty blocks out, in the raid, with nothing to fight.
	far := float64(lx + 80)
	m := h.spawnMob(players, entityPillager, far, float64(center.y), float64(lz))
	m.raidCenter = center
	before := math.Hypot(m.x-float64(center.x), m.z-float64(center.z))

	// And an idle pillager beside it, in no raid at all.
	idle := h.spawnMob(players, entityPillager, far+3, float64(center.y), float64(lz))
	if idle.raidCenter != (blockPos{}) {
		t.Fatal("the second pillager should start outside any raid")
	}

	if !h.raidPathStep(players, m) {
		t.Fatal("a stray raider should take the walk-to-the-raid goal")
	}
	if m.vx == 0 && m.vz == 0 {
		t.Fatal("it should be moving")
	}
	if m.vx > 0 {
		t.Errorf("it should head back west toward the raid, vx=%v", m.vx)
	}
	if idle.raidCenter != center {
		t.Errorf("the idle pillager alongside should have been recruited, got %v", idle.raidCenter)
	}
	if !h.raids[center].alive[idle.eid] {
		t.Error("a recruit counts toward the wave")
	}
	_ = before

	// Standing at the centre, the goal stands down and the ordinary ones run.
	m.x, m.z = float64(center.x)+1, float64(center.z)+1
	if h.raidPathStep(players, m) {
		t.Error("a raider already at the village should not take the goal")
	}
	// And so does a raider whose raid is over.
	m.x, m.z = far, float64(lz)
	delete(h.raids, center)
	if h.raidPathStep(players, m) {
		t.Error("the goal should stop once the raid is over")
	}
}

// A raider with prey to chase keeps hunting: PathfindToRaidGoal only runs
// when getTarget() is null.
func TestRaiderWithATargetIgnoresTheRaidWalk(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.playersRef = players
	center := blockPos{0, 70, 0}
	h.raids[center] = &raid{center: center, alive: map[int32]bool{}, shown: map[int32]bool{}}
	m := h.spawnMob(players, entityVindicator, 90, 70, 0)
	m.raidCenter = center
	m.hasTarget, m.tx, m.tz = true, 95, 0
	if h.raidPathStep(players, m) {
		t.Error("a raider with a target should keep hunting")
	}
}
