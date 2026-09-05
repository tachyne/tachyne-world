package server

import (
	"testing"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Gear attributes follow equipment changes — and only equipment changes.
func TestGearAttributesRefreshOnChangeOnly(t *testing.T) {
	pl := testTracked()
	armorOf := func() float64 { return pl.playerAttrs().Get(attr.Armor).Value() }

	pl.refreshGearIfChanged() // first sync: nothing worn
	if got := armorOf(); got != 0 {
		t.Fatalf("bare armor = %v, want 0", got)
	}
	pl.armor[0] = invStack{item: itemByName["iron_helmet"], count: 1}
	pl.refreshGearIfChanged()
	worn := armorOf()
	if worn <= 0 {
		t.Fatalf("armor after equipping a helmet = %v, want > 0", worn)
	}
	if pl.lastArmor != pl.armor || !pl.gearSynced {
		t.Fatal("last-synced copy not updated")
	}
	// Unchanged gear: the refresh is skipped (observable: lastArmor already equal,
	// and the value is stable).
	pl.refreshGearIfChanged()
	if armorOf() != worn {
		t.Errorf("armor drifted on a no-change refresh: %v vs %v", armorOf(), worn)
	}
	pl.armor[0] = invStack{} // took it off
	pl.refreshGearIfChanged()
	if got := armorOf(); got != 0 {
		t.Errorf("armor after unequipping = %v, want 0", got)
	}
}
