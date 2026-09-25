package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// flightHub is a hub with one creative thrower (projectiles pass through
// creative players) standing in open air at y=180, facing +z on the level.
func flightHub(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2) // loaded ground: ageless projectiles end at the loaded edge
	pl := survPlayer(h)
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, 0
	h.arrows = map[int32]*arrowEntity{}
	return h, map[int32]*tracked{pl.p.eid: pl}, pl
}

// hold puts a stack of item in the selected hotbar slot: a use throws
// what is in the hand used.
func hold(pl *tracked, item int32) {
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: item, count: 16}
	pl.p.setHotbarSlot(pl.p.heldSlot(), item)
}

func onlyProjectile(t *testing.T, h *hub) *arrowEntity {
	t.Helper()
	if len(h.arrows) != 1 {
		t.Fatalf("want one projectile in the air, have %d", len(h.arrows))
	}
	for _, a := range h.arrows {
		return a
	}
	return nil
}

const flightEps = 1e-9

// Each thrown projectile falls on its own getDefaultGravity, integrated
// the ThrowableProjectile way: gravity, then 0.99 inertia, then the move.
func TestThrownProjectilesFallOnTheirOwnGravity(t *testing.T) {
	cases := []struct {
		name  string
		throw func(h *hub, players map[int32]*tracked, pl *tracked)
		grav  float64
	}{
		{"snowball", func(h *hub, ps map[int32]*tracked, pl *tracked) {
			hold(pl, itemSnowball)
			h.throwProjectile(ps, pl, itemSnowball)
		}, 0.03},
		{"egg", func(h *hub, ps map[int32]*tracked, pl *tracked) {
			hold(pl, itemEgg)
			h.throwProjectile(ps, pl, itemEgg)
		}, 0.03},
		{"ender pearl", func(h *hub, ps map[int32]*tracked, pl *tracked) { hold(pl, itemEnderPearl); h.throwPearl(ps, pl) }, 0.03},
		{"bottle o' enchanting", func(h *hub, ps map[int32]*tracked, pl *tracked) { hold(pl, itemXPBottle); h.throwXPBottle(ps, pl) }, 0.07},
		{"splash potion", func(h *hub, ps map[int32]*tracked, pl *tracked) {
			pl.inv.slots[0] = invStack{item: itemSplashPotion, count: 1, potion: potPoison}
			h.throwSplashPotion(ps, pl, 0)
		}, 0.05},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, players, pl := flightHub(t)
			c.throw(h, players, pl)
			a := onlyProjectile(t, h)
			y0, vy0, vz0 := a.y, a.vy, a.vz
			h.updateArrows(players)
			wantVY := (vy0 - c.grav) * 0.99
			if math.Abs(a.vy-wantVY) > flightEps {
				t.Fatalf("vy after one tick %.6f, want %.6f (gravity %.2f)", a.vy, wantVY, c.grav)
			}
			// It moves along the already-integrated velocity.
			if math.Abs(a.y-(y0+wantVY)) > flightEps || math.Abs(a.vz-vz0*0.99) > flightEps {
				t.Fatalf("stepped to y %.6f vz %.6f, want %.6f / %.6f", a.y, a.vz, y0+wantVY, vz0*0.99)
			}
		})
	}
}

// ExperienceBottleItem and ThrowablePotionItem throw with a −20° lift at
// power 0.7 and 0.5: a level look sends them up, not flat.
func TestBottleAndPotionThrowsLiftTwentyDegrees(t *testing.T) {
	// shootFromRotation at xRot 0, yOffset −20: (0, sin 20°, 1) normalized.
	s := math.Sin(20 * math.Pi / 180)
	n := math.Sqrt(1 + s*s)
	for _, c := range []struct {
		name  string
		pow   float64
		throw func(h *hub, ps map[int32]*tracked, pl *tracked)
	}{
		{"bottle o' enchanting", 0.7, func(h *hub, ps map[int32]*tracked, pl *tracked) { hold(pl, itemXPBottle); h.throwXPBottle(ps, pl) }},
		{"lingering potion", 0.5, func(h *hub, ps map[int32]*tracked, pl *tracked) {
			pl.inv.slots[0] = invStack{item: itemLingerPotion, count: 1, potion: potPoison}
			h.throwSplashPotion(ps, pl, 0)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, players, pl := flightHub(t)
			c.throw(h, players, pl)
			a := onlyProjectile(t, h)
			// The throw's uncertainty (1.0) nudges each axis by up to
			// 0.0172275 before the power scales it.
			tol := 0.0172275*c.pow + 1e-9
			if math.Abs(a.vy-c.pow*s/n) > tol || math.Abs(a.vz-c.pow/n) > tol || math.Abs(a.vx) > tol {
				t.Fatalf("launch (%.4f, %.4f, %.4f), want (0, %.4f, %.4f)", a.vx, a.vy, a.vz, c.pow*s/n, c.pow/n)
			}
		})
	}
}

