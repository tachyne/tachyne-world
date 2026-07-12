package server

import "testing"

// TestMapColorSpotChecks pins mapColorFor against known vanilla base map
// colors, probed at each block's default state id (canonical 1.21.11).
func TestMapColorSpotChecks(t *testing.T) {
	cases := []struct {
		name  string
		state uint32 // default state id
		want  uint8  // vanilla base map color id
	}{
		{"grass_block", 9, 1},           // GRASS
		{"stone", 1, 11},                // STONE
		{"water", 86, 12},               // WATER
		{"sand", 118, 2},                // SAND
		{"oak_planks", 15, 13},          // WOOD
		{"white_wool", 2093, 8},         // SNOW
		{"red_wool", 2107, 28},          // COLOR_RED
		{"snow", 6718, 8},               // SNOW
		{"dirt", 10, 10},                // DIRT
		{"oak_leaves", 279, 7},          // PLANT
		{"white_terracotta", 11242, 36}, // TERRACOTTA_WHITE
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
