package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Both spellings resolve: the canonical vanilla name and the legacy one
// people (and existing worlds) have been using.
func TestGameruleNamesAcceptBothSpellings(t *testing.T) {
	for legacy, canon := range gameruleAlias {
		got, ok := canonicalRule(legacy)
		if !ok || got != canon {
			t.Errorf("legacy %q resolved to %q/%v, want %q", legacy, got, ok, canon)
		}
		if got, ok := canonicalRule(canon); !ok || got != canon {
			t.Errorf("canonical %q did not resolve to itself", canon)
		}
	}
	if _, ok := canonicalRule("notARule"); ok {
		t.Error("an unknown name resolved to a rule")
	}
	if !isNumericRule("random_tick_speed") || isNumericRule("keep_inventory") {
		t.Error("numeric/boolean split is wrong")
	}
}

// Setting a rule by either name reaches the same field.
func TestGameruleAppliesUnderEitherName(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.rules.KeepInventory = false
	h.applyRule(players, evSetRule{rule: "keepInventory", on: true})
	if !h.rules.KeepInventory {
		t.Error("the legacy name did not apply")
	}
	h.applyRule(players, evSetRule{rule: "keep_inventory", on: false})
	if h.rules.KeepInventory {
		t.Error("the canonical name did not apply")
	}
}

// The new rules actually gate something.
func TestNewGamerulesGateTheirMechanic(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	// tnt_explodes off: the fuse burns out and leaves the world alone.
	h.rules.TNTExplodes = false
	w := h.worldFor(0)
	w.SetBlock(0, 180, 0, worldgen.BlockBase("dirt"))
	h.tnt = append(h.tnt, &primedTNT{eid: h.allocEID(), dim: 0, x: 0.5, y: 181, z: 0.5, fuse: 1})
	h.updateTNT(players)
	if w.At(0, 180, 0) != worldgen.BlockBase("dirt") {
		t.Error("TNT exploded with tnt_explodes off")
	}
	if len(h.tnt) != 0 {
		t.Error("the spent charge should still be cleaned up")
	}

	// water_source_conversion off: two sources no longer make a third.
	h.rules.WaterSourceCnv = false
	w.SetBlock(10, 181, 0, worldgen.WaterBase)
	w.SetBlock(12, 181, 0, worldgen.WaterBase)
	for x := 9; x <= 13; x++ {
		w.SetBlock(x, 180, 0, worldgen.BlockBase("stone"))
	}
	st, _ := h.getNewLiquid(0, blockPos{11, 181, 0}, true, worldgen.WaterBase, 1)
	if st == worldgen.WaterBase {
		t.Error("an infinite source formed with water_source_conversion off")
	}
	h.rules.WaterSourceCnv = true
	if st, _ := h.getNewLiquid(0, blockPos{11, 181, 0}, true, worldgen.WaterBase, 1); st != worldgen.WaterBase {
		t.Error("an infinite source failed to form with the rule on")
	}
}
