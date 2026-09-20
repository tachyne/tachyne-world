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

// The wandering trader's pools, against vanilla's WANDERING_TRADER_TRADES.
// Three of the buy listings had maxUses and villagerXp swapped, so a water
// bucket could be sold once for double experience instead of twice for one;
// and the two listings that carry a component — the enchanted iron pickaxe and
// the bottle of Invisibility — were left out of the rare pool entirely.
func TestTraderPoolsMatchVanilla(t *testing.T) {
	buys := map[string]traderListing{}
	for _, l := range traderPools[0].listings {
		buys[l.item] = l
	}
	for _, tc := range []struct {
		item                     string
		cost, count, maxUses, xp int32
	}{
		{"water_bucket", 1, 2, 2, 1},
		{"milk_bucket", 1, 2, 2, 1},
		{"fermented_spider_eye", 1, 3, 2, 1},
		{"baked_potato", 4, 1, 2, 1},
		{"hay_block", 1, 1, 2, 1},
	} {
		l, ok := buys[tc.item]
		if !ok {
			t.Errorf("%s is missing from the buy pool", tc.item)
			continue
		}
		if !l.buy || l.cost != tc.cost || l.count != tc.count || l.maxUses != tc.maxUses || l.xp != tc.xp {
			t.Errorf("%s is %d→%d, %d uses, %d xp; want %d→%d, %d uses, %d xp",
				tc.item, l.cost, l.count, l.maxUses, l.xp, tc.cost, tc.count, tc.maxUses, tc.xp)
		}
	}

	// The rare pool's two component-carrying listings.
	var pick, potion *traderListing
	for i, l := range traderPools[1].listings {
		switch {
		case l.kind == vTradeEnchantedGear:
			pick = &traderPools[1].listings[i]
		case l.potion != 0:
			potion = &traderPools[1].listings[i]
		}
	}
	if pick == nil || pick.item != "iron_pickaxe" || pick.mult100 != 20 {
		t.Errorf("the rare pool needs the enchanted iron pickaxe at 0.2, got %+v", pick)
	}
	if potion == nil || potion.item != "potion" || potion.potion != potLongInvisibility || potion.cost != 5 {
		t.Errorf("the rare pool needs the long Invisibility bottle for 5 emeralds, got %+v", potion)
	}

	// Rolling a trader gives the pools' quotas, and the two special listings
	// arrive carrying their component.
	w := world.New(23)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityWanderingTrader, pl.x+1, pl.y, pl.z)
	sawEnch, sawPotion := false, false
	for i := 0; i < 80; i++ {
		h.rollTraderOffers(m)
		if want := traderPools[0].pick + traderPools[1].pick + traderPools[2].pick; len(m.offers) != want {
			t.Fatalf("roll %d gave %d offers, want %d", i, len(m.offers), want)
		}
		for _, o := range m.offers {
			if o.trade.kind != vTradeFixed {
				t.Fatal("an offer on a trader must be resolved")
			}
			if o.outEnchs[0].lvl > 0 && o.trade.outItem == itemByName["iron_pickaxe"] {
				sawEnch = true
			}
			if o.output().potion == potLongInvisibility {
				sawPotion = true
			}
		}
	}
	if !sawEnch {
		t.Error("the enchanted pickaxe never came up in 80 rolls")
	}
	if !sawPotion {
		t.Error("the Invisibility bottle never came up in 80 rolls")
	}
}
