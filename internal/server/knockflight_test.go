package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// flyKnocked runs mob updates until the knocked mob's flight is over,
// returning the highest its feet got and how many updates the flight took.
func flyKnocked(t *testing.T, h *hub, players map[int32]*tracked, m *mob) (peak float64, updates int) {
	t.Helper()
	peak = m.y
	for updates = 0; updates < 60 && m.kbFlight; updates++ {
		h.mobUpdate(players)
		h.tick.Add(mobMoveInterval)
		peak = math.Max(peak, m.y)
	}
	if m.kbFlight {
		t.Fatalf("the knocked zombie never came to rest: at (%.2f, %.2f) airborne %v", m.x, m.y, m.airborne)
	}
	return peak, updates
}

// vanillaKnockArc is LivingEntity.knockback then travel on flat ground,
// worked by hand: deltaMovement (v, hop) from a body on the ground, the
// first tick's drag the floor's (0.6 × 0.91, it was on the ground when it
// was hit), then 0.91 across and gravity with 0.98 down until it lands.
// It returns the peak rise and how far it has gone when it lands.
func vanillaKnockArc(v, hop float64) (peak, dist float64) {
	x, y, vx, vy, ground := 0.0, 0.0, v, hop, true
	for i := 0; i < 200; i++ {
		f := 0.91
		if ground {
			f = 0.6 * 0.91
		}
		x += vx
		y += vy
		ground = y <= 0
		if ground {
			y, vy = 0, 0
		}
		peak = math.Max(peak, y)
		vy = (vy - 0.08) * 0.98
		vx *= f
		if ground && i > 0 {
			return peak, x
		}
	}
	return peak, x
}

// LivingEntity.knockback sets deltaMovement, and travel carries it: a
// grounded zombie hit by a bare hand leaves the ground with vanilla's 0.4
// hop, rises a little over a block, and comes down where vanilla's arc puts
// it, under its goals again. The server's body used to stay on the floor and
// slide under a block.
func TestKnockbackLaunchesTheMob(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	x0, y0 := m.x, m.y
	h.attackMob(players, 1, m.eid)
	if !m.kbFlight || math.Abs(m.vy-0.4) > 1e-9 {
		t.Fatalf("a grounded zombie should be launched with the 0.4 hop: flight %v vy %.3f", m.kbFlight, m.vy)
	}
	if math.Abs(m.dmx-0.4) > 1e-9 || m.dmz != 0 {
		t.Fatalf("the flight's per-tick speed should be the blow's 0.4 away from the player, got (%.3f, %.3f)", m.dmx, m.dmz)
	}
	peak, updates := flyKnocked(t, h, players, m)
	wantPeak, wantDist := vanillaKnockArc(0.4, 0.4)
	if rise := peak - y0; math.Abs(rise-wantPeak) > 0.05 {
		t.Errorf("the hop should lift the zombie %.3f blocks, it rose %.3f", wantPeak, rise)
	}
	if d := m.x - x0; math.Abs(d-wantDist) > 0.2 {
		t.Errorf("a bare-hand knock carries a zombie %.3f blocks, it went %.3f", wantDist, d)
	}
	if m.y != y0 || m.airborne {
		t.Errorf("the zombie should be back on its floor: y %.3f (floor %.0f) airborne %v", m.y, y0, m.airborne)
	}
	if updates > 15 {
		t.Errorf("the flight should be over within a second or so, took %d updates", updates)
	}
	if m.kb != 0 {
		t.Errorf("the landed zombie should be back under its goals, kb %d", m.kb)
	}
}

// A wall stops a knocked body on the axis it meets it; it does not pick a
// random new heading as a walker does.
func TestKnockbackStopsAtAWall(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	for y := 180; y <= 183; y++ {
		h.world.SetBlock(44, y, 40, worldgen.Stone)
	}
	h.attackMob(players, 1, m.eid)
	for i := 0; i < 60 && m.kbFlight; i++ {
		h.mobUpdate(players)
		h.tick.Add(mobMoveInterval)
		if m.x >= 44 {
			t.Fatalf("update %d: the knocked zombie passed through the wall to x=%.3f", i, m.x)
		}
		if math.Abs(m.z-40.5) > 1e-9 {
			t.Fatalf("update %d: the knock was straight along x, but the zombie drifted to z=%.3f", i, m.z)
		}
	}
	if m.x <= 42 {
		t.Errorf("the zombie should have flown up to the wall, it is at x=%.3f", m.x)
	}
}

// A knock carries a body off a ledge — the walker's three-block drop rule
// is not a blow's business — and the fall counts: five blocks hurt.
func TestKnockbackCarriesOffALedge(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	for x := 43; x <= 60; x++ { // the edge a block and a half off: the arc lands past it
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(x, 179, 40+dz, worldgen.Air)
			h.world.SetBlock(x, 174, 40+dz, worldgen.Stone)
			for y := 175; y <= 178; y++ {
				h.world.SetBlock(x, y, 40+dz, worldgen.Air)
			}
		}
	}
	hp := m.health
	h.attackMob(players, 1, m.eid)
	afterHit := m.health
	flyKnocked(t, h, players, m)
	if m.y != 175 {
		t.Fatalf("the zombie should have been knocked off the ledge onto the floor below, y=%.3f x=%.3f", m.y, m.x)
	}
	if afterHit >= hp || m.health >= afterHit {
		t.Errorf("the fall of about six blocks should hurt: health %d after the hit, %d after landing", afterHit, m.health)
	}
}
