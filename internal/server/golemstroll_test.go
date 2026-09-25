package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func golemStrollRig(t *testing.T) (*hub, map[int32]*tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	for x := -30; x <= 30; x++ {
		for z := -30; z <= 30; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 40.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.dayTime.Store(1000)
	g := h.spawnMob(players, entityIronGolem, 0.5, 180, 0.5) // as golemspawn.go makes one
	g.health, g.behavior = 100, golemBehavior{}
	g.setKBResist(1)
	return h, players, g
}

// An idle golem walks its village at 0.6 of its pace, toward a villager that
// wants a golem more often than not; it used to stand where it was made.
func TestIronGolemStrollsTheVillage(t *testing.T) {
	h, players, g := golemStrollRig(t)
	v := h.spawnSpecies(players, entityVillager, 0, 20.5, 180, 0.5)
	v.bed = blockPos{20, 180, 2} // a claimed bed makes this a village
	v.lastSlept = h.tick.Load() + 1
	h.gridDirty()
	cap := g.moveSpeed() * golemStrollSpeed * 1.05
	sx, sz := g.x, g.z
	far := 0.0
	for i := 0; i < 1500; i++ {
		px, pz := g.x, g.z
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if d := math.Hypot(g.x-px, g.z-pz); d > cap {
			t.Fatalf("update %d: an idle golem walks at 0.6 (%.3f), not %.3f", i, cap, d)
		}
		far = math.Max(far, math.Hypot(g.x-sx, g.z-sz))
	}
	if far < 5 {
		t.Fatalf("the golem should have strolled about the village, got at most %.1f blocks", far)
	}
	if !g.golemStrolling {
		t.Fatal("with nothing to fight it is strolling")
	}
}

// A golem after a zombie is not held to the stroll's pace.
func TestIronGolemChasesAtFullPace(t *testing.T) {
	h, players, g := golemStrollRig(t)
	z := h.spawnSpecies(players, entityZombie, 0, 8.5, 180, 0.5)
	z.hostile = true
	h.gridDirty()
	if vx, _ := g.behavior.steer(h, g); vx <= 0 || g.golemStrolling {
		t.Fatalf("the golem should head for the zombie: vx %.3f strolling %v", vx, g.golemStrolling)
	}
	if h.strollSpeedFor(g) != 1 {
		t.Fatalf("a chase is not capped at the stroll's 0.6: %.2f", h.strollSpeedFor(g))
	}
}
