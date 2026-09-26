package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Report #47: "is this iron golem supposed to be stuck on top of the village
// fountain?" — a plains meeting-point fountain, rebuilt from the report's
// capture: a one-block cobblestone pillar with a water source on top, the
// water falling round it into a basin two blocks down.

// The spawn itself is vanilla's: SpawnUtil's LEGACY_IRON_GOLEM strategy takes
// a solid block with air OR LIQUID over it, and IronGolem.checkSpawnObstruction
// tests the feet cell with an empty fluid, so the source on the pillar is a
// place a village's golem may appear.
func TestGolemMaySpawnOnTheFountainPillar(t *testing.T) {
	h := newHub(world.New(1))
	stampBugCapture(t, h.world, "bug47-fountain.json")
	w := h.world
	x, z := -249, -586
	if !golemFloor(w.At(x, 76, z), w.At(x, 77, z)) {
		t.Fatalf("the pillar top (%d) under the source (%d) is a golem floor in vanilla", w.At(x, 76, z), w.At(x, 77, z))
	}
	if !golemUnobstructed(w.At(x, 76, z), w.At(x, 77, z), w.At(x, 78, z), w.At(x, 79, z)) {
		t.Fatal("the source on the pillar is room for a golem in vanilla")
	}
}

// …but a golem there must be able to get down, as vanilla's does: the drop
// into the basin is two blocks, inside Mob.getMaxFallDistance's three. The
// walker refused any drop past one, so it paced the pillar top for good.
func TestGolemWalksOffTheFountainPillar(t *testing.T) {
	h := newHub(world.New(1))
	stampBugCapture(t, h.world, "bug47-fountain.json")
	players := map[int32]*tracked{}
	h.playersRef = players
	h.dayTime.Store(1000)
	g := h.spawnMob(players, entityIronGolem, -248.5, 77, -585.5) // where the report found it
	g.health, g.behavior = 100, golemBehavior{}
	g.setKBResist(1)
	g.home = blockPos{-249, 76, -588}
	for i := 0; i < 3000; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if g.y < 77 && math.Hypot(g.x+248.5, g.z+585.5) > 2.5 {
			return // down and away from the fountain
		}
	}
	t.Fatalf("the golem is still on the fountain: %.2f %.2f %.2f", g.x, g.y, g.z)
}

// A walker steps off a drop of up to three, not four (the comfortable fall
// distance with no target).
func TestWalkerDropsUpToThree(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -4; x <= 8; x++ {
		for z := -4; z <= 4; z++ {
			for y := 170; y <= 185; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			h.world.SetBlock(x, 169, z, worldgen.Stone)
		}
	}
	for z := -4; z <= 4; z++ {
		for y := 170; y <= 179; y++ {
			h.world.SetBlock(0, y, z, worldgen.Stone) // a ledge whose top is y=180 feet
		}
	}
	players := map[int32]*tracked{}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	for _, c := range []struct {
		floor int
		ok    bool
	}{{177, true}, {176, false}} {
		for y := 170; y < 180; y++ {
			h.world.SetBlock(1, y, 0, worldgen.Air)
		}
		for y := 170; y < c.floor; y++ {
			h.world.SetBlock(1, y, 0, worldgen.Stone)
		}
		if got := h.mobStepOK(cow, 1.5, 0.5); got != c.ok {
			t.Errorf("a drop of %d: step allowed %v, want %v", 180-c.floor, got, c.ok)
		}
	}
}
