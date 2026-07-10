package worldgen

import "testing"

func TestBlockMetaScalars(t *testing.T) {
	// state, hardness, diggable, stack, collides
	cases := []struct {
		state    uint32
		hardness float32
		diggable bool
		collides bool
	}{
		{Air, 0, false, false},       // air: instant, not diggable as a target, pass-through
		{1, 1.5, true, true},         // stone
		{85, -1, false, true},        // bedrock: unbreakable
		{279, 0.2, true, true},       // oak_leaves: soft, solid
		{ShortGrass, 0, true, false}, // short_grass: instant, pass-through
	}
	for _, c := range cases {
		if got := Hardness(c.state); got != c.hardness {
			t.Errorf("Hardness(%d) = %v, want %v", c.state, got, c.hardness)
		}
		if got := Diggable(c.state); got != c.diggable {
			t.Errorf("Diggable(%d) = %v, want %v", c.state, got, c.diggable)
		}
		if got := Collides(c.state); got != c.collides {
			t.Errorf("Collides(%d) = %v, want %v", c.state, got, c.collides)
		}
	}
}

func TestHarvestableBy(t *testing.T) {
	const woodenPick = 913 // wooden_pickaxe (1.21.11) — a tool id in stone's harvestTools set
	stone := blockID("stone")
	// Stone requires a pickaxe: drops with one, not by hand.
	if HarvestableBy(stone, woodenPick) != true {
		t.Error("stone should be harvestable with a wooden pickaxe")
	}
	if HarvestableBy(stone, 0) != false {
		t.Error("stone should NOT drop when broken by hand (item 0)")
	}
	// Dirt (state 10) has no tool requirement: drops by hand.
	if HarvestableBy(10, 0) != true {
		t.Error("dirt should drop by hand")
	}
}

func TestStackSizeState(t *testing.T) {
	cases := map[uint32]int{
		blockID("stone"):     64,
		blockID("oak_sign"):  16,
		blockID("white_bed"): 1,
	}
	for state, want := range cases {
		if got := StackSizeState(state); got != want {
			t.Errorf("StackSizeState(%d) = %d, want %d", state, got, want)
		}
	}
}
