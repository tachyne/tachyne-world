package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSheepDoesNotWalkIntoATallWall (bug #32): a column buried more than
// eight deep made the step rule read a wall as a flat step, so a sheep
// wandered into a player's tall wall and was drawn black inside it.
func TestSheepDoesNotWalkIntoATallWall(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 192; y++ { // a wall twelve high along x = 2
				st := worldgen.Air
				if x == 2 {
					st = worldgen.BlockBase("poplar_planks")
				}
				w.SetBlock(x, y, z, st)
			}
		}
	}
	pl.x, pl.y, pl.z = -4.5, 180, 0.5
	s := h.spawnMob(players, entitySheep, 1.5, 180, 0.5)
	if s == nil {
		t.Fatal("no sheep")
	}
	if h.mobStepOK(s, 2.5, 0.5) {
		t.Fatal("the step rule lets the sheep into the wall")
	}
	for i := 0; i < 400; i++ {
		s.vx, s.vz = s.moveSpeed(), 0 // keep walking at the wall
		s.reroute = 0
		h.updateMobs(players)
		if int(math.Floor(s.x)) == 2 {
			t.Fatalf("tick %d: the sheep walked into the wall at x=%.2f y=%.2f", i, s.x, s.y)
		}
	}
	// A sheep already wedged in the wall may still walk out of it.
	s.x, s.z = 2.5, 0.5
	if !h.mobStepOK(s, 1.5, 0.5) {
		t.Error("a wedged sheep cannot leave the wall")
	}
}
