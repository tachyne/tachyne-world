package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A shulker bullet must curve toward its target (homing) and not fall — vanilla
// ShulkerBullet steers its motion each tick and has no gravity.
func TestShulkerBulletHomes(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 20, 80, 0 // target off to the +x
	players := map[int32]*tracked{1: pl}
	h.tick.Store(100)

	// Fire from the origin moving +z (perpendicular to the target), homing on it.
	a := h.launchProjectileIn(players, entityShulkerBullet, 0, 0, 80, 0, 0, 0, shulkerBulletSpeed)
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
	a := h.launchProjectileIn(players, entityShulkerBullet, 0, 4, 80, 0, shulkerBulletSpeed, 0, 0)
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
