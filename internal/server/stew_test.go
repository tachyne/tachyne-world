package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A bowl, both mushrooms and a flower craft a suspicious stew carrying the
// flower's effect; eating it applies that effect; a brown mooshroom fed a
// flower gives the same stew from its next bowl; the effect survives the
// persisted stack row.
func TestSuspiciousStew(t *testing.T) {
	poppy := int32(itemByName["poppy"])
	grid := make([]invStack, 9)
	grid[0] = invStack{item: itemBowlEmpty, count: 1}
	grid[4] = invStack{item: itemRedMushroom, count: 1}
	grid[5] = invStack{item: itemBrownMushroom, count: 1}
	grid[8] = invStack{item: poppy, count: 1}
	res, ok := stewCraftMatch(grid)
	if !ok || res.item != itemSuspiciousStew || res.stew != stewIndexFor(poppy) {
		t.Fatalf("stew craft %+v %v", res, ok)
	}
	grid[8] = invStack{item: int32(itemByName["stick"]), count: 1}
	if _, ok := stewCraftMatch(grid); ok {
		t.Error("a stick is no stew flower")
	}
	grid[8] = invStack{item: poppy, count: 1}
	grid[1] = invStack{item: poppy, count: 1}
	if _, ok := stewCraftMatch(grid); ok {
		t.Error("two flowers do not make a stew")
	}
	h := newHub(world.New(1))
	if r, _ := h.craftResult(grid[:9], 3); r.item != 0 {
		t.Error("the general crafting path should reject two flowers")
	}
	grid[1] = invStack{}
	if r, _ := h.craftResult(grid[:9], 3); r.item != itemSuspiciousStew || r.stew != res.stew {
		t.Errorf("crafting path result %+v", r)
	}

	// Eating: a poppy stew grants night vision.
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.food = 1
	pl.inv.slots[0] = res
	h.eat(players, pl, 0)
	if pl.hasEffect(effNightVision) == 0 {
		t.Error("a poppy stew should grant night vision")
	}

	// The brown mooshroom remembers a flower for its next bowl.
	m := h.spawnMobIn(players, entityMooshroom, 0, 0, 70, 0)
	m.variant, m.variantSet = mooshroomBrown, true
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: poppy, count: 2}
	if !h.tryFlowerMooshroom(players, pl, m) || m.stew != stewIndexFor(poppy) || pl.inv.slots[pl.p.heldSlot()].count != 1 {
		t.Fatalf("feeding: stew=%d held=%+v", m.stew, pl.inv.slots[pl.p.heldSlot()])
	}
	if !h.tryFlowerMooshroom(players, pl, m) || pl.inv.slots[pl.p.heldSlot()].count != 1 {
		t.Error("a mooshroom already holding a flower takes no second one")
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBowlEmpty, count: 1}
	if !h.tryMilkStew(players, pl, m) {
		t.Fatal("bowl refused")
	}
	if got := pl.inv.slots[pl.p.heldSlot()]; got.item != itemSuspiciousStew || got.stew != stewIndexFor(poppy) || m.stew != 0 {
		t.Errorf("bowl gave %+v (mob stew %d)", got, m.stew)
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBowlEmpty, count: 1}
	h.tryMilkStew(players, pl, m)
	if got := pl.inv.slots[pl.p.heldSlot()]; got.item != itemMushroomStew {
		t.Errorf("the second bowl should be plain stew, got %+v", got)
	}
	red := h.spawnMobIn(players, entityMooshroom, 0, 0, 70, 0)
	red.variant, red.variantSet = mooshroomRed, true
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: poppy, count: 1}
	if h.tryFlowerMooshroom(players, pl, red) {
		t.Error("a red mooshroom does not take flowers")
	}

	// Persistence keeps the flower.
	if back := unpackStack(packStack(res)); back.stew != res.stew || back.item != res.item {
		t.Errorf("stack row lost the stew: %+v", back)
	}
}
