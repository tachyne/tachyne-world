package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The three damage families each add 2.5 a level against the set they name,
// and Impaling's set is #aquatic — which a trident in the HAND bites too, not
// only a thrown one, because the effect belongs to the item.
func TestFamilyMeleeBonusCoversImpaling(t *testing.T) {
	sword := invStack{item: tDiamondSword, count: 1}
	trident := invStack{item: itemTrident, count: 1}
	sword.ench[0] = enchApply{id: enchSmite, lvl: 3}
	if got, want := familyMeleeBonus(sword, entityZombie), 7.5; got != want {
		t.Errorf("Smite III on a zombie adds %v, want %v", got, want)
	}
	if got := familyMeleeBonus(sword, entityCow); got != 0 {
		t.Errorf("Smite adds %v to a cow, want 0", got)
	}
	trident.ench[0] = enchApply{id: enchImpaling, lvl: 4}
	if got, want := familyMeleeBonus(trident, entityDolphin), 10.0; got != want {
		t.Errorf("Impaling IV on a dolphin adds %v, want %v", got, want)
	}
	if got := familyMeleeBonus(trident, entityZombie); got != 0 {
		t.Error("Impaling does not bite a zombie standing in the rain")
	}
	// Bane and Smite on the same weapon each apply on their own terms.
	both := invStack{item: tDiamondSword, count: 1}
	both.ench[0] = enchApply{id: enchSmite, lvl: 2}
	both.ench[1] = enchApply{id: enchBaneOfArthropods, lvl: 2}
	if got, want := familyMeleeBonus(both, entitySpider), 5.0; got != want {
		t.Errorf("Bane II on a spider adds %v, want %v", got, want)
	}
}

// Bane of Arthropods' post_attack half: a struck arthropod is slowed to a
// crawl, Slowness IV for 1.5 s plus half a second a level above the first.
func TestBaneOfArthropodsSlows(t *testing.T) {
	h := newHub(world.New(47))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players

	weapon := invStack{item: tDiamondSword, count: 1}
	weapon.ench[0] = enchApply{id: enchBaneOfArthropods, lvl: 5}
	spider := h.spawnMob(players, entitySpider, 2, 70, 0)
	h.applyBaneSlowness(players, weapon, spider)
	eff := spider.effects[effSlowness]
	if eff == nil {
		t.Fatal("a struck spider is slowed")
	}
	if eff.amp != 3 {
		t.Errorf("amplifier %d, want 3 (Slowness IV)", eff.amp)
	}
	if eff.left < 30 || eff.left > 70 {
		t.Errorf("duration %d ticks, want 30..70 for level 5", eff.left)
	}

	// Nothing happens to a cow, or without the enchantment.
	cow := h.spawnMob(players, entityCow, 3, 70, 0)
	h.applyBaneSlowness(players, weapon, cow)
	if cow.effects[effSlowness] != nil {
		t.Error("a cow is not an arthropod")
	}
	plain := h.spawnMob(players, entitySpider, 4, 70, 0)
	h.applyBaneSlowness(players, invStack{item: tDiamondSword, count: 1}, plain)
	if plain.effects[effSlowness] != nil {
		t.Error("a plain sword slows nothing")
	}

	// The roll stays inside vanilla's window at every level.
	for lvl := 1; lvl <= 5; lvl++ {
		lo, hi := 30, 30+10*(lvl-1)
		for i := 0; i < 50; i++ {
			if got := baneSlownessTicks(h.rng, lvl); got < lo || got > hi {
				t.Fatalf("level %d rolled %d ticks, want %d..%d", lvl, got, lo, hi)
			}
		}
	}
}
