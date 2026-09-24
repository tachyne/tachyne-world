package server

import "testing"

// Lunge is in the engine's table at 42 (appended after the 1.21.5 registry,
// as clients number it), goes on spears only, and can come from the
// enchanting table and trades.
func TestLungeIsAnEnchantment(t *testing.T) {
	if int(enchLunge) >= len(enchDefs) || enchDefs[enchLunge].name != "lunge" {
		t.Fatalf("enchantment %d is not lunge", enchLunge)
	}
	d := enchDefs[enchLunge]
	if d.maxLevel != 3 || d.flags&enchInTable == 0 || d.flags&enchTradeable == 0 {
		t.Errorf("lunge %+v: want max level 3, in the table, tradeable", d)
	}
	if !enchIsSupported(enchLunge, itemByName["iron_spear"]) || enchIsSupported(enchLunge, itemIronSword) {
		t.Error("lunge goes on spears and nothing else")
	}
	if enchantabilityOf(itemByName["iron_spear"]) != 14 {
		t.Error("an iron spear has the iron tool material's enchantability")
	}
}
