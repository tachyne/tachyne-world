package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestWanderingTrader: every listing names a real item; a spawn puts a
// trader with nine offers and two leashed llamas near the player; at night
// it drinks invisibility and at dawn milk; the clock despawns it.
func TestWanderingTrader(t *testing.T) {
	for _, pool := range traderPools {
		for _, l := range pool.listings {
			if itemByName[l.item] == 0 {
				t.Fatalf("unknown item %q", l.item)
			}
		}
	}
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, float64(h.world.SurfaceY(0, 0)), 0.5
	spawned := false
	for i := 0; i < 200 && !spawned; i++ {
		spawned = h.traderSpawn(players)
	}
	if !spawned {
		t.Fatal("one time in ten a trader should appear")
	}
	var tr *mob
	llamas := 0
	for _, m := range h.mobs {
		switch m.etype {
		case entityWanderingTrader:
			tr = m
		case entityTraderLlama:
			llamas++
		}
	}
	if tr == nil || len(tr.offers) != 9 || tr.traderDespawn != traderDespawnTicks {
		t.Fatalf("a trader with nine offers and its clock: %v", tr != nil)
	}
	if llamas == 0 {
		t.Fatal("its llamas come along")
	}
	for _, m := range h.mobs {
		if m.etype == entityTraderLlama && m.leash != tr.eid {
			t.Fatal("the llamas are leashed to the trader")
		}
	}
	h.dayTime.Store(15000)
	if !h.traderStep(players, tr) || tr.traderDrink != traderDrinkPotion {
		t.Fatalf("night: the invisibility potion: %d", tr.traderDrink)
	}
	for i := 0; i < 20 && tr.drinkTicks > 0; i++ {
		h.traderStep(players, tr)
	}
	if tr.hasEffect(effInvisibility) == 0 {
		t.Fatal("invisible once drunk")
	}
	h.dayTime.Store(3000)
	if !h.traderStep(players, tr) || tr.traderDrink != traderDrinkMilk {
		t.Fatalf("day: the milk: %d", tr.traderDrink)
	}
	for i := 0; i < 20 && tr.drinkTicks > 0; i++ {
		h.traderStep(players, tr)
	}
	if tr.hasEffect(effInvisibility) != 0 {
		t.Fatal("visible again after the milk")
	}
	tr.traderDespawn = 2
	h.traderStep(players, tr)
	if h.mobs[tr.eid] != nil {
		t.Fatal("the clock runs out and it leaves")
	}
}
