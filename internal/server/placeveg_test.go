package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /place feature grows 26.3's vegetation features, each the single plant
// its configured feature is: grass on a grass block, a double plant with
// room above, a sugar cane column cut where the cells above stop being
// air, a flower refused on stone (it cannot survive there).
func TestCommandPlaceVegetation(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	grass := worldgen.BlockBase("grass_block")
	onHub(t, h, func() {
		for _, x := range []int{3, 5, 7} {
			h.world.SetBlock(x, 199, 3, grass)
		}
		h.world.SetBlock(9, 199, 3, worldgen.BlockBase("stone"))
		h.world.SetBlock(7, 203, 3, worldgen.BlockBase("stone")) // a lid three over the cane
	})
	for _, c := range []string{
		"place feature minecraft:grass 3 200 3",
		"place feature minecraft:tall_grass 5 200 3",
		"place feature minecraft:sugar_cane 7 200 3",
		"place feature minecraft:flower_default 9 200 3",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "V1")
	a := linesBetween(logs["alice"], "", "V1")
	for _, want := range []string{
		`Placed "minecraft:grass" at 3, 200, 3`,
		`Placed "minecraft:tall_grass" at 5, 200, 3`,
		`Placed "minecraft:sugar_cane" at 7, 200, 3`,
		"Failed to place feature",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	onHub(t, h, func() {
		w := h.world
		if got := w.At(3, 200, 3); got != worldgen.BlockBase("short_grass") {
			t.Errorf("no short grass: %d", got)
		}
		lower, upper := w.At(5, 200, 3), w.At(5, 201, 3)
		lo, hi := worldgen.BlockRange("tall_grass")
		li, _ := worldgen.InfoForState(lower)
		ui, _ := worldgen.InfoForState(upper)
		if lower < lo || lower > hi || upper < lo || upper > hi ||
			worldgen.GetProperty(li, lower, "half") != "lower" || worldgen.GetProperty(ui, upper, "half") != "upper" {
			t.Errorf("tall grass halves %d %d", lower, upper)
		}
		cane := worldgen.BlockBase("sugar_cane")
		n := 0
		for y := 200; y < 203; y++ {
			if w.At(7, y, 3) == cane {
				n++
			}
		}
		if n < 2 || w.At(7, 202, 3) == cane {
			t.Errorf("sugar cane column of %d (cut under the lid: the cell under it stays empty)", n)
		}
		if w.At(9, 200, 3) != worldgen.Air {
			t.Error("a flower grew on stone")
		}
	})
}
