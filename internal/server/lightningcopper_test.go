package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A bolt that lands on oxidized copper resets it to plain copper, and its
// random walks scrape stages off the copper around it; struck waxed copper
// keeps its wax and stage.
func TestLightningClearsCopper(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	h.rules.Difficulty = diffEasy // no fire
	oxid := worldgen.BlockID("oxidized_copper")
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			w.SetBlock(x+dx, y, z+dz, oxid)
		}
	}
	h.strikeLightning(players, dimOverworld, float64(x)+0.5, float64(y)+1, float64(z)+0.5, false)
	if got := w.At(x, y, z); got != worldgen.BlockID("copper_block") {
		t.Fatalf("the struck block becomes plain copper, got %s", copperName(got))
	}
	cleaned := 0
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if (dx != 0 || dz != 0) && w.At(x+dx, y, z+dz) != oxid {
				cleaned++
			}
		}
	}
	if cleaned == 0 {
		t.Fatal("the bolt's walks scraped none of the surrounding copper")
	}

	// Waxed: the struck block keeps its wax and stage.
	h2 := newHub(world.New(1))
	h2.rules.Difficulty = diffEasy
	w2 := h2.world
	waxed := worldgen.BlockID("waxed_oxidized_copper")
	w2.SetBlock(0, 200, 0, waxed)
	h2.strikeLightning(map[int32]*tracked{}, dimOverworld, 0.5, 201, 0.5, false)
	if w2.At(0, 200, 0) != waxed {
		t.Fatal("struck waxed copper is left alone")
	}
}