// An arrow drags first and then falls (AbstractArrow.tick), after its move.
func TestArrowDragsThenFalls(t *testing.T) {
	h, players, _ := flightHub(t)
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 180, 0.5, 0, 0.5, 1)
	h.updateArrows(players)
	if want := 0.5*0.99 - 0.05; math.Abs(a.vy-want) > flightEps {
		t.Fatalf("arrow vy %.6f, want %.6f", a.vy, want)
	}
	if math.Abs(a.y-180.5) > flightEps {
		t.Fatalf("arrow moved to y %.6f on its launch velocity, want 180.5", a.y)
	}
}

// Llama spit falls at 0.06; a shulker bullet whose target is gone falls at 0.04.
func TestSpitAndOrphanBulletGravity(t *testing.T) {
	h, players, _ := flightHub(t)
	spit := h.launchProjectileIn(players, entityLlamaSpit, 0, 0.5, 180, 0.5, 0, 0, 1)
	bullet := h.launchProjectileIn(players, entityShulkerBullet, 0, 10.5, 180, 0.5, 0, 0, 0.4)
	bullet.homing = 9999 // nobody by that id: the target is gone
	h.updateArrows(players)
	if want := -0.06; math.Abs(spit.vy-want) > flightEps {
		t.Fatalf("spit vy %.6f, want %.6f", spit.vy, want)
	}
	if want := -0.04; math.Abs(bullet.vy-want) > flightEps {
		t.Fatalf("orphan bullet vy %.6f, want %.6f", bullet.vy, want)
	}
}

// Projectile.shootFromRotation: a throw carries the thrower's own movement —
// all of it in the air, only the horizontal part on the ground — and
// getMovementToShoot scatters it by the throw's uncertainty.
func TestThrowsCarryTheThrowersMotionAndScatter(t *testing.T) {
	for _, c := range []struct {
		name     string
		onGround bool
		wantVY   float64
	}{{"on the ground", true, 0}, {"in the air", false, 0.4}} {
		t.Run(c.name, func(t *testing.T) {
			h, players, pl := flightHub(t)
			h.tick.Store(100)
			pl.onGround = c.onGround
			h.noteKnownMove(pl, 0.3, 0.4, 0) // strafing east while moving up
			hold(pl, itemSnowball)
			h.throwProjectile(players, pl, itemSnowball)
			a := onlyProjectile(t, h)
			tol := 0.0172275*throwSpeed + 1e-9
			if math.Abs(a.vx-0.3) > tol || math.Abs(a.vy-c.wantVY) > tol || math.Abs(a.vz-throwSpeed) > tol {
				t.Fatalf("snowball launched (%.3f, %.3f, %.3f), want about (0.3, %.1f, 1.5)", a.vx, a.vy, a.vz, c.wantVY)
			}
		})
	}

	h, players, pl := flightHub(t)
	seen := map[[3]float64]bool{}
	for i := 0; i < 20; i++ {
		h.arrows = map[int32]*arrowEntity{}
		hold(pl, itemSnowball)
		h.throwProjectile(players, pl, itemSnowball)
		a := onlyProjectile(t, h)
		seen[[3]float64{a.vx, a.vy, a.vz}] = true
	}
	if len(seen) < 10 {
		t.Errorf("20 snowballs flew only %d distinct lines: no throw scatter", len(seen))
	}
}

// ProjectileDispenseBehavior: a dispensed snowball leaves 0.7 out of the face
// and 0.1 up at power 1.1; a thrown potion at 1.375 with half the scatter.
func TestDispenserShotPowerAndMouth(t *testing.T) {
	for _, c := range []struct {
		item int32
		pow  float64
		unc  float64
	}{{itemSnowball, 1.1, 6}, {itemSplashPotion, 1.375, 3}} {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 1)
		h.arrows = map[int32]*arrowEntity{}
		state := eastDispenser(t)
		pos := blockPos{5, 180, 5}
		h.world.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: c.item, count: 1, potion: potPoison}
		h.bins[simPos{blockPos: pos}] = b
		h.ejectFromBin(map[int32]*tracked{}, simPos{blockPos: pos}, state)
		a := onlyProjectile(t, h)
		if math.Abs(a.x-6.2) > 1e-9 || math.Abs(a.y-180.6) > 1e-9 || math.Abs(a.z-5.5) > 1e-9 {
			t.Errorf("item %d left from (%.2f, %.2f, %.2f), want (6.2, 180.6, 5.5)", c.item, a.x, a.y, a.z)
		}
		tol := 0.0172275*c.unc*c.pow + 1e-9
		if math.Abs(a.vx-c.pow) > tol || math.Abs(a.vy) > tol || math.Abs(a.vz) > tol {
			t.Errorf("item %d launched (%.3f, %.3f, %.3f), want about (%.3f, 0, 0)", c.item, a.vx, a.vy, a.vz, c.pow)
		}
	}
}
