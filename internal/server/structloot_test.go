package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestDesertTempleChestLoots(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	g := w.Gen()
	// Find a temple in this seed.
	var found bool
	var chestPos blockPos
	for cx := 0; cx < 24000 && !found; cx += 336 {
		for cz := 0; cz < 24000; cz += 336 {
			d := g.DesertTempleIn(cx+168, cz+168)
			if d.Exists {
				c := d.Chests()[0]
				chestPos = blockPos{c[0], c[1], c[2]}
				found = true
				break
			}
		}
	}
	if !found {
		t.Skip("no desert temple in range")
	}
	c := &chest{}
	h.chests[chestPos] = c
	h.desertTempleLoot(chestPos, c)
	items := 0
	for _, s := range c.slots {
		if s.item != 0 {
			items++
		}
	}
	if items == 0 {
		t.Fatal("a desert-temple chest should hold loot")
	}
}
