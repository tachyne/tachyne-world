package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
			h.precipTick(h.playersRef, 0, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
			if w.At(fx, y, fz) == iceBlock {
				froze = true
			}
		}
		if !froze {
			t.Error("exposed cold water never froze to ice")
		}
	})
}

// max_snow_accumulation_height: at the default of one, snow never piles
// past a single layer; raised, snowfall stacks layers up to it.
func TestSnowAccumulatesToRule(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	h.playersRef = map[int32]*tracked{}
	fx, fz, found := 0, 0, false
	for r := 0; r < 400 && !found; r += 8 {
		for _, c := range [][2]int{{r, 0}, {0, r}, {-r, 0}, {0, -r}} {
			if worldgen.PrecipitationAt(w.BiomeAt(c[0], c[1]), worldgen.SeaLevel) == worldgen.PrecipSnow {
				fx, fz, found = c[0], c[1], true
				break
			}
		}
	}
	if !found {
		t.Skip("no snowy biome near origin in this seed")
	}
	y := max(w.GroundY(fx, fz), worldgen.SeaLevel)
	w.SetBlock(fx, y, fz, worldgen.Stone)
	for cy := y + 1; cy < y+7; cy++ {
		w.SetBlock(fx, cy, fz, worldgen.Air)
	}
	h.raining = true
	layers := func() int {
		s := w.At(fx, y+1, fz)
		if s >= snowLayer1 && s <= snowLayer1+7 {
			return int(s-snowLayer1) + 1
		}
		return 0
	}
	for i := 0; i < 6000 && layers() < 1; i++ {
		h.precipTick(h.playersRef, 0, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
	}
	if layers() != 1 {
		t.Fatalf("snowfall should lay a layer: %d", layers())
	}
	for i := 0; i < 6000; i++ {
		h.precipTick(h.playersRef, 0, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
	}
	if layers() != 1 {
		t.Fatalf("at the default height snow stays one layer: %d", layers())
	}
	h.rules.MaxSnowHeight = 3
	for i := 0; i < 20000 && layers() < 3; i++ {
		h.precipTick(h.playersRef, 0, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
	}
	if layers() != 3 {
		t.Fatalf("raised to three, snow should pile to three layers: %d", layers())
	}
	for i := 0; i < 6000; i++ {
		h.precipTick(h.playersRef, 0, chunkFloor(float64(fx)), chunkFloor(float64(fz)))
	}
	if layers() != 3 {
		t.Fatalf("and no further: %d", layers())
	}
}
