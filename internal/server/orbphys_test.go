package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// orbFixture is a stone floor at y=179 around (0, 0), high above the seed's
// terrain, with the chunks loaded, and a single orb of value 1 at (0.5, y, 0.5).
func orbFixture(t *testing.T, y float64) (*hub, *xpOrb) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	h.spawnXPOrb(map[int32]*tracked{}, 1, 0.5, y, 0.5)
	var o *xpOrb
	for _, x := range h.orbs {
		o = x
	}
	if o == nil {
		t.Fatal("no orb spawned")
	}
	return h, o
}

// An orb leaves the spot it was awarded at with the ExperienceOrb
// constructor's random hop: up to 0.2 either way sideways and 0–0.4 up.
func TestOrbSpawnsWithVanillaHop(t *testing.T) {
	_, o := orbFixture(t, 185)
	if math.Abs(o.vx) > 0.2 || math.Abs(o.vz) > 0.2 || o.vy < 0 || o.vy > 0.4 {
		t.Fatalf("spawn velocity (%.3f, %.3f, %.3f) outside the constructor's range", o.vx, o.vy, o.vz)
	}
	if o.x != 0.5 || o.y != 185 || o.z != 0.5 {
		t.Fatalf("the orb must appear where it was awarded, got (%.2f, %.2f, %.2f)", o.x, o.y, o.z)
	}
}

// An orb falls under its own gravity (0.03 a tick, 0.98 drag) rather than
// being set on the floor, bounces off it at 0.4 of a fast landing, and comes
// to rest on it.
func TestOrbFallsBouncesAndRests(t *testing.T) {
	h, o := orbFixture(t, 185)
	players := map[int32]*tracked{}
	o.vx, o.vy, o.vz = 0, 0, 0

	h.updateOrbs(players)
	if want := 185 - orbGravity; math.Abs(o.y-want) > 1e-9 {
		t.Fatalf("after one tick the orb should be at %.4f, got %.4f", want, o.y)
	}
	if want := -orbGravity * orbDrag; math.Abs(o.vy-want) > 1e-9 {
		t.Fatalf("after one tick vy should be %.5f, got %.5f", want, o.vy)
	}

	bounced := false
	for i := 0; i < 200; i++ {
		h.updateOrbs(players)
		if o.y < 180 {
			t.Fatalf("tick %d: the orb sank into the floor (y=%.3f)", i, o.y)
		}
		if o.y < 181 && o.vy > 0 {
			bounced = true
		}
	}
	if !bounced {
		t.Fatal("a six-block fall should bounce the orb off the floor")
	}
	if o.y != 180 || o.vy != 0 {
		t.Fatalf("the orb should come to rest on the floor, at y=%.4f vy=%.4f", o.y, o.vy)
	}
}

// A survival player within eight blocks pulls an orb toward the middle of
// their eyes by (1 − d/8)² × 0.1 a tick, and it is collected once it
// reaches them.
func TestOrbFollowsNearbyPlayer(t *testing.T) {
	h, o := orbFixture(t, 180)
	pl := testTracked()
	pl.x, pl.y, pl.z = 5.5, 180, 0.5
	players := map[int32]*tracked{1: pl}
	o.vx, o.vy, o.vz = 0, 0, 0

	dx, dy := pl.x-o.x, pl.y+pl.eyeHeight()/2-o.y
	d := math.Hypot(dx, dy)
	power := 1 - d/orbFollowRange
	wantVX := dx / d * power * power * orbFollowPull
	h.updateOrbs(players)
	if o.follow != 1 {
		t.Fatalf("the orb should follow the nearby player, follows %d", o.follow)
	}
	// The pull is applied before the move, the drag and the floor's
	// friction after it: this tick's move is the pull alone.
	if moved := o.x - 0.5; math.Abs(moved-wantVX) > 1e-9 {
		t.Fatalf("the first tick's pull should move the orb %.5f, got %.5f", wantVX, moved)
	}
	for i := 0; i < 300 && len(h.orbs) > 0; i++ {
		h.updateOrbs(players)
	}
	if len(h.orbs) != 0 || totalXP(pl.xpLevel, pl.xpPoints) != 1 {
		t.Fatalf("the orb should fly to the player and be collected: orbs=%d xp=%d",
			len(h.orbs), totalXP(pl.xpLevel, pl.xpPoints))
	}
}

// A player outside the eight-block range does not draw the orb at all.
func TestOrbIgnoresDistantPlayer(t *testing.T) {
	h, o := orbFixture(t, 180)
	pl := testTracked()
	pl.x, pl.y, pl.z = 9.5, 180, 0.5
	players := map[int32]*tracked{1: pl}
	o.vx, o.vy, o.vz = 0, 0, 0
	for i := 0; i < 20; i++ {
		h.updateOrbs(players)
	}
	if o.follow != 0 || o.x != 0.5 {
		t.Fatalf("an orb nine blocks off should stay put, follow=%d x=%.3f", o.follow, o.x)
	}
}
