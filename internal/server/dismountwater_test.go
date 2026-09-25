package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// dismounts_underwater: a player on a pig is thrown off once their eyes are
// in water, but not while their head is in a bubble column; a zombie riding
// a chicken is too, and a horse's rider in dry air stays put.
func TestDismountsUnderwater(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.allocEID()
	pig := h.spawnMob(players, entityPig, 0.5, 179.4, 0.5)
	if !h.forceRide(players, cmdEntity{t: pl}, pig) {
		t.Fatal("could not seat the player on the pig")
	}
	h.dismountUnderwater(players)
	if pl.ridingEID != pig.eid {
		t.Fatal("a rider in dry air was thrown off")
	}
	eye := int(pl.y + pl.eyeHeight())
	h.world.SetBlock(0, eye, 0, worldgen.BlockBase("bubble_column"))
	h.dismountUnderwater(players)
	if pl.ridingEID != pig.eid {
		t.Fatal("a rider with their head in a bubble column was thrown off")
	}
	h.world.SetBlock(0, eye, 0, worldgen.Water)
	h.dismountUnderwater(players)
	if pl.ridingEID != 0 || pig.rider != 0 {
		t.Fatal("a rider with their eyes in water stayed on the pig")
	}

	chicken := h.spawnMob(players, entityChicken, 5.5, 180, 5.5)
	z := h.spawnMob(players, entityZombie, 5.5, 180, 5.5)
	if !h.forceRide(players, cmdEntity{m: z}, chicken) {
		t.Fatal("could not seat the zombie on the chicken")
	}
	h.world.SetBlock(5, int(z.y+z.box().h*0.85), 5, worldgen.Water)
	h.dismountUnderwater(players)
	if z.mount != 0 {
		t.Error("a zombie with its eyes in water stayed on the chicken")
	}
}
