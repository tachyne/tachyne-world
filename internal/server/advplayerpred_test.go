package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Distract Piglin's direct criterion asks that the player wear no gold
// armour (a piglin ignores the ingot of anyone wearing gold): handing an
// adult piglin an ingot in a golden helmet does not count; bare-headed does.
func TestDistractPiglinNeedsNoGoldArmour(t *testing.T) {
	const adv, crit = "minecraft:nether/distract_piglin", "distract_piglin_directly"
	give := func(helmet string) bool {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.adv = advState{}
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		if helmet != "" {
			pl.armor[0] = invStack{item: itemByName[helmet], count: 1}
		}
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemGoldIngot, count: 1}
		p := h.spawnMob(players, entityPiglin, pl.x+1, pl.y, pl.z)
		if !h.interactMob(players, pl, p, false) {
			t.Fatal("the piglin did not take the ingot")
		}
		_, ok := pl.adv[adv][crit]
		return ok
	}
	if !give("") {
		t.Error("an ingot handed over without gold armour does not count")
	}
	if give("golden_helmet") {
		t.Error("an ingot handed over in a golden helmet counted")
	}
	if !give("iron_helmet") {
		t.Error("an iron helmet stopped the criterion")
	}
}

// Star Trader is a trade at the build limit: the player at y ≥ 319.
func TestTradeAtWorldHeightNeedsTheHeight(t *testing.T) {
	const adv, crit = "minecraft:adventure/trade_at_world_height", "trade_at_world_height"
	trade := func(y float64) bool {
		h := newHub(world.New(7))
		pl := testTracked()
		pl.adv = advState{}
		players := map[int32]*tracked{1: pl}
		pl.y = y
		m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
		h.initVillagerTrades(m, 0)
		m.offers = []mobOffer{
			{trade: vTrade{itemByName["wheat"], 20, itemByName["emerald"], 1, 16, 2, vTradeFixed, 0, 0, 0, defaultPriceMult100, 0}},
		}
		h.openTrades(pl, m)
		pl.trade[0] = invStack{item: itemByName["wheat"], count: 20}
		h.takeTradeResult(players, pl, 0)
		if pl.cursor.item != itemByName["emerald"] {
			t.Fatalf("the trade did not go through at y=%.0f", y)
		}
		_, ok := pl.adv[adv][crit]
		return ok
	}
	if trade(100) {
		t.Error("a trade at y=100 earned Star Trader")
	}
	if !trade(319) {
		t.Error("a trade at y=319 does not earn Star Trader")
	}
}
