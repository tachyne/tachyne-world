package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A witch in throwing range stands (RangedAttackGoal); a blaze that sees
// its target holds its ground (BlazeAttackGoal) — neither backs away like a
// skeleton.
func TestWitchAndBlazeStandTheirGround(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	for _, tc := range []struct {
		etype int
		dist  float64
	}{{entityWitch, 5}, {entityBlaze, 5}, {entityBlaze, 20}} {
		m := h.spawnMob(players, tc.etype, 0.5, 180, 0.5)
		if tc.etype == entityBlaze {
			h.configureNetherMob(players, m)
		} else {
			h.configureHostile2(players, m)
		}
		m.hasTarget, m.tx, m.tz = true, 0.5+tc.dist, 0.5
		if vx, vz := m.behavior.steer(h, m); vx != 0 || vz != 0 {
			t.Errorf("%s with its target %v blocks off moves (%v, %v)", advEntityName[tc.etype], tc.dist, vx, vz)
		}
	}
	w := h.spawnMob(players, entityWitch, 0.5, 180, 0.5)
	h.configureHostile2(players, w)
	w.hasTarget, w.tx, w.tz = true, 20.5, 0.5
	if vx, _ := w.behavior.steer(h, w); vx <= 0 {
		t.Error("a witch out of range does not close in")
	}
}
