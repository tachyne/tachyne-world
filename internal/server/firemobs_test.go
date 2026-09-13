package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestGhastChargeAndBlazeVolley: a ghast with a target charges for twenty
// ticks then fires and rests; a blaze flares, waits sixty, fires three
// fireballs six ticks apart and rests a hundred.
func TestGhastChargeAndBlazeVolley(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 20.5, 180, 0.5
	g := h.spawnMobIn(players, entityGhast, 1, 0.5, 190, 0.5)
	pl.dim = 1
	before := len(h.arrows)
	for i := 0; i < 5; i++ {
		h.ghastTick(players, g)
	}
	if g.ghastCharge != 10 || !(g.ghastCharge > ghastChargeWarn) == false && len(h.arrows) != before {
		t.Fatalf("ten ticks in: warned, not yet charged-looking: charge %d", g.ghastCharge)
	}
	for i := 0; i < 5; i++ {
		h.ghastTick(players, g)
	}
	if len(h.arrows) != before+1 || g.ghastCharge != ghastChargeRest {
		t.Fatalf("at twenty it fires and rests: bullets %d charge %d", len(h.arrows)-before, g.ghastCharge)
	}
	b := h.spawnMobIn(players, entityBlaze, 1, 0.5, 180, 0.5)
	pl.x = 6.5
	before = len(h.arrows)
	h.blazeTick(players, b)
	if b.blazeStep != 1 || !b.blazeCharged || b.blazeTime != blazeFlareTicks {
		t.Fatalf("it flares first: step %d charged %v time %d", b.blazeStep, b.blazeCharged, b.blazeTime)
	}
	for i := 0; i < 200 && b.blazeStep != 0; i++ {
		h.blazeTick(players, b)
	}
	if len(h.arrows) != before+blazeVolleyShots || b.blazeCharged || b.blazeTime != blazeRestTicks {
		t.Fatalf("three fireballs, then a hundred ticks' rest: shots %d charged %v time %d", len(h.arrows)-before, b.blazeCharged, b.blazeTime)
	}
}
