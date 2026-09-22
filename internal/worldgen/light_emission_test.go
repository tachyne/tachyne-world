package worldgen

import "testing"

// Light is a per-STATE property, and the table used to be built from a source
// carrying one value per BLOCK — the default state's. That flattened 60
// blocks and was wrong in both directions: a lit furnace emitted nothing, and
// an unlit redstone torch emitted 7, because a redstone torch's default state
// is the lit one.
func TestLightEmissionIsPerState(t *testing.T) {
	for _, tc := range []struct {
		block, prop, val string
		want             uint8
	}{
		{"furnace", "lit", "true", 13},
		{"furnace", "lit", "false", 0},
		{"redstone_lamp", "lit", "true", 15},
		{"redstone_lamp", "lit", "false", 0},
		{"redstone_ore", "lit", "true", 9},
		{"redstone_ore", "lit", "false", 0},
		{"redstone_torch", "lit", "true", 7},
		{"redstone_torch", "lit", "false", 0}, // the one the old table got backwards
		{"respawn_anchor", "charges", "0", 0},
		{"respawn_anchor", "charges", "4", 15},
		{"cave_vines", "berries", "true", 14},
		{"cave_vines", "berries", "false", 0},
		{"blast_furnace", "lit", "true", 13},
		{"smoker", "lit", "true", 13},
		{"campfire", "lit", "true", 15},
		{"campfire", "lit", "false", 0},
	} {
		info, ok := OrientInfo(BlockID(tc.block))
		if !ok {
			t.Errorf("%s: no block info", tc.block)
			continue
		}
		state := SetProperty(info, BlockID(tc.block), tc.prop, tc.val)
		if got := LightEmission(state); got != tc.want {
			t.Errorf("%s[%s=%s] emits %d, want %d", tc.block, tc.prop, tc.val, got, tc.want)
		}
	}
	// Blocks with no light at all stay dark whatever their state.
	for _, n := range []string{"stone", "dirt", "oak_planks"} {
		if got := LightEmission(BlockID(n)); got != 0 {
			t.Errorf("%s emits %d, want 0", n, got)
		}
	}
}

// The fast flat table must agree with the range table for every state it
// covers. It also carries a hand-written patch for copper bulbs, added when
// the generated ranges were per-BLOCK and could not express "lit". Now that
// they are per-state, that patch should be redundant — and this proves it,
// so the day it stops agreeing, something has drifted.
func TestFastLightTableMatchesRanges(t *testing.T) {
	var checked, bad int
	for s := uint32(0); s < 30000; s++ {
		a, b := LightEmission(s), LightEmissionFast(s)
		checked++
		if a != b {
			if bad < 5 {
				t.Errorf("state %d: range table says %d, fast table says %d", s, a, b)
			}
			bad++
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d states disagree", bad, checked)
	}
}

// Light filtering is per state too, and for the same reason: a double slab
// renders solid and blocks all light while its halves block none, and a
// waterlogged fence dims by 1 where a dry one dims by nothing. Built from a
// per-BLOCK source, 10,394 of the 29,671 states were wrong — 125 of them
// letting light straight through a solid block.
func TestLightFilterIsPerState(t *testing.T) {
	for _, tc := range []struct {
		block, prop, val string
		want             int
	}{
		{"oak_slab", "type", "double", 15}, // solid: blocks everything
		{"oak_slab", "type", "bottom", 0},
		{"oak_slab", "type", "top", 0},
		{"stone_slab", "type", "double", 15},
		{"oak_fence", "waterlogged", "true", 1}, // water dims by one
		{"oak_fence", "waterlogged", "false", 0},
		{"oak_stairs", "waterlogged", "true", 1},
		{"oak_stairs", "waterlogged", "false", 0},
	} {
		info, ok := OrientInfo(BlockID(tc.block))
		if !ok {
			t.Errorf("%s: no block info", tc.block)
			continue
		}
		state := SetProperty(info, BlockID(tc.block), tc.prop, tc.val)
		if got := LightFilter(state); got != tc.want {
			t.Errorf("%s[%s=%s] filters %d, want %d", tc.block, tc.prop, tc.val, got, tc.want)
		}
	}
	// Stone is opaque and glass is not, whatever else changes.
	if got := LightFilter(BlockID("stone")); got != Opaque {
		t.Errorf("stone filters %d, want %d", got, Opaque)
	}
	if got := LightFilter(BlockID("glass")); got != 0 {
		t.Errorf("glass filters %d, want 0", got)
	}
}

// The fast flat filter table must agree with the range table everywhere.
func TestFastFilterTableMatchesRanges(t *testing.T) {
	for s := uint32(0); s < 30000; s++ {
		if a, b := LightFilter(s), int(LightFilterFast(s)); a != b {
			t.Fatalf("state %d: range table says %d, fast table says %d", s, a, b)
		}
	}
}
