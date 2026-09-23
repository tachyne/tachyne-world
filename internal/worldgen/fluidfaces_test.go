package worldgen

import "testing"

func stateWith(t *testing.T, name string, props map[string]string) uint32 {
	t.Helper()
	s := BlockBase(name)
	info, ok := InfoForState(s)
	if !ok {
		t.Fatalf("no info for %s", name)
	}
	for k, v := range props {
		s = SetProperty(info, s, k, v)
	}
	return s
}

// canPassThroughWall on the shapes that matter for bug #25: water held in a
// bottom stair facing west goes out of the open east side and the south side
// (the step's profile leaves it uncovered), not through the full back, not
// down through the full underside. Full blocks stop it; air never does.
func TestFluidPassesFace(t *testing.T) {
	stair := stateWith(t, "smooth_quartz_stairs", map[string]string{"facing": "west", "half": "bottom", "shape": "straight", "waterlogged": "true"})
	slab := stateWith(t, "smooth_quartz_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	cases := []struct {
		name     string
		d        int
		src, dst uint32
		want     bool
	}{
		{"stair east out the step", FaceEast, stair, Air, true},
		{"stair south past the profile", FaceSouth, stair, Air, true},
		{"stair west through its back", FaceWest, stair, Air, false},
		{"stair down through its floor", FaceDown, stair, Air, false},
		{"stair up over its step", FaceUp, stair, Air, true},
		{"slab sideways", FaceEast, slab, Air, true},
		{"slab down", FaceDown, slab, Air, false},
		{"into stone", FaceEast, Air, Stone, false},
		{"air to air", FaceNorth, Air, Air, true},
		{"into the stair's back from the west", FaceEast, Air, stair, false},
	}
	for _, c := range cases {
		if got := FluidPassesFace(c.d, c.src, c.dst); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
