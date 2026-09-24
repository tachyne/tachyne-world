package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A zoglin goes for the closest living thing, a pet included, and does
// not drop a mob it is already on for a player that walks up.
func TestZoglinTakesTheClosest(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	z := h.spawnMob(players, entityZoglin, 0.5, 180, 0.5)
	wolf := h.spawnMob(players, entityWolf, 3.5, 180, 0.5)
	wolf.tamed = true
	if !mobHuntPrey(z, wolf) {
		t.Fatal("a zoglin spares a tamed wolf")
	}
	pl.x, pl.y, pl.z = 10.5, 180, 0.5
	z.hasTarget = true
	if !h.mobHuntStep(players, z) || z.wolfPrey != wolf.eid {
		t.Fatal("the zoglin chose the farther player over the nearer wolf")
	}
	pl.x = 1.5 // now the player is closer, but the zoglin keeps its target
	if !h.mobHuntStep(players, z) || z.wolfPrey != wolf.eid {
		t.Error("the zoglin dropped its wolf for a player")
	}
}
