package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Fireballs are self-propelled: no gravity, a push along the flight each
// tick and inertia, so one launched at its 0.1 works up toward 1.9 a tick
// on a level line. A wind charge coasts at the speed it left with.
func TestHurtingProjectileMotion(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(40, 0, 3) // the fireball's whole flight is in loaded chunks
	players := map[int32]*tracked{}
	fb := h.launchProjectileIn(players, entityLargeFireball, 0, 0, 200, 0, hurtingSpeed, 0, 0)
	wc := h.launchProjectileIn(players, entityWindCharge, 0, 0, 200, 0, 0.7, 0, 0)
	ar := h.launchProjectileIn(players, entityArrow, 0, 0, 200, 0, 0.7, 0, 0)
	for i := 0; i < 40; i++ {
		h.updateArrows(players)
	}
	if fb.y != 200 || fb.vy != 0 {
		t.Fatalf("a fireball has no gravity, y=%.2f vy=%.3f", fb.y, fb.vy)
	}
	if fb.vx < 1.5 || fb.vx > 1.9 {
		t.Fatalf("a fireball works up toward 1.9 a tick, vx=%.3f after 40 ticks", fb.vx)
	}
	if math.Abs(wc.vx-0.7) > 1e-9 || wc.y != 200 {
		t.Fatalf("a wind charge coasts, vx=%.3f y=%.2f", wc.vx, wc.y)
	}
	if ar.y >= 200 || ar.vx >= 0.7 {
		t.Fatalf("an arrow still falls and slows, y=%.2f vx=%.3f", ar.y, ar.vx)
	}
}
