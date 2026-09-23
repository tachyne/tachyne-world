package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestMapColorSpotChecks pins mapColorFor against known vanilla base map
// colors, probed at each block's default state id (canonical 1.21.11).
func TestMapColorSpotChecks(t *testing.T) {
	cases := []struct {
		name  string
		state uint32 // default state id
		want  uint8  // vanilla base map color id
	}{
		{"grass_block", worldgen.BlockID("grass_block"), 1},            // GRASS
		{"stone", worldgen.BlockID("stone"), 11},                       // STONE
		{"water", worldgen.BlockID("water"), 12},                       // WATER
		{"sand", worldgen.BlockID("sand"), 2},                          // SAND
		{"oak_planks", worldgen.BlockID("oak_planks"), 13},             // WOOD
		{"white_wool", worldgen.BlockID("white_wool"), 8},              // SNOW
		{"red_wool", worldgen.BlockID("red_wool"), 28},                 // COLOR_RED
		{"snow", worldgen.BlockID("snow"), 8},                          // SNOW
		{"dirt", worldgen.BlockID("dirt"), 10},                         // DIRT
		{"oak_leaves", worldgen.BlockID("oak_leaves"), 7},              // PLANT
		{"white_terracotta", worldgen.BlockID("white_terracotta"), 36}, // TERRACOTTA_WHITE
	}
	for _, c := range cases {
		if got := mapColorFor(c.state); got != c.want {
			t.Errorf("%s (state %d): got color %d, want %d", c.name, c.state, got, c.want)
		}
	}
	// air (state 0) has no map color
	if got := mapColorFor(0); got != 0 {
		t.Errorf("air (state 0): got color %d, want 0", got)
	}
}
