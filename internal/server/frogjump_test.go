package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// FrogAi LONG_JUMP: a frog on a pillar with another pillar three blocks
// off across a drop cannot walk over, so within a few cooldowns it leaps
// it and lands on top.
func TestFrogLongJumpsAGap(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	w := h.world
	for x := -6; x <= 8; x++ {
		for y := 170; y <= 185; y++ {
			for z := -6; z <= 6; z++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	for y := 170; y <= 179; y++ {
		w.SetBlock(0, y, 0, worldgen.Stone)
		w.SetBlock(3, y, 0, worldgen.Stone)
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 200, 12.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	f := h.spawnSpecies(players, entityFrog, 0, 0.5, 180, 0.5)
	if f == nil {
		t.Fatal("no frog")
	}
	f.baby = false
	jumped := false
	for i := 0; i < 1500 && !jumped; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		jumped = !f.goatJumping && floorInt(f.x) == 3 && floorInt(f.z) == 0 && f.y == 180
		if f.y < 179 {
			t.Fatalf("the frog fell off: (%.2f, %.2f, %.2f)", f.x, f.y, f.z)
		}
	}
	if !jumped {
		t.Fatalf("the frog never leapt to the other pillar: at (%.2f, %.2f, %.2f)", f.x, f.y, f.z)
	}
}
