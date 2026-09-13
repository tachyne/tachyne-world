package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestGolemOffersPoppy: a golem beside a villager eventually holds out a
// poppy for four hundred ticks, standing still; alone, it never does.
func TestGolemOffersPoppy(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	g := h.spawnMob(players, entityIronGolem, 0.5, 180, 0.5)
	for i := 0; i < 20000; i++ {
		if h.golemOfferTick(players, g) {
			t.Fatal("no villager, no flower")
		}
	}
	h.spawnMob(players, entityVillager, 3.5, 180, 0.5)
	offered := false
	for i := 0; i < 40000 && !offered; i++ {
		offered = h.golemOfferTick(players, g)
	}
	if !offered || g.golemFlower != golemFlowerTicks {
		t.Fatalf("a golem beside a villager offers its poppy: %v %d", offered, g.golemFlower)
	}
	for i := 0; i < 200 && g.golemFlower > 0; i++ {
		if !h.golemOfferTick(players, g) && g.golemFlower > 0 {
			t.Fatal("held still while offering")
		}
	}
	if g.golemFlower != 0 {
		t.Fatal("and puts it away after four hundred ticks")
	}
}
