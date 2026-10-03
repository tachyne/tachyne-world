package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func golemStrollRig(t *testing.T) (*hub, map[int32]*tracked, *mob) {
	t.Helper()
	h := newTestHub(world.New(1))
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
//
// The pace is the golem's own walk. A villager that walks into it shoves it
// (Entity.push: the golem is pushable, and IronGolem.doPush only adds its
// 1-in-20 targeting roll), which moves it well past 0.6 for a tick or two, as
// in vanilla. So the cap skips an update whose ticks began with the golem
// touching another body, and the few after it while that motion bleeds off
// (0.546 a tick: under 1% of the shove is left after five updates).
func TestIronGolemStrollsTheVillage(t *testing.T) {
	h, players, g := golemStrollRig(t)
	v := h.spawnSpecies(players, entityVillager, 0, 20.5, 180, 0.5)
	v.bed = blockPos{20, 180, 2} // a claimed bed makes this a village
	v.lastSlept = h.tick.Load() + 1
	h.gridDirty()
	cap := walkPerUpdate(g, golemStrollSpeed) * 1.05
	const shoveFade = 5 // updates a shove's motion is still worth more than the 5% slack
	walked, lastShove, checked := 0.0, -shoveFade-1, 0
	for i := 0; i < 1500; i++ {
		px, pz := g.x, g.z
		h.tick.Add(mobMoveInterval)
		for k := 0; k < mobGoalInterval; k++ {
			for _, o := range h.mobs {
				if o != g && g.overlaps(o) {
					lastShove = i // pushMobs, at the head of this tick, shoves the pair apart
				}
			}
			h.updateMobs(players)
		}
		d := math.Hypot(g.x-px, g.z-pz)
		walked += d
		if i-lastShove <= shoveFade {
			continue
		}
		checked++
		if d > cap {
			t.Fatalf("update %d: an idle golem walks at 0.6 (%.3f), not %.3f", i, cap, d)
		}
	}
	if checked < 1000 {
		t.Fatalf("the pace was checked on only %d of 1500 updates: the golem spent the test being shoved", checked)
	}
	if walked < 10 {
		t.Fatalf("the golem should have strolled about the village, walked %.1f blocks", walked)
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
