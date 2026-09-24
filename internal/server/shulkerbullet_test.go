package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A shulker bullet must work its way toward its target (homing) and not
// fall — vanilla ShulkerBullet steers its motion each tick and has no gravity.
func TestShulkerBulletHomes(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 20, 80, 0 // target off to the +x
	players := map[int32]*tracked{1: pl}
	for x := -1; x <= 21; x++ { // open air along the way
		for y := 79; y <= 82; y++ {
			h.world.SetBlock(x, y, 0, worldgen.Air)
		}
	}
	h.tick.Store(100)

	// Fire from the origin moving +z (perpendicular to the target), homing on it.
	a := h.launchProjectileIn(players, entityShulkerBullet, 0, 0, 80, 0, 0, 0, shulkerBulletStep)
	a.shooter, a.dmg, a.breaks, a.homing, a.levitate = 999, 4, true, pl.p.eid, 10

	for i := 0; i < 6; i++ {
		h.updateArrows(players)
	}
	if a.vx <= 0 {
		t.Fatalf("shulker bullet should curve toward the target (+x): vx=%.3f", a.vx)
	}
	if a.vy < -0.05 {
		t.Fatalf("shulker bullet should not fall (no gravity): vy=%.3f", a.vy)
	}
}

// A shulker bullet hit inflicts Levitation (vanilla LEVITATION I, 10 s).
func TestShulkerBulletLevitatesOnHit(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 5, 80, 0
	players := map[int32]*tracked{1: pl}
	h.tick.Store(100)

	// Right beside the player, driving into it.
	a := h.launchProjectileIn(players, entityShulkerBullet, 0, 4, 80, 0, shulkerBulletStep, 0, 0)
	a.shooter, a.dmg, a.breaks, a.homing, a.levitate = 999, 4, true, pl.p.eid, 10

	for i := 0; i < 6 && len(h.arrows) > 0; i++ {
		h.updateArrows(players)
	}
	if len(h.arrows) != 0 {
		t.Fatal("the bullet should have struck the player and been removed")
	}
	if pl.hasEffect(effLevitation) == 0 {
		t.Fatal("a shulker bullet hit should apply Levitation")
	}
}

// shulkerBulletRig is a shulker and a survival player on a stone floor at
// y=179 in open air, the player eight blocks off on both horizontal axes.
func shulkerBulletRig(t *testing.T) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(4, 4, 2)
	h.arrows = map[int32]*arrowEntity{}
	for x := -4; x <= 12; x++ {
		for z := -4; z <= 12; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			for y := 180; y < 190; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 8.5, 180, 8.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnMobIn(players, entityShulker, 0, 0.5, 180, 0.5)
	return h, players, pl, m
}

// ShulkerBullet flies in legs along one axis at a time, from rest, not in a
// smooth curve straight at its victim — and it still gets there.
func TestShulkerBulletFliesAxisByAxis(t *testing.T) {
	h, players, pl, m := shulkerBulletRig(t)
	h.shulkerFire(players, m, pl)
	a := onlyProjectile(t, h)
	if a.vx != 0 || a.vy != 0 || a.vz != 0 {
		t.Fatalf("the bullet should leave at rest, got (%.3f, %.3f, %.3f)", a.vx, a.vy, a.vz)
	}
	if a.bullet.dir == shulkerBulletNoDir || bulletDirAxis(a.bullet.dir) == axisY {
		t.Fatalf("the first leg %d should be horizontal: a floor shulker's shot never starts along Y", a.bullet.dir)
	}
	for i := 0; i < 4; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	axes := 0
	for _, v := range []float64{a.vx, a.vy, a.vz} {
		if math.Abs(v) > 1e-9 {
			axes++
		}
	}
	if axes != 1 {
		t.Fatalf("four ticks into its first leg the bullet moves along %d axes (%.3f, %.3f, %.3f), want one",
			axes, a.vx, a.vy, a.vz)
	}
	for i := 0; i < 400 && len(h.arrows) > 0; i++ {
		h.tick.Add(1)
		if a.born+arrowLifeTicks <= h.tick.Load() {
			a.born = h.tick.Load() // this test is about the course, not the litter timer
		}
		h.updateArrows(players)
	}
	if len(h.arrows) != 0 || pl.hasEffect(effLevitation) == 0 {
		t.Fatalf("the bullet never reached the player (still flying: %d, at %.1f %.1f %.1f)", len(h.arrows), a.x, a.y, a.z)
	}
}

// ShulkerBullet.hurtServer: a punch destroys a bullet (it is not deflected).
func TestPunchDestroysShulkerBullet(t *testing.T) {
	h, players, pl, m := shulkerBulletRig(t)
	h.shulkerFire(players, m, pl)
	a := onlyProjectile(t, h)
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: a.eid})
	if len(h.arrows) != 0 {
		t.Fatal("the punched bullet is still flying")
	}
}
