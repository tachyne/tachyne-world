package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A spider two to four blocks from its target springs at it and lands
// closer; too close or too far, it never leaps.
func TestSpiderLeapsAtTarget(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -4; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 3.5, 180, 0.5
	s := h.spawnMob(players, entitySpider, 0.5, 180, 0.5)
	s.hostile = true
	s.hasTarget, s.tx, s.tz = true, pl.x, pl.z
	leapt := false
	for i := 0; i < 200 && !leapt; i++ {
		h.leapCheck(players, s)
		leapt = s.leaping
	}
	if !leapt {
		t.Fatal("the spider never sprang at a target three blocks off")
	}
	if s.leapVY != 0.4 || s.leapVX <= 0 {
		t.Errorf("spring %.2f %.2f %.2f", s.leapVX, s.leapVY, s.leapVZ)
	}
	for i := 0; i < 60 && s.leaping; i++ {
		h.leapFlight(players, s)
	}
	if s.leaping || s.y != 180 {
		t.Errorf("still airborne: leaping %v y %.2f", s.leaping, s.y)
	}
	if s.x < 1.5 {
		t.Errorf("the spring carried it only to x=%.2f", s.x)
	}
	// Nose to nose it bites rather than jumps; far off it walks.
	for _, x := range []float64{1.0, 8.5} {
		s.x, s.tx = 0.5, x
		pl.x = x
		for i := 0; i < 200; i++ {
			h.leapCheck(players, s)
			if s.leaping {
				t.Fatalf("sprang at a target %.1f blocks off", x-0.5)
			}
		}
	}
	// A zombie has no such goal.
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	z.hostile, z.hasTarget, z.tx, z.tz = true, true, 3.5, 0.5
	for i := 0; i < 200; i++ {
		h.leapCheck(players, z)
	}
	if z.leaping {
		t.Error("a zombie leapt")
	}
}
