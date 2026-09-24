package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// VexChargeAttackGoal: a vex with a player about darts at them, charging,
// and the blow lands when it touches them.
func TestVexChargesAndStrikes(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 200, 0.5
	v := h.spawnMob(players, entityVex, 0.5, 200, 0.5)
	v.hostile = true
	before := pl.health
	charged := false
	for i := 0; i < 200 && pl.health == before; i++ {
		h.vexFlight(players, v)
		charged = charged || v.vexCharging
	}
	if !charged {
		t.Fatal("the vex never charged")
	}
	if pl.health >= before {
		t.Fatalf("the vex charged but never struck (vex at %.1f,%.1f,%.1f)", v.x, v.y, v.z)
	}
}

// VexRandomMoveGoal: with nobody to fight, a summoned vex drifts about its
// bound origin, never far from it.
func TestVexDriftsAboutItsOrigin(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	v := h.spawnMob(players, entityVex, 0.5, 200, 0.5)
	v.vexOrigin, v.vexHasOrigin = blockPos{0, 200, 0}, true
	moved := false
	for i := 0; i < 400; i++ {
		h.vexFlight(players, v)
		moved = moved || math.Hypot(v.x-0.5, v.z-0.5) > 1
		if math.Abs(v.x) > 10 || math.Abs(v.z) > 10 || math.Abs(v.y-200) > 8 {
			t.Fatalf("the vex strayed to %.1f,%.1f,%.1f", v.x, v.y, v.z)
		}
	}
	if !moved {
		t.Error("the vex never drifted")
	}
}
