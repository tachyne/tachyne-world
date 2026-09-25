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
