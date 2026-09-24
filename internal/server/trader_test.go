package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestWanderingTrader: every listing names a real item; a spawn puts a
// trader with nine offers and two leashed llamas near the player; at night
// it drinks invisibility and at dawn milk; the clock despawns it.
func TestWanderingTrader(t *testing.T) {
	for _, set := range traderTradeSets {
		for _, tr := range set.trades {
			if tr.inItem == 0 || tr.outItem == 0 {
				t.Fatalf("a listing names no item: %+v", tr)
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

// The wandering trader's pools are vanilla's wandering_trader trade sets:
// buying (it pays emeralds), uncommon and common, in that order, drawing 2, 2
// and 5. The buy listings pay out as vanilla's do (a water bucket sells twice
// for two emeralds, one experience each), and the two uncommon listings that
// carry a component — the enchanted iron pickaxe and the long Invisibility
// bottle — roll it.
func TestTraderPoolsMatchVanilla(t *testing.T) {
	if len(traderTradeSets) != 3 {
		t.Fatalf("%d wandering-trader sets, want buying, uncommon and common", len(traderTradeSets))
	}
	for i, want := range []int{2, 2, 5} {
		if traderTradeSets[i].amount != want {
			t.Errorf("set %d draws %d, want %d", i, traderTradeSets[i].amount, want)
		}
	}
	emerald := itemByName["emerald"]
	buys := map[int32]vTrade{}
	for _, tr := range traderTradeSets[0].trades {
		if tr.outItem != emerald {
			t.Errorf("the buying set only buys, got %+v", tr)
		}
		buys[tr.inItem] = tr
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
		tr, ok := buys[itemByName[tc.item]]
		if !ok {
			t.Errorf("%s is missing from the buy pool", tc.item)
			continue
		}
		if tr.inCount != tc.cost || tr.outCount != tc.count || tr.maxUses != tc.maxUses || tr.xp != tc.xp {
			t.Errorf("%s is %d→%d, %d uses, %d xp; want %d→%d, %d uses, %d xp",
				tc.item, tr.inCount, tr.outCount, tr.maxUses, tr.xp, tc.cost, tc.count, tc.maxUses, tc.xp)
		}
	}

	// The uncommon pool's two component-carrying listings.
	var pick, potion *vTrade
	for i, tr := range traderTradeSets[1].trades {
		switch tr.kind {
		case vTradeEnchantedGear:
			pick = &traderTradeSets[1].trades[i]
		case vTradePotion:
			potion = &traderTradeSets[1].trades[i]
		}
	}
	if pick == nil || pick.outItem != itemByName["iron_pickaxe"] || pick.mult100 != 20 {
		t.Errorf("the uncommon pool needs the enchanted iron pickaxe at 0.2, got %+v", pick)
	}
	if potion == nil || potion.outItem != itemPotion || potion.inCount != 5 ||
		len(villagerPotionPools[potion.aux-1]) != 1 || villagerPotionPools[potion.aux-1][0] != "long_invisibility" {
		t.Errorf("the uncommon pool needs the long Invisibility bottle for 5 emeralds, got %+v", potion)
	}

	// Rolling a trader gives the sets' amounts, and the two special listings
	// arrive carrying their component.
	w := world.New(23)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityWanderingTrader, pl.x+1, pl.y, pl.z)
	sawEnch, sawPotion := false, false
	for i := 0; i < 80; i++ {
		h.rollTraderOffers(m)
		want := 0
		for _, set := range traderTradeSets {
			want += set.amount
		}
		if len(m.offers) != want {
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
