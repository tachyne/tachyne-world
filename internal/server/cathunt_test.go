package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestCatsHunt: a wild cat goes for a rabbit, a tamed one does not; an
// ocelot goes for a chicken.
func TestCatsHunt(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 40, 180, 40
	c := h.spawnMob(players, entityCat, 0.5, 180, 0.5)
	c.tamed = false
	r := h.spawnMob(players, entityRabbit, 5.5, 180, 0.5)
	h.gridDirty()
	for i := 0; i < 100 && c.wolfPrey == 0; i++ {
		h.catHuntStep(players, c)
	}
	if c.wolfPrey != r.eid {
		t.Fatalf("a wild cat hunts the rabbit: prey %d", c.wolfPrey)
	}
	c.tamed = true
	h.catHuntStep(players, c)
	if c.wolfPrey != 0 {
		t.Fatal("a tamed cat does not")
	}
	o := h.spawnMob(players, entityOcelot, 0.5, 180, 10.5)
	ch := h.spawnMob(players, entityChicken, 4.5, 180, 10.5)
	h.gridDirty()
	for i := 0; i < 100 && o.wolfPrey == 0; i++ {
		h.catHuntStep(players, o)
	}
	if o.wolfPrey != ch.eid {
		t.Fatalf("an ocelot hunts the chicken: prey %d", o.wolfPrey)
	}
}
