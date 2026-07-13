package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lava next to flammable material eventually ignites nearby air (vanilla
// LavaFluid.randomTick). Deterministic enough over many attempts.
func TestLavaIgnition(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		y := 70
		planks := worldgen.BlockBase("oak_planks")
		w.SetBlock(30, y, 0, worldgen.LavaBase) // lava source
		w.SetBlock(31, y, 0, planks)            // flammable beside it
		w.SetBlock(31, y+1, 0, worldgen.Air)    // open above the planks
		lit := false
		for i := 0; i < 3000 && !lit; i++ {
			h.lavaIgnite(h.playersRef, 30, y, 0)
			for dx := -1; dx <= 2; dx++ {
				for dy := 0; dy <= 3; dy++ {
					for dz := -1; dz <= 1; dz++ {
						if isFire(w.At(30+dx, y+dy, dz)) {
							lit = true
						}
					}
				}
			}
		}
		if !lit {
			t.Error("lava never ignited nearby flammable material in 3000 tries")
		}
	})
}
