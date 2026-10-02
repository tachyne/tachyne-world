package server

import (
	"math"
	"testing"
)

// Every knock site stores its shove in the same units: vanilla's per-tick
// deltaMovement × mobMoveInterval in m.v*, so mobKnockVelocity reads back
// the vanilla figure. The push sites (mace, wind burst) and the knockback
// helpers used to store the per-tick figure raw, which read back at half.

// MaceItem.knockback: (3.5 − distance) × 0.7 a tick, added to the motion.
func TestMaceShockwaveShovesInTickUnits(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	cow := putMob(t, h, players, entityCow, 2, 70, 0)
	cow.vx, cow.vz = 0, 0
	h.smashAround(players, attacker, 0, 70, 0, 9999, 2)
	want := (maceKnockRadius - 2) * maceKnockPower * cow.kbScale()
	if got := cow.vx / mobMoveInterval; math.Abs(got-want) > 1e-9 {
		t.Errorf("the shockwave shoved %.4f a tick, want %.4f", got, want)
	}
	if cow.kbFlight && math.Abs(cow.kvx-want) > 1e-9 {
		t.Errorf("the flight carries %.4f a tick, want %.4f", cow.kvx, want)
	}
}

// A wind burst pushes (1 − d/2r) × 1.22 a tick, on top of what the mob had.
func TestWindBurstPushesInTickUnits(t *testing.T) {
	h, players := maceHub(t)
	cow := putMob(t, h, players, entityCow, 1, 70, 0)
	cow.vx, cow.vz = 0, 0
	const radius = 1.2
	h.windPush(players, 0, 0, 70, 0, radius)
	want := (1 - 1/(radius*2)) * windChargeKnockback * cow.kbScale()
	if got := cow.vx / mobMoveInterval; math.Abs(got-want) > 1e-9 {
		t.Errorf("the burst pushed %.4f a tick, want %.4f", got, want)
	}
}

// /damage's default knockback on a mob is LivingEntity.knockback(0.4).
func TestDamageCommandKnocksMobInTickUnits(t *testing.T) {
	h, players := maceHub(t)
	cow := putMob(t, h, players, entityCow, 5.5, 70, 0.5)
	cow.vx, cow.vz, cow.spawnInvuln = 0, 0, 0
	src := cmdDamageSource{pos: true, x: 4.5, z: 0.5}
	if !h.commandHurt(players, cmdEntity{m: cow}, 1, dtGeneric, src) {
		t.Fatal("the blow did not land")
	}
	want := 0.4 * cow.kbScale()
	if got := cow.vx / mobMoveInterval; math.Abs(got-want) > 1e-9 {
		t.Errorf("knocked %.4f a tick, want %.4f", got, want)
	}
}

// An iron golem's punch: the blow's 0.4 knockback, not a sideways 0.6.
func TestGolemPunchKnocksInTickUnits(t *testing.T) {
	h, players := maceHub(t)
	g := putMob(t, h, players, entityIronGolem, 0.5, 70, 0.5)
	g.behavior = golemBehavior{}
	z := putMob(t, h, players, entityZombie, 1.5, 70, 0.5)
	z.hostile, z.health, z.vx, z.vz = true, 100, 0, 0
	h.golemMelee(players, g)
	if z.kb == 0 {
		t.Fatal("the golem's punch did not knock the zombie")
	}
	want := 0.4 * z.kbScale()
	if got := z.vx / mobMoveInterval; math.Abs(got-want) > 1e-9 {
		t.Errorf("knocked %.4f a tick, want %.4f", got, want)
	}
}
