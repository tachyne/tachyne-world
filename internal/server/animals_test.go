package server

import (
	"testing"

	"tachyne/internal/world"
)

// TestRosterSpeciesBreed: the new species breed on their table love-food, not
// just the original farm four.
func TestRosterSpeciesBreed(t *testing.T) {
	cases := []struct {
		etype int
		food  int32
	}{
		{entityMooshroom, itemByName["wheat"]},
		{entityWolf, itemByName["beef"]},
		{entityPanda, itemByName["bamboo"]},
		{entityHorse, itemByName["golden_carrot"]},
	}
	for _, c := range cases {
		h := newHub(world.New(1))
		pl := testTracked()
		pl.p.setHotbarSlot(0, c.food)
		pl.inv.slots[0] = invStack{item: c.food, count: 2}
		players := map[int32]*tracked{1: pl}
		a := h.spawnAnimal(players, c.etype, 3, 3)
		b := h.spawnAnimal(players, c.etype, 5, 3)
		pl.x, pl.y, pl.z = a.x, a.y, a.z
		if !h.feedAnimal(players, pl, a) || !h.feedAnimal(players, pl, b) {
			t.Fatalf("etype %d should court on its love-food %d", c.etype, c.food)
		}
		before := len(h.mobs)
		h.updateBreeding(players)
		if len(h.mobs) != before+1 {
			t.Fatalf("etype %d courting pair should breed: %d mobs, was %d", c.etype, len(h.mobs), before)
		}
	}
}

func TestFeedingPairMakesABaby(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemWheat)
	pl.inv.slots[0] = invStack{item: itemWheat, count: 2}
	players := map[int32]*tracked{1: pl}

	a := h.spawnAnimal(players, entityCow, 3, 3)
	b := h.spawnAnimal(players, entityCow, 5, 3)
	pl.x, pl.y, pl.z = a.x, a.y, a.z

	if !h.feedAnimal(players, pl, a) || !h.feedAnimal(players, pl, b) {
		t.Fatal("feeding wheat to adult cows must court them")
	}
	if pl.inv.slots[0].count != 0 {
		t.Fatalf("two wheat should be consumed, left %d", pl.inv.slots[0].count)
	}
	before := len(h.mobs)
	h.updateBreeding(players)
	if len(h.mobs) != before+1 {
		t.Fatalf("courting pair should produce a baby: %d mobs, was %d", len(h.mobs), before)
	}
	var baby *mob
	for _, m := range h.mobs {
		if m.baby {
			baby = m
		}
	}
	// (the same 1 Hz sweep that delivered it may have aged it one step)
	if baby == nil || baby.etype != entityCow || baby.growLeft < growUpTicks-2*survivalTickN {
		t.Fatalf("baby wrong: %+v", baby)
	}
	if a.breedCD == 0 || b.breedCD == 0 || a.loveTicks != 0 {
		t.Fatal("parents must be on cooldown, out of love mode")
	}
	// The baby grows up.
	baby.growLeft = survivalTickN
	h.updateBreeding(players)
	if baby.baby {
		t.Fatal("baby should mature when growLeft runs out")
	}
}

func TestFeedGatesAndCooldown(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemWheat)
	pl.inv.slots[0] = invStack{item: itemWheat, count: 5}
	players := map[int32]*tracked{1: pl}
	m := h.spawnAnimal(players, entityPig, 3, 3)
	if h.feedAnimal(players, pl, m) {
		t.Fatal("wheat must not court a pig (it wants carrots)")
	}
	m.breedCD = 100
	pl.p.setHotbarSlot(0, itemCarrot)
	pl.inv.slots[0] = invStack{item: itemCarrot, count: 1}
	if h.feedAnimal(players, pl, m) {
		t.Fatal("a parent on cooldown must refuse")
	}
}

func TestShearingAndRegrowth(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemShears)
	pl.inv.slots[0] = invStack{item: itemShears, count: 1}
	players := map[int32]*tracked{1: pl}
	m := h.spawnAnimal(players, entitySheep, 3, 3)

	if !h.shearSheep(players, pl, m) || !m.sheared {
		t.Fatal("shears on a woolly sheep must shear it")
	}
	if len(h.items) == 0 {
		t.Fatal("shearing must drop wool")
	}
	if h.shearSheep(players, pl, m) {
		t.Fatal("an already-sheared sheep gives nothing")
	}
	for i := 0; i < 500 && m.sheared; i++ {
		h.updateBreeding(players)
	}
	if m.sheared {
		t.Fatal("wool must regrow eventually")
	}
}

func TestShearedSheepDropsNoWool(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnAnimal(players, entitySheep, 3, 3)
	m.sheared, m.hitByPlayer = true, true
	h.despawnMob(players, m)
	for _, it := range h.items {
		if it.item == itemWhiteWool {
			t.Fatal("a sheared sheep must not drop wool")
		}
	}
}

func TestChickensLayEggs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnAnimal(players, entityChicken, 3, 3)
	m.eggIn = survivalTickN
	h.updateBreeding(players)
	found := false
	for _, it := range h.items {
		if it.item == itemEgg {
			found = true
		}
	}
	if !found {
		t.Fatal("the chicken should have laid an egg")
	}
	if m.eggIn < eggLayMin-survivalTickN {
		t.Fatalf("the layer must re-arm, eggIn=%d", m.eggIn)
	}
}
