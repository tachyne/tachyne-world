package worldgen

import "testing"

func TestLightFilter(t *testing.T) {
	cases := map[uint32]int{
		Air:  0,  // transparent
		1:    15, // stone — opaque
		4697: 0,  // oak_door — transparent (the dark-doorway bug)
		279:  1,  // oak_leaves — translucent
	}
	for state, want := range cases {
		if got := SkyOpacity(state); got != want {
			t.Errorf("SkyOpacity(%d) = %d, want %d", state, got, want)
		}
	}
}
