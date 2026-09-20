package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// A tipped arrow gives an eighth of the bottle's duration, as its
// POTION_DURATION_SCALE says — not the whole thing.
func TestTippedArrowGivesAnEighth(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 70, 0.5

	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 71, 4.5, 0, 0, -0.5)
	a.tipped, a.potion, a.dmg, a.mobShot = true, potSwiftness, 1, true
	full := 0
	for _, e := range potionEffects(potSwiftness) {
		full = e.ticks
	}
	if full == 0 {
		t.Fatal("swiftness should have a duration")
	}
	h.arrowHitsPlayer(players, a, pl.x, pl.y+1, pl.z)
	eff := pl.effects[effSpeed]
	if eff == nil {
		t.Fatal("the arrow should have applied swiftness")
	}
	want := full / 8 // POTION_DURATION_SCALE: an eighth of the bottle's ticks
	if eff.left > want+20 || eff.left < want-20 {
		t.Errorf("a tipped arrow gives about %d ticks, got %d (the bottle is %d)", want, eff.left, full)
	}
}

// A netherite set carries knockback resistance: a hit barely moves you.
func TestNetheriteResistsKnockback(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	if got := pl.playerAttrs().Value(attr.KnockbackResistance); got != 0 {
		t.Fatalf("bare, a player resists nothing: %v", got)
	}
	for i, n := range []string{"netherite_helmet", "netherite_chestplate", "netherite_leggings", "netherite_boots"} {
		pl.armor[i] = invStack{item: itemByName[n], count: 1}
	}
	pl.refreshArmorAttrs()
	if got := pl.playerAttrs().Value(attr.KnockbackResistance); got < 0.39 || got > 0.41 {
		t.Fatalf("a full netherite set resists 0.4, got %v", got)
	}
	// Taking it off clears the modifier again.
	for i := range pl.armor {
		pl.armor[i] = invStack{}
	}
	pl.refreshArmorAttrs()
	if got := pl.playerAttrs().Value(attr.KnockbackResistance); got != 0 {
		t.Fatalf("stripped, it is back to zero: %v", got)
	}
}
