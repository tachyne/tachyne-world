package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func findTestVillage(w *world.World) (worldgen.Village, bool) {
	for x := -5000; x <= 5000; x += 384 {
		for z := -5000; z <= 5000; z += 384 {
			if v := w.Gen().VillageIn(x, z); v.Exists {
				return v, true
			}
		}
	}
	return worldgen.Village{}, false
}

func TestVillagePopulatesOnApproach(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	v, ok := findTestVillage(w)
	if !ok {
		t.Skip("no village near origin")
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(v.X), float64(v.Y), float64(v.Z)
	players := map[int32]*tracked{1: pl}
	h.updateVillages(players)
	villagers, golems := 0, 0
	for _, m := range h.mobs {
		switch m.etype {
		case entityVillager:
			villagers++
		case entityIronGolem:
			golems++
		}
	}
	if villagers != len(v.Houses) || golems != 1 {
		t.Fatalf("want %d villagers + 1 golem, got %d + %d", len(v.Houses), villagers, golems)
	}
	// Second pass: no duplicates.
	before := len(h.mobs)
	h.updateVillages(players)
	if len(h.mobs) != before {
		t.Fatal("village must populate once per session")
	}
}

func TestTradeAuthority(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	h.initVillagerTrades(m, 0) // farmer
	// Pin two known offers so the exchange math is deterministic (the tier
	// rotation is a separate concern).
	m.offers = []mobOffer{
		{trade: vTrade{itemByName["wheat"], 20, itemByName["emerald"], 1, 16, 2}},
		{trade: vTrade{itemByName["emerald"], 1, itemByName["bread"], 6, 16, 1}},
	}
	h.openTrades(pl, m)
	if pl.winKind != winTrade {
		t.Fatal("trade window should open")
	}
	// Not enough wheat: result empty, click gives nothing.
	pl.trade[0] = invStack{item: itemByName["wheat"], count: 10}
	h.takeTradeResult(players, pl)
	if pl.cursor.item != 0 {
		t.Fatal("AUTHORITY: trade must not pay without the full cost")
	}
	// Full cost: pays out, consumes exactly 20.
	pl.trade[0] = invStack{item: itemByName["wheat"], count: 25}
	h.takeTradeResult(players, pl)
	if pl.cursor.item != itemByName["emerald"] || pl.cursor.count != 1 {
		t.Fatalf("trade should pay 1 emerald, cursor %+v", pl.cursor)
	}
	if pl.trade[0].count != 5 {
		t.Fatalf("trade should consume 20 wheat, left %d", pl.trade[0].count)
	}
	// Selecting another offer changes the result.
	pl.cursor = invStack{}
	pl.tradeSel = 1 // 1 emerald → 6 bread
	pl.trade[0] = invStack{item: itemByName["emerald"], count: 1}
	h.takeTradeResult(players, pl)
	if pl.cursor.item != itemByName["bread"] || pl.cursor.count != 6 {
		t.Fatalf("bread trade broken: %+v", pl.cursor)
	}
}

func TestGolemPunchesHostiles(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	g := h.spawnMob(players, entityIronGolem, 0.5, 70, 0.5)
	g.behavior = golemBehavior{}
	z := h.spawnMob(players, entityZombie, 1.5, 70, 0.5)
	z.hostile = true
	hp := z.health
	h.golemMelee(players, g)
	if z.health >= hp {
		t.Fatal("golem should damage the zombie")
	}
	if z.kb == 0 {
		t.Fatal("golem hits should launch the target")
	}
}

func TestVillagerLevelsUp(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityVillager, 0, 70, 0)
	h.initVillagerTrades(m, 0) // farmer
	if m.tradeLevel != 1 {
		t.Fatalf("fresh villager should be tier 1, got %d", m.tradeLevel)
	}
	base := len(m.offers)
	if base == 0 {
		t.Fatal("a novice should already have tier-1 offers")
	}
	h.awardTradeXP(m, 10) // vanilla threshold to apprentice
	if m.tradeLevel != 2 {
		t.Fatalf("10 XP should promote to tier 2, got %d", m.tradeLevel)
	}
	if len(m.offers) <= base {
		t.Fatal("leveling up should unlock more offers")
	}
	h.awardTradeXP(m, 500) // overshoot straight to master, capped
	if m.tradeLevel != 5 {
		t.Fatalf("should cap at master (5), got %d", m.tradeLevel)
	}
}

func TestAllProfessionsHaveTrades(t *testing.T) {
	for i := range professionNames {
		if len(villagerTrades[i]) == 0 {
			t.Errorf("profession %d (%s) has no generated trades", i, professionNames[i])
		}
	}
}
