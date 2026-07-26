package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The border's size is a DIAMETER, not a radius — getting that backwards
// halves or doubles everyone's world, so pin it.
func TestBorderSizeIsADiameter(t *testing.T) {
	b := worldBorder{CenterX: 0, CenterZ: 0, Size: 100}
	// A 100-wide border centred on origin reaches 50 blocks each way.
	if d := b.distanceToBorder(0, 0, 100); d != 50 {
		t.Errorf("centre is %v from the edge, want 50", d)
	}
	if d := b.distanceToBorder(49, 0, 100); d != 1 {
		t.Errorf("x=49 is %v from the edge, want 1", d)
	}
	// Outside is NEGATIVE — the damage rule depends on the sign.
	if d := b.distanceToBorder(60, 0, 100); d != -10 {
		t.Errorf("x=60 is %v from the edge, want -10", d)
	}
}

// A moving border is computed from the clock rather than animated.
func TestBorderInterpolatesOverTime(t *testing.T) {
	b := worldBorder{Size: 100, OldSize: 500, StartTick: 1000, LerpTicks: 200}
	for _, c := range []struct {
		now  uint64
		want float64
	}{
		{999, 500},  // before it sets off
		{1000, 500}, // the moment it does
		{1100, 300}, // halfway: 500 -> 100
		{1200, 100}, // arrived
		{9999, 100}, // and stays
	} {
		if got := b.sizeAt(c.now); got != c.want {
			t.Errorf("size at tick %d = %v, want %v", c.now, got, c.want)
		}
	}
}

// Damage starts only beyond the safe zone, scales with distance, and never
// falls below half a heart.
func TestBorderDamageOnlyBeyondTheSafeZone(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.border = worldBorder{Size: 100, Damage: 0.2, SafeZone: 5}

	at := func(x float64) float32 {
		pl.x, pl.z = x, 0
		pl.health, pl.dead = 20, false
		h.borderDamage(players)
		return 20 - pl.health
	}

	if d := at(0); d != 0 {
		t.Errorf("standing in the middle cost %v health", d)
	}
	if d := at(52); d != 0 {
		t.Errorf("inside the safe zone cost %v health", d)
	}
	// 100 blocks out: distance -50, +5 safe zone = -45, x0.2 = 9.
	if d := at(100); d != 9 {
		t.Errorf("100 blocks out cost %v, want 9", d)
	}
	// Just past the buffer still costs the minimum half-heart, never zero.
	if d := at(56); d != 1 {
		t.Errorf("just past the buffer cost %v, want the 1 minimum", d)
	}
}

// The command drives the state, and a timed set leaves a border in motion.
func TestWorldBorderCommand(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.tick.Store(500)

	h.cmdWorldBorder(players, pl, []string{"set", "200"})
	if h.border.Size != 200 || h.border.LerpTicks != 0 {
		t.Fatalf("set: size=%v lerp=%v", h.border.Size, h.border.LerpTicks)
	}
	h.cmdWorldBorder(players, pl, []string{"center", "10", "-20"})
	if h.border.CenterX != 10 || h.border.CenterZ != -20 {
		t.Errorf("centre = %v,%v", h.border.CenterX, h.border.CenterZ)
	}
	// A timed set starts a move rather than jumping.
	h.cmdWorldBorder(players, pl, []string{"set", "50", "10"})
	if h.border.LerpTicks != 200 || h.border.OldSize != 200 {
		t.Errorf("timed set: lerp=%v old=%v", h.border.LerpTicks, h.border.OldSize)
	}
	if got := h.border.sizeAt(600); got != 125 {
		t.Errorf("halfway through the move size=%v, want 125", got)
	}
	// …and the frame hands the client the remaining time, not the whole move.
	h.tick.Store(600)
	f := h.borderFrame()
	if f.Target != 50 || f.LerpMs != 100*50 {
		t.Errorf("frame mid-move: target=%v lerpMs=%v", f.Target, f.LerpMs)
	}
	// add is relative to the current size.
	h.tick.Store(2000)
	h.cmdWorldBorder(players, pl, []string{"add", "25"})
	if h.border.Size != 75 {
		t.Errorf("add: size=%v, want 75", h.border.Size)
	}
}
