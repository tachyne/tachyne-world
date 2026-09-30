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
		h.updateMobs(players)
		h.tick.Add(mobMoveInterval)
		peak = math.Max(peak, m.y)
	}
	if m.kbFlight {
		t.Fatalf("the knocked zombie never came to rest: at (%.2f, %.2f) airborne %v", m.x, m.y, m.airborne)
	}
	return peak, updates
}

// LivingEntity.knockback sets deltaMovement, and travel carries it: a
// grounded zombie hit by a bare hand leaves the ground with vanilla's 0.4
// hop, rises a little over a block, and comes down about three blocks off
// (0.4 a tick kept at 0.91 a tick in the air, then the floor's friction).
// The server's body used to stay on the floor and slide under a block.
func TestKnockbackLaunchesTheMob(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	x0, y0 := m.x, m.y
	h.attackMob(players, 1, m.eid)
	if !m.kbFlight || !m.airborne || math.Abs(m.vy-0.4) > 1e-9 {
		t.Fatalf("a grounded zombie should be launched with the 0.4 hop: flight %v airborne %v vy %.3f", m.kbFlight, m.airborne, m.vy)
	}
	if math.Abs(m.kvx-0.4) > 1e-9 || m.kvz != 0 {
		t.Fatalf("the flight's per-tick speed should be the blow's 0.4 away from the player, got (%.3f, %.3f)", m.kvx, m.kvz)
	}
	peak, updates := flyKnocked(t, h, players, m)
	if rise := peak - y0; rise < 1.0 || rise > 1.25 {
		t.Errorf("the hop should lift the zombie about 1.15 blocks, it rose %.3f", rise)
	}
	if d := m.x - x0; d < 2.8 || d > 3.8 {
		t.Errorf("a bare-hand knock carries a zombie about three blocks, it went %.3f", d)
	}
	if m.y != y0 || m.airborne {
		t.Errorf("the zombie should be back on its floor: y %.3f (floor %.0f) airborne %v", m.y, y0, m.airborne)
	}
	if updates > 15 {
		t.Errorf("the flight and the skid should be over within a second or so, took %d updates", updates)
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
		h.updateMobs(players)
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
	for x := 44; x <= 60; x++ {
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
