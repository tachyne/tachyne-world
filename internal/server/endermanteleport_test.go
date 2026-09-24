package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestEndermanTeleportStaysNearItsHeight: Enderman.teleport picks a height
// within 32 of the enderman and drops to ground there; it never climbs to
// the surface, and never lands inside a block. Ours sent every blink to
// the top of the column, so endermen could not leave the sunlit surface
// (the daytime crowds) and cave endermen were pulled up into the open.
func TestEndermanTeleportStaysNearItsHeight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	w.ForceLoad(0, 0, 4)
	// A sealed room (y 145..149) under stone that runs up to y 160.
	for x := -40; x <= 40; x++ {
		for z := -40; z <= 40; z++ {
			for y := 140; y <= 160; y++ {
				st := worldgen.Stone
				if y >= 145 && y <= 149 && x > -38 && x < 38 && z > -38 && z < 38 {
					st = worldgen.Air
				}
				w.SetBlock(x, y, z, st)
			}
		}
	}
	m := h.spawnMob(players, entityEnderman, 0.5, 145, 0.5)
	if m == nil {
		t.Fatal("no enderman")
	}
	below := 0
	for i := 0; i < 300; i++ {
		m.x, m.y, m.z = 0.5, 145, 0.5
		if !h.endermanTeleport(players, m) {
			continue
		}
		for cy := int(math.Floor(m.y)); cy <= int(math.Floor(m.y+2.9-1e-7)); cy++ {
			if s := w.At(int(math.Floor(m.x)), cy, int(math.Floor(m.z))); worldgen.Collides(s) {
				t.Fatalf("landed inside a block at %.2f %.2f %.2f", m.x, m.y, m.z)
			}
		}
		if !worldgen.Collides(w.At(int(math.Floor(m.x)), int(math.Floor(m.y))-1, int(math.Floor(m.z)))) {
			t.Fatalf("landed with nothing under it at %.2f %.2f %.2f", m.x, m.y, m.z)
		}
		if m.y < 160 {
			below++
		}
	}
	if below == 0 {
		t.Fatal("every blink left the cave for the roof: the teleport went to the surface")
	}
}
