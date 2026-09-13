package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestGoatLongJumps: a goat on a ledge with a gap to another crouches and
// jumps it; on flat ground it finds nothing to jump.
func TestGoatLongJumps(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -12; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			for y := 176; y <= 190; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
			if x >= -2 && x <= 1 || x >= 4 && x <= 8 { // two ledges, a three-block gap
				w.SetBlock(x, 179, z, worldgen.Stone)
			}
		}
	}
	g := h.spawnMob(players, entityGoat, 0.5, 180, 0.5)
	g.goatJumpSet, g.goatJumpCD = true, 0
	jumped := false
	for i := 0; i < 4000 && !jumped; i++ {
		h.goatJumpStep(players, g)
		jumped = g.goatJumping
		if g.goatJumpCD > 0 {
			g.goatJumpCD = 0 // keep looking: the test cares about the jump, not the wait
		}
	}
	if !jumped || g.goatVY <= 0 {
		t.Fatalf("the goat should jump the gap: jumping %v vy %.2f target %.1f,%.1f", jumped, g.goatVY, g.goatJumpX, g.goatJumpZ)
	}
	for i := 0; i < 200 && g.goatJumping; i++ {
		h.goatFlight(players, g)
	}
	if g.goatJumping || g.y != 180 || g.goatJumpCD < goatJumpCDMin {
		t.Fatalf("it lands with a fresh cooldown: jumping %v y %.1f cd %d", g.goatJumping, g.y, g.goatJumpCD)
	}
	// Flat ground: nothing worth a jump.
	for x := -12; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	flat := h.spawnMob(players, entityGoat, 0.5, 180, 0.5)
	flat.goatJumpSet, flat.goatJumpCD = true, 0
	for i := 0; i < 20; i++ {
		flat.goatJumpCD = 0
		if h.goatJumpStep(players, flat) {
			t.Fatal("on flat ground there is nothing to jump")
		}
	}
}
