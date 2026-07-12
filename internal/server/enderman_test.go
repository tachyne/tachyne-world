package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestEndermanStareAggro: looking straight at an enderman's eyes provokes it
// (vanilla isBeingStaredBy); a carved pumpkin on the head exempts the starer.
func TestEndermanStareAggro(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnHostile(players, entityEnderman, 10, 0)
	m.x, m.y, m.z = 10.5, 64, 0.5

	// Look exactly at the enderman's eyes (dx=10, dy=eye diff, dz=0).
	dy := (m.y + 2.55) - (pl.y + 1.62)
	pl.yaw = -90 // vanilla yaw: -90 faces +x
	pl.pitch = float32(-math.Atan2(dy, 10) * 180 / math.Pi)
	if !h.staredAt(players, m) {
		t.Fatal("a crosshair on the enderman's eyes must register as a stare")
	}
	h.acquireTarget(players, m)
	if m.anger == 0 {
		t.Fatal("a stared-at enderman must anger")
	}

	// The carved pumpkin disguise exempts the starer.
	m.anger = 0
	pl.armor[0] = invStack{item: itemCarvedPumpkin, count: 1}
	if h.staredAt(players, m) {
		t.Fatal("a carved pumpkin must fool the enderman")
	}

	// Looking away must not provoke.
	pl.armor[0] = invStack{}
	pl.yaw = 90 // facing -x, away from it
	if h.staredAt(players, m) {
		t.Fatal("looking away must not provoke")
	}
}

// TestZombieReinforcements: on hard difficulty a hurt zombie with a charged
// SPAWN_REINFORCEMENTS_CHANCE summons a same-species backup targeting the
// attacker, and both lose 0.05 charge.
func TestZombieReinforcements(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffHard
	pl := testTracked()
	pl.gamemode = gmSurvival
	px, pz, found := 0, 0, false
	for x := 0; x < 300 && !found; x += 3 {
		for z := 0; z < 300 && !found; z += 3 {
			if h.world.Spawnable(x, z) {
				px, pz, found = x, z, true
			}
		}
	}
	if !found {
		t.Skip("no spawnable column")
	}
	pl.x, pl.y, pl.z = float64(px)+0.5, h.world.SurfaceY(px, pz), float64(pz)+0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnHostile(players, entityZombie, px+2, pz)
	m.reinf = 1.0 // guaranteed call
	before := len(h.mobs)

	h.zombieReinforce(players, m, pl)
	if len(h.mobs) != before+1 {
		t.Fatalf("reinforcement should have spawned: %d mobs, want %d", len(h.mobs), before+1)
	}
	if m.reinf != 0.95 {
		t.Fatalf("caller charge should drop 0.05: got %v", m.reinf)
	}
	for _, o := range h.mobs {
		if o != m && o.etype == entityZombie && o != nil {
			if o.reinf != 0.95 {
				t.Fatalf("recruit charge should be caller-0.05: got %v", o.reinf)
			}
			if !o.hasTarget {
				t.Fatal("the recruit must already hunt the attacker")
			}
		}
	}

	// Never on normal difficulty.
	h.rules.Difficulty = diffNormal
	m.reinf = 1.0
	before = len(h.mobs)
	h.zombieReinforce(players, m, pl)
	if len(h.mobs) != before {
		t.Fatal("reinforcements are a HARD-difficulty mechanic")
	}
}
