package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Drive the freeze path against a hand-built cold, sky-open water column.
// We can't rely on where snowy biomes generate, so we find a snowy column
// near origin (skipping if the seed has none) and exercise precipTick.
func TestSnowAndIce(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		if snowLayer1 != worldgen.BlockBase("snow") || iceBlock != worldgen.BlockBase("ice") {
			t.Fatalf("id wiring: snow=%d ice=%d", snowLayer1, iceBlock)
		}
		var fx, fz int
		found := false
		for r := 0; r < 400 && !found; r += 8 {
			for _, c := range [][2]int{{r, 0}, {0, r}, {-r, 0}, {0, -r}} {
				x, z := c[0], c[1]
				if worldgen.PrecipitationAt(w.BiomeAt(x, z), worldgen.SeaLevel) == worldgen.PrecipSnow {
					fx, fz, found = x, z, true
					break
				}
			}
		}
		if !found {
			t.Skip("no snowy biome near origin in this seed")
		}
		gy := w.GroundY(fx, fz)
		y := gy
		if y < worldgen.SeaLevel {
			y = worldgen.SeaLevel
		}
		w.SetBlock(fx, y, fz, worldgen.WaterBase) // exposed source
		w.SetBlock(fx+1, y, fz, worldgen.Stone)   // a non-water edge neighbour
		for cy := y + 1; cy < y+7; cy++ {
			w.SetBlock(fx, cy, fz, worldgen.Air) // clear the sky column
		}
		froze := false
		for i := 0; i < 4000 && !froze; i++ {
			h.precipTick(h.playersRef, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
			if w.At(fx, y, fz) == iceBlock {
				froze = true
			}
		}
		if !froze {
			t.Error("exposed cold water never froze to ice")
		}
	})
}
