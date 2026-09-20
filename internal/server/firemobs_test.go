package server

import (
	"math"
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
	// Within four blocks of the ghast's height: its target selector wants
	// somebody at roughly its own level.
	pl.x, pl.y, pl.z = 20.5, 188, 0.5
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

// A ghast drifts: it never closes on its target, and it ignores anyone far
// above or below it.
func TestGhastFloatsAndIgnoresHeight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.dim = 1
	pl.x, pl.y, pl.z = 30.5, 100, 0.5
	g := h.spawnMobIn(players, entityGhast, 1, 0.5, 190, 0.5)
	h.configureNetherMob(players, g) // the spawner's path: species quirks applied
	if _, ok := g.behavior.(floatAroundBehavior); !ok {
		t.Fatalf("a ghast floats around, got %s", g.behavior.name())
	}
	// Ninety blocks below: no shot, however long it waits.
	for i := 0; i < 40; i++ {
		h.ghastTick(players, g)
	}
	if len(h.arrows) != 0 {
		t.Error("a ghast does not shoot at somebody far below it")
	}
	if ghastCanTarget(g, pl.y) {
		t.Error("the target selector should refuse that height difference")
	}
	// The drift picks a spot within sixteen blocks and heads for it, whatever
	// the target is doing.
	g.hasTarget, g.tx, g.tz = true, pl.x, pl.z
	vx, vz := floatAroundBehavior{}.steer(h, g)
	if vx == 0 && vz == 0 {
		t.Fatal("a ghast is always drifting somewhere")
	}
	if d := math.Hypot(g.floatX-g.x, g.floatZ-g.z); d > ghastFloatRange*1.5 {
		t.Errorf("the spot should be within about sixteen blocks, got %v", d)
	}
}
