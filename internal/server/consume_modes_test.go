package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Drinking a potion through the eat-hold: use, hold for 32 ticks, and the
// effect lands and the bottle comes back.
func TestDrinkPotionThroughTheHold(t *testing.T) {
	for _, mode := range []int{gmSurvival, gmAdventure, gmCreative} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.gamemode = mode
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		st := potionStack(potSwiftness)
		pl.inv.slots[pl.p.heldSlot()] = st
		h.startEating(pl, pl.p.heldSlot())
		for i := 0; i < 40; i++ {
			h.tick.Add(1)
			h.updateEating(players)
		}
		if pl.effects[effSpeed] == nil {
			t.Errorf("mode %d: drank a swiftness potion, no Speed", mode)
		}
		got := pl.inv.slots[pl.p.heldSlot()]
		if mode == gmCreative && got.item != itemPotion {
			t.Errorf("creative: the potion was used up (%d)", got.item)
		}
		if mode != gmCreative && got.item != itemGlassBottle {
			t.Errorf("mode %d: no bottle back (%d)", mode, got.item)
		}
	}
}

// Adventure players eat as survival ones do; a creative player eats on a
// full bar and keeps the food.
func TestEatingInAdventureAndCreative(t *testing.T) {
	for _, mode := range []int{gmAdventure, gmCreative} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		pl.gamemode = mode
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		bread := itemByName["bread"]
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: bread, count: 2}
		pl.food = 10
		if mode == gmCreative {
			pl.food = maxFood
		}
		h.startEating(pl, pl.p.heldSlot())
		for i := 0; i < 40; i++ {
			h.tick.Add(1)
			h.updateEating(players)
		}
		got := pl.inv.slots[pl.p.heldSlot()].count
		if mode == gmAdventure && (got != 1 || pl.food <= 10) {
			t.Errorf("adventure: bread %d, food %d", got, pl.food)
		}
		if mode == gmCreative && got != 2 {
			t.Errorf("creative: ate bread and it was used up (%d left)", got)
		}
	}
}

// BundleItem.use + onUseTick, through the use path: holding a bundle
// tosses its contents out one at a time, the first at once, then one
// every other tick after the tenth.
func TestBundleEmptiesWhileHeld(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 200, 0.5
	id := h.newBundleID()
	h.bundles.set(id, []invStack{{item: itemByName["stone"], count: 1}, {item: itemByName["dirt"], count: 1}, {item: itemByName["sand"], count: 1}})
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["bundle"], count: 1, bundleID: id}
	h.startEating(pl, pl.p.heldSlot())
	h.updateEating(players) // the first tick of use
	if n := len(h.bundles.get(id)); n != 2 || len(h.items) != 1 {
		t.Fatalf("after the first tick: %d left in the bundle, %d on the ground", n, len(h.items))
	}
	for i := 0; i < 11; i++ {
		h.tick.Add(1)
		h.updateEating(players)
	}
	if n := len(h.bundles.get(id)); n != 2 {
		t.Errorf("tick 11: %d left, want 2 (the next goes at tick 12)", n)
	}
	for i := 0; i < 4; i++ {
		h.tick.Add(1)
		h.updateEating(players)
	}
	if n := len(h.bundles.get(id)); n != 0 || pl.eatingSlot != -1 {
		t.Errorf("the bundle did not empty and stop: %d left, using %d", n, pl.eatingSlot)
	}
}
