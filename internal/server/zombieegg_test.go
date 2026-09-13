package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A zombie finds a clutch of turtle eggs nearby, walks to it, stamps on it
// for sixty ticks and the clutch is gone; mob griefing off leaves it be.
func TestZombieBreaksTurtleEggs(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -4; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	w.SetBlock(6, 180, 0, turtleEggState(3, 0))
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	z.hostile = true
	z.eggNext = 0

	h.rules.MobGriefing = false
	if h.zombieEggStep(players, z) || z.eggPos != (blockPos{}) {
		t.Fatal("mob griefing off, yet the zombie went for the eggs")
	}
	h.rules.MobGriefing = true
	if !h.zombieEggStep(players, z) || z.eggPos != (blockPos{6, 180, 0}) {
		t.Fatalf("the zombie did not find the clutch: %+v", z.eggPos)
	}
	if z.vx <= 0 {
		t.Error("the zombie is not walking toward the eggs")
	}
	// Standing on the clutch it stamps; the eggs last sixty ticks.
	z.x, z.z = 6.5, 0.5
	for i := 0; i < eggStampTicks/mobMoveInterval; i++ {
		if !h.zombieEggStep(players, z) {
			t.Fatal("the zombie left the clutch")
		}
	}
	if !isTurtleEgg(w.At(6, 180, 0)) {
		t.Fatal("the clutch broke before sixty ticks")
	}
	for i := 0; i < 3 && isTurtleEgg(w.At(6, 180, 0)); i++ {
		h.zombieEggStep(players, z)
	}
	if isTurtleEgg(w.At(6, 180, 0)) || z.eggPos != (blockPos{}) {
		t.Fatal("the clutch survived the stamping")
	}
	// Nothing left to find: the search waits out its interval.
	if h.zombieEggStep(players, z) || z.eggNext == 0 {
		t.Error("with no eggs about the goal should stand down and wait")
	}
	// An egg under a roof is not a target.
	w.SetBlock(6, 180, 0, turtleEggState(1, 0))
	w.SetBlock(6, 181, 0, worldgen.Stone)
	if _, ok := h.findNearestEgg(z); ok {
		t.Error("a covered egg was targeted")
	}
}
