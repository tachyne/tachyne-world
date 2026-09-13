package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestZoglinAndEndermanHuntMobs: a zoglin goes for a pig but not a creeper;
// an enderman goes for an endermite.
func TestZoglinAndEndermanHuntMobs(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 60, 180, 60
	z := h.spawnMob(players, entityZoglin, 0.5, 180, 0.5)
	z.baby = false
	cr := h.spawnMob(players, entityCreeper, 3.5, 180, 0.5)
	h.gridDirty()
	h.mobHuntStep(players, z)
	if z.wolfPrey == cr.eid {
		t.Fatal("a zoglin leaves creepers alone")
	}
	pig := h.spawnMob(players, entityPig, 5.5, 180, 0.5)
	h.gridDirty()
	h.mobHuntStep(players, z)
	if z.wolfPrey != pig.eid {
		t.Fatalf("a zoglin goes for the pig: prey %d", z.wolfPrey)
	}
	hp := pig.health
	for i := 0; i < 200 && pig.health >= hp; i++ {
		h.mobHuntStep(players, z)
		z.x += z.vx
		z.z += z.vz
	}
	if pig.health >= hp {
		t.Fatal("and bites it")
	}
	en := h.spawnMob(players, entityEnderman, 0.5, 180, 20.5)
	mite := h.spawnMob(players, entityEndermite, 10.5, 180, 20.5)
	h.gridDirty()
	h.mobHuntStep(players, en)
	if en.wolfPrey != mite.eid {
		t.Fatalf("an enderman goes for the endermite: prey %d", en.wolfPrey)
	}
}
