package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestWitchPotions: the potion follows vanilla's table (slowness far off,
// poison when healthy, harming otherwise), a splash lands on everyone
// within four blocks, and a burning witch drinks fire resistance.
func TestWitchPotions(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.spawnMob(players, entityWitch, 0.5, 180, 0.5)
	pl.x, pl.y, pl.z = 9.5, 180, 0.5
	if k := h.witchPotionFor(w, pl); k != potSlowness {
		t.Fatalf("far off and not slowed: slowness, got %d", k)
	}
	pl.x = 5.5
	if k := h.witchPotionFor(w, pl); k != potPoison {
		t.Fatalf("healthy: poison, got %d", k)
	}
	pl.health = 6
	pl.x = 5.5
	if k := h.witchPotionFor(w, pl); k != potHarming {
		t.Fatalf("hurt and at five blocks: harming, got %d", k)
	}
	// A poison splash: the player at the burst in full, a bystander three
	// blocks off for a quarter, one beyond four blocks untouched.
	near := survPlayer(h)
	near.p.eid = pl.p.eid + 1
	near.x, near.y, near.z = 8.5, 180, 0.5
	far := survPlayer(h)
	far.p.eid = pl.p.eid + 2
	far.x, far.y, far.z = 20, 180, 0.5
	players[near.p.eid], players[far.p.eid] = near, far
	h.splashPotion(players, 0, 5.5, 180, 0.5, potPoison, false)
	if pl.hasEffect(effPoison) == 0 || near.hasEffect(effPoison) == 0 || far.hasEffect(effPoison) != 0 {
		t.Fatalf("splash reach: struck %d near %d far %d", pl.hasEffect(effPoison), near.hasEffect(effPoison), far.hasEffect(effPoison))
	}
	if pl.effects[effPoison].left <= near.effects[effPoison].left {
		t.Fatal("the player at the burst takes the longer dose")
	}
	// Drinking: a burning witch reaches for fire resistance and is slower
	// for the thirty-two ticks it takes.
	w.burning = true
	kind := int8(potNone)
	for i := 0; i < 200 && kind == potNone; i++ {
		kind = h.witchWantsToDrink(players, w, nil)
	}
	if kind != potFireRes {
		t.Fatalf("burning: fire resistance, got %d", kind)
	}
	base := w.moveSpeed()
	h.witchStartDrink(players, w, kind)
	if w.drinkTicks != witchDrinkTicks || w.held != itemPotion || w.moveSpeed() >= base {
		t.Fatalf("drinking: ticks %d held %d speed %.3f vs %.3f", w.drinkTicks, w.held, w.moveSpeed(), base)
	}
	for i := 0; i < 20 && w.drinkTicks > 0; i++ {
		h.witchTick(players, w)
	}
	if w.drinkTicks != 0 || w.held != 0 || w.hasEffect(effFireRes) == 0 || w.moveSpeed() != base {
		t.Fatalf("drunk: ticks %d held %d fireres %d speed %.3f", w.drinkTicks, w.held, w.hasEffect(effFireRes), w.moveSpeed())
	}
}
