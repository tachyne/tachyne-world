package worldgen

import "testing"

// TestIsFaceSturdy: the faces WallBlock, FenceBlock and IronBarsBlock join,
// read per face from the game's own shapes.
func TestIsFaceSturdy(t *testing.T) {
	open := stateWith(t, "poplar_trapdoor", map[string]string{"facing": "west", "half": "top", "open": "true"})
	shut := stateWith(t, "poplar_trapdoor", map[string]string{"facing": "west", "half": "top", "open": "false"})
	stair := stateWith(t, "oak_stairs", map[string]string{"facing": "east", "half": "bottom", "shape": "straight"})
	slab := stateWith(t, "smooth_quartz_slab", map[string]string{"type": "bottom"})
	for _, c := range []struct {
		what  string
		state uint32
		face  int
		want  bool
	}{
		{"an open trapdoor's hinge side", open, FaceEast, true},
		{"an open trapdoor's free side", open, FaceWest, false},
		{"a shut top trapdoor's side", shut, FaceEast, false},
		{"a shut top trapdoor's top", shut, FaceUp, true},
		{"a stair's back", stair, FaceEast, true},
		{"a stair's front", stair, FaceWest, false},
		{"a bottom slab's side", slab, FaceNorth, false},
		{"a bottom slab's underside", slab, FaceDown, true},
		{"stone", Stone, FaceSouth, true},
		{"glass", BlockBase("glass"), FaceWest, true},
		{"leaves", BlockBase("oak_leaves"), FaceWest, false},
		{"soul sand's side", BlockBase("soul_sand"), FaceWest, true},
		{"air", Air, FaceUp, false},
	} {
		if got := IsFaceSturdy(c.state, c.face); got != c.want {
			t.Errorf("%s: sturdy=%v, want %v", c.what, got, c.want)
		}
	}
}
