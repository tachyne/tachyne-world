package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// breezeRig is open air over a stone floor at y=179, with a survival player
// at the origin looking +z.
func breezeRig(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	h.arrows = map[int32]*arrowEntity{}
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 16; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			for y := 180; y < 188; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.yaw, pl.pitch = 0.5, 180, 0.5, 0, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	return h, players, pl
}

func flyAll(h *hub, players map[int32]*tracked, ticks int) {
	for i := 0; i < ticks && len(h.arrows) > 0; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
}

// A breeze's wind charge bursts at 3.0 (BreezeWindCharge), and keeps that
// burst when a player bats it back.
func TestBattedBreezeChargeKeepsItsBurst(t *testing.T) {
	h, players, pl := breezeRig(t)
	for x := -3; x <= 3; x++ {
		for y := 180; y <= 183; y++ {
			h.world.SetBlock(x, y, 6, worldgen.BlockBase("stone"))
		}
	}
	by := survPlayer(h) // a bystander four blocks off the impact
	by.p.eid = pl.p.eid + 100
	by.x, by.y, by.z = 4.5, 180, 4.5
	players[by.p.eid] = by
	breeze := h.spawnMob(players, entityBreeze, 0.5, 180, -6)
	h.breezeFire(players, breeze, pl)
	a := onlyProjectile(t, h)
	a.x, a.y, a.z = 0.5, 181.5, 1.5 // just in front of the batter
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: a.eid})
	if a.shooter != pl.p.eid {
		t.Fatalf("the charge was not batted back (owner %d)", a.shooter)
	}
	flyAll(h, players, 20)
	if len(h.arrows) != 0 {
		t.Fatal("the charge never burst on the wall")
	}
	if by.launchCause != "wind_charge" {
		t.Error("the batted breeze charge's burst did not reach a bystander four blocks off: it burst at a player charge's 1.2")
	}
}

// A breeze's charge strikes the mobs in its way — but never another breeze
// (Breeze.isInvulnerableTo: whatever a breeze owns).
func TestBreezeChargeSparesBreezes(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.z = 10.5
	shooter := h.spawnMob(players, entityBreeze, 0.5, 180, -2)
	other := h.spawnMob(players, entityBreeze, 0.5, 180, 3.5)
	before := other.health
	h.breezeFire(players, shooter, pl)
	a := onlyProjectile(t, h)
	a.vx, a.vz = 0, 0.7 // straight down the line, no spread
	a.x = 0.5
	flyAll(h, players, 20)
	if len(h.arrows) != 0 {
		t.Fatal("the charge did not burst on the breeze in its way")
	}
	if other.health != before {
		t.Errorf("a breeze took %v from another breeze's charge", before-other.health)
	}

	h, players, pl = breezeRig(t)
	pl.z = 10.5
	shooter = h.spawnMob(players, entityBreeze, 0.5, 180, -2)
	z := h.spawnMob(players, entityZombie, 0.5, 180, 3.5)
	before = z.health
	h.breezeFire(players, shooter, pl)
	a = onlyProjectile(t, h)
	a.vx, a.vz = 0, 0.7
	a.x = 0.5
	flyAll(h, players, 20)
	if z.health >= before {
		t.Error("a breeze's charge flew through a zombie in its way")
	}
}

// Breeze.deflection: an arrow is turned back at half speed and does not land.
func TestBreezeTurnsArrowsBack(t *testing.T) {
	h, players, pl := breezeRig(t)
	breeze := h.spawnMob(players, entityBreeze, 0.5, 180, 5.5)
	before := breeze.health
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 181, 2, 0, 0, 1)
	a.shooter, a.dmg, a.playerShot, a.noHitUntil = pl.p.eid, 6, true, h.tick.Load()+100
	for i := 0; i < 6 && a.vz > 0; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if breeze.health != before {
		t.Fatalf("the arrow struck the breeze for %v", before-breeze.health)
	}
	if a.vz >= 0 || a.vz < -0.6 {
		t.Fatalf("the arrow should fly back at half speed, vz %.3f", a.vz)
	}
	if a.turnedBy != breeze.eid {
		t.Error("the breeze that turned it is not remembered")
	}
}

// Shoot.tick: a wind charge leaves the breeze's firing height at 0.7 blocks
// a tick. It was launched at 1.4, doubled as if projectiles stepped once per
// mob update, from a fixed block above the breeze's feet.
func TestBreezeChargeLeavesAtVanillaSpeed(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.z = 8.5
	b := h.spawnMob(players, entityBreeze, 0.5, 180, 0.5)
	h.breezeFire(players, b, pl)
	a := onlyProjectile(t, h)
	if sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz); sp < 0.6 || sp > 0.8 {
		t.Errorf("launched at %.3f blocks a tick, want 0.7", sp)
	}
	if want := b.y + b.box().h/2 + 0.3; math.Abs(a.y-want) > 1e-9 {
		t.Errorf("launched from y=%.3f, want the firing height %.3f", a.y, want)
	}
}
