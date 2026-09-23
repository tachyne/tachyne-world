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
		{Air, 0, false, false},                // air: instant, not diggable as a target, pass-through
		{blockID("stone"), 1.5, true, true},   // stone
		{blockID("bedrock"), -1, false, true}, // bedrock: unbreakable
		{StateWith("oak_leaves", map[string]string{"distance": "7", "persistent": "false", "waterlogged": "false"}), 0.2, true, true}, // oak_leaves: soft, solid
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

// The pickaxe half of this lives in the server package, which has item ids
// by name (TestStoneNeedsAPickaxe).
func TestHarvestableBy(t *testing.T) {
	if HarvestableBy(blockID("stone"), 0) != false {
		t.Error("stone should NOT drop when broken by hand (item 0)")
	}
	// Dirt has no tool requirement: drops by hand.
	if HarvestableBy(blockID("dirt"), 0) != true {
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
