package server

import "testing"

// A farmer standing at its composter in working hours gets to work: it bakes
// bread from its wheat, empties the ready composter of its bone meal, and
// tips its spare seeds in — keeping ten for sowing.
func TestFarmerWorksTheComposter(t *testing.T) {
	h, v, players := farmerSetup(t)
	h.world.ForceLoad(0, 0, 2)
	site := blockPos{2, 180, 0}
	h.world.SetBlock(site.x, site.y, site.z, composterBase+composterReady)
	v.work = site
	v.x, v.y, v.z = 1.5, 180, 0.5
	seeds := int32(itemByName["wheat_seeds"])
	v.hoard = []invStack{{item: itemWheat, count: 9}, {item: seeds, count: 40}}
	for i := 0; i < 400 && villagerCount(v, itemBread) == 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if got := villagerCount(v, itemBread); got != 3 || villagerCount(v, itemWheat) != 0 {
		t.Fatalf("three loaves from nine wheat: bread %d wheat %d", got, villagerCount(v, itemWheat))
	}
	boneMeal := villagerCount(v, itemBoneMeal)
	for _, it := range h.items {
		if it.item == itemBoneMeal {
			boneMeal += it.count
		}
	}
	if boneMeal != 1 {
		t.Fatalf("the ready composter should give up one bone meal, got %d", boneMeal)
	}
	// Twenty at most go in, and it stops early if the pile fills.
	lvl, _ := composterLevel(h.world.At(site.x, site.y, site.z))
	if got := villagerCount(v, seeds); got < 20 || got >= 40 || (got > 20 && lvl < composterFull) {
		t.Fatalf("twenty seeds composted unless the pile filled first: %d left, level %d", got, lvl)
	}
	if lvl == 0 {
		t.Fatal("the emptied composter should have been fed")
	}
	// Once it has worked, the next go waits out WorkAtPoi's cooldown.
	v.hoard = []invStack{{item: itemWheat, count: 9}}
	for i := 0; i < (workCheckCooldown-20)/mobMoveInterval; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if villagerCount(v, itemBread) != 0 {
		t.Fatal("it worked again inside the three-hundred-tick cooldown")
	}
}

// Away from its job site, or outside working hours, a villager does no work.
func TestNoWorkAwayFromTheSite(t *testing.T) {
	h, v, players := farmerSetup(t)
	h.world.ForceLoad(0, 0, 2)
	site := blockPos{2, 180, 0}
	h.world.SetBlock(site.x, site.y, site.z, composterBase)
	v.work = site
	v.hoard = []invStack{{item: itemWheat, count: 9}}
	h.dayTime.Store(10000) // the gathering hour
	v.x, v.y, v.z = 1.5, 180, 0.5
	for i := 0; i < 400; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		v.x, v.z = 1.5, 0.5
	}
	if villagerCount(v, itemBread) != 0 {
		t.Fatal("no work outside working hours")
	}
}

// A villager shows a nearby player what it would give for the thing in
// their hand, holding the result up; a hand with nothing it wants gets
// nothing shown, and walking off ends it.
func TestVillagerShowsTrades(t *testing.T) {
	h, v, players := farmerSetup(t)
	h.world.ForceLoad(0, 0, 2)
	h.dayTime.Store(1000) // idle: no job site to walk to
	h.unlockTier(v, 1)
	if len(v.offers) == 0 {
		t.Fatal("a farmer should have tier-one offers")
	}
	want := v.offers[0]
	var pl *tracked
	for _, p := range players {
		pl = p
	}
	pl.x, pl.y, pl.z = v.x+2, v.y, v.z
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: want.trade.inItem, count: 1}
	step := func(n int) {
		for i := 0; i < n; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
			pl.x, pl.y, pl.z = v.x+2, v.y, v.z // keep beside it
		}
	}
	step(3)
	if !v.showTrades.showing || v.showTrades.player != pl.p.eid {
		t.Fatalf("the villager should hold up a trade for %d: showing %v", want.trade.inItem, v.showTrades.showing)
	}
	found := false
	for _, st := range v.showTrades.items {
		if st.item == want.trade.outItem {
			found = true
		}
	}
	if !found {
		t.Fatalf("the offer's result %d should be among those shown", want.trade.outItem)
	}
	// Something it does not trade for: the hand goes empty.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["dirt"]), count: 1}
	step(2)
	if v.showTrades.showing {
		t.Fatal("dirt buys nothing: nothing should be shown")
	}
	// Back to the trade item, then walk away: the display ends.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: want.trade.inItem, count: 1}
	step(2)
	if !v.showTrades.showing {
		t.Fatal("the trade should be shown again")
	}
	pl.x = v.x + 10
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if v.showTrades.showing || v.showTrades.player != 0 {
		t.Fatal("a player ten blocks off is no longer being shown anything")
	}
}
