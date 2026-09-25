package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /locate poi through the dispatcher: a type by name, a tag with the type
// it matched, and the not-found line. A beehive is no village point: it must
// not make a place a village.
func TestLocatePoi(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	var y int
	onHub(t, h, func() {
		y = int(alice.y) + 3
		h.world.SetBlock(5, y, 0, worldgen.BlockBase("lodestone"))
		h.world.SetBlock(-3, y, 4, worldgen.BlockBase("beehive"))
	})
	s.handleCommand(alice, "locate poi minecraft:lodestone")
	s.handleCommand(alice, "locate poi #minecraft:bee_home")
	s.handleCommand(alice, "locate poi nether_portal")
	s.handleCommand(alice, "locate poi minecraft:nonsense")
	settle(t, h, logs, "P1")
	a := linesBetween(logs["alice"], "", "P1")
	for _, want := range []string{
		"The nearest minecraft:lodestone is at [5, ~, 0] (5 blocks away)",
		"The nearest #minecraft:bee_home (minecraft:beehive) is at [-3, ~, 4] (5 blocks away)",
		"Could not find a point of interest of type \"minecraft:nether_portal\" within a reasonable distance",
		"Can't find element 'minecraft:nonsense' of type 'minecraft:point_of_interest_type'",
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		if h.isVillage(blockPos{-3, y, 4}) {
			t.Error("a beehive made a village")
		}
	})
}
