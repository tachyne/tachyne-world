package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A fed dolphin heads for the nearest shipwreck and forgets the errand on
// arrival; a dolphin with no wreck in reach just eats.
func TestDolphinLeadsToTreasure(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	g := h.worldFor(0).Gen()
	x, z, ok := g.NearestShipwreck(0, 0, 50*16*4) // a wide net: seed 1 has wrecks somewhere
	if !ok {
		t.Skip("no shipwreck within reach of the origin on this seed")
	}
	d := h.spawnSpecies(players, entityDolphin, 0, float64(x)+20, 62, float64(z))
	if d == nil {
		t.Fatal("no dolphin")
	}
	d.x, d.z = float64(x)+20, float64(z)
	cod := int32(itemByName["cod"])
	pl.inv.slots[0] = invStack{item: cod, count: 1}
	pl.p.setHotbarSlot(0, cod)
	pl.p.held = 0
	if !h.tryFeedDolphin(players, pl, d) || !d.gotFish || pl.inv.slots[0].item != 0 {
		t.Fatalf("a fish should set it off: gotFish=%v", d.gotFish)
	}
	if d.treasureX != x || d.treasureZ != z {
		t.Fatalf("it should aim at the nearest wreck (%d,%d), aims at (%d,%d)", x, z, d.treasureX, d.treasureZ)
	}
	if !h.dolphinStep(players, d) || d.vx >= 0 {
		t.Fatalf("it should swim west toward the wreck: vx=%.3f", d.vx)
	}
	d.x = float64(x) + 1
	if h.dolphinStep(players, d) || d.gotFish {
		t.Fatal("within four blocks the errand is done")
	}
}
