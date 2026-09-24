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
	pl := survPlayer(h)
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, 0
	h.arrows = map[int32]*arrowEntity{}
	return h, map[int32]*tracked{pl.p.eid: pl}, pl
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
		{"snowball", func(h *hub, ps map[int32]*tracked, pl *tracked) { h.throwProjectile(ps, pl, itemSnowball) }, 0.03},
		{"egg", func(h *hub, ps map[int32]*tracked, pl *tracked) { h.throwProjectile(ps, pl, itemEgg) }, 0.03},
		{"ender pearl", func(h *hub, ps map[int32]*tracked, pl *tracked) { h.throwPearl(ps, pl) }, 0.03},
		{"bottle o' enchanting", func(h *hub, ps map[int32]*tracked, pl *tracked) { h.throwXPBottle(ps, pl) }, 0.07},
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
		{"bottle o' enchanting", 0.7, func(h *hub, ps map[int32]*tracked, pl *tracked) { h.throwXPBottle(ps, pl) }},
		{"lingering potion", 0.5, func(h *hub, ps map[int32]*tracked, pl *tracked) {
			pl.inv.slots[0] = invStack{item: itemLingerPotion, count: 1, potion: potPoison}
			h.throwSplashPotion(ps, pl, 0)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, players, pl := flightHub(t)
			c.throw(h, players, pl)
			a := onlyProjectile(t, h)
			if math.Abs(a.vy-c.pow*s/n) > 1e-6 || math.Abs(a.vz-c.pow/n) > 1e-6 || math.Abs(a.vx) > 1e-6 {
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
