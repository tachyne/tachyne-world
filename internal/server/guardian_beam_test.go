package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The beam locks on (synced attack target), charges for the attack
// duration without hurting, lands its hits at the end, and lets go.
func TestGuardianBeamChargesThenFires(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 100.5, 40, 100.5)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	g := h.spawnSpecies(players, entityGuardian, 0, 108.5, 40, 100.5) // 8 blocks off: in reach, past 3
	if g == nil {
		t.Skip("no guardian species in this build")
	}
	h.guardianTick(players, g)
	if g.beamTarget != pl.p.eid || g.beamTicks != -10 {
		t.Fatalf("lock-on: target %d ticks %d", g.beamTarget, g.beamTicks)
	}
	for i := 0; i < 60 && pl.health >= 20; i++ {
		if g.beamTicks < 70 && g.beamTarget != 0 { // still charging: nothing should land yet
			h.guardianTick(players, g)
			if pl.health < 20 {
				t.Fatalf("hurt at attackTime %d, before the beam lands", g.beamTicks)
			}
			continue
		}
		h.guardianTick(players, g)
	}
	if pl.health >= 20 {
		t.Fatalf("the beam never landed: health %v ticks %d", pl.health, g.beamTicks)
	}
	if g.beamTarget != 0 {
		t.Error("after the hit the beam lets go")
	}
	// A target that walks out of reach is dropped without a hit.
	h.guardianTick(players, g) // re-lock
	pl.x = 130.5
	before := pl.health
	h.guardianTick(players, g)
	if g.beamTarget != 0 || pl.health != before {
		t.Error("a target out of reach releases the beam without damage")
	}
}
