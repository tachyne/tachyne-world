package worldgen

import "testing"

func TestLightFilter(t *testing.T) {
	cases := map[uint32]int{
		Air:              0,  // transparent
		BlockID("stone"): 15, // stone — opaque
		StateWith("oak_door", map[string]string{"facing": "north", "half": "lower", "hinge": "left", "open": "false", "powered": "false"}): 0, // oak_door — transparent (the dark-doorway bug; this was a literal id that meant redstone dust after 1.21.5)
		BlockID("oak_leaves"): 1, // oak_leaves — translucent
	}
	for state, want := range cases {
		if got := SkyOpacity(state); got != want {
			t.Errorf("SkyOpacity(%d) = %d, want %d", state, got, want)
		}
	}
}
