package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /place template through the dispatcher: the template lands block for
// block (plain, and turned and mirrored), integrity 0 places nothing, and an
// unknown template is refused.
func TestCommandPlaceTemplate(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	tm := worldgen.TemplateByName("igloo/top")
	if tm == nil {
		t.Fatal("no igloo/top template")
	}
	s.handleCommand(alice, "place template minecraft:igloo/top 0 150 0")
	s.handleCommand(alice, "place template igloo/top 20 150 20 clockwise_90 left_right")
	s.handleCommand(alice, "place template igloo/top 0 170 0 none none 0 5")
	s.handleCommand(alice, "place template minecraft:nope 0 150 0")
	s.handleCommand(alice, "place template igloo/top 0 150 0 sideways")
	settle(t, h, logs, "P1")
	a := linesBetween(logs["alice"], "", "P1")
	for _, want := range []string{
		`Loaded template "minecraft:igloo/top" at 0, 150, 0`,
		`Loaded template "minecraft:igloo/top" at 20, 150, 20`,
		`Loaded template "minecraft:igloo/top" at 0, 170, 0`,
		`There is no template with ID "minecraft:nope"`,
		"Invalid rotation: sideways",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	onHub(t, h, func() {
		for _, c := range tm.PlaceCells(0, 150, 0, 0, worldgen.MirrorNone, nil) {
			if got := h.world.At(c.X, c.Y, c.Z); got != c.State {
				t.Errorf("plain: %d,%d,%d is %d, want %d", c.X, c.Y, c.Z, got, c.State)
				return
			}
		}
		for _, c := range tm.PlaceCells(20, 150, 20, 1, worldgen.MirrorLeftRight, nil) {
			if got := h.world.At(c.X, c.Y, c.Z); got != c.State {
				t.Errorf("turned: %d,%d,%d is %d, want %d", c.X, c.Y, c.Z, got, c.State)
				return
			}
		}
		for _, c := range tm.PlaceCells(0, 170, 0, 0, worldgen.MirrorNone, nil) {
			if c.State != worldgen.Air && h.world.At(c.X, c.Y, c.Z) == c.State {
				t.Errorf("integrity 0 placed %d at %d,%d,%d", c.State, c.X, c.Y, c.Z)
				return
			}
		}
	})
}

// /place feature grows a tree from the given position; an unknown feature
// is refused by name.
func TestCommandPlaceFeature(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() { h.world.SetBlock(5, 199, 5, parseBlockOrAir("grass_block")) })
	s.handleCommand(alice, "place feature minecraft:oak 5 200 5")
	s.handleCommand(alice, "place feature minecraft:nope 5 200 5")
	settle(t, h, logs, "F1")
	a := linesBetween(logs["alice"], "", "F1")
	for _, want := range []string{
		`Placed "minecraft:oak" at 5, 200, 5`,
		"Can't find element 'minecraft:nope' in registry 'minecraft:worldgen/configured_feature'",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	onHub(t, h, func() {
		if !worldgen.IsLog(h.world.At(5, 200, 5)) {
			t.Errorf("no trunk at 5,200,5: %d", h.world.At(5, 200, 5))
		}
	})
}

// /place structure builds the igloo at the chunk corner and a jigsaw grows
// from a pool; unknown and unsupported structures and pools are refused.
func TestCommandPlaceStructureAndJigsaw(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "place structure minecraft:igloo 3 ~ 3")
	s.handleCommand(alice, "place structure minecraft:mansion")
	s.handleCommand(alice, "place structure minecraft:nope")
	s.handleCommand(alice, "place jigsaw minecraft:trial_chambers/chamber/entrance_cap minecraft:entrance_cap 1 0 200 0")
	s.handleCommand(alice, "place jigsaw minecraft:nope minecraft:x 1")
	s.handleCommand(alice, "place jigsaw minecraft:trial_chambers/chamber/entrance_cap minecraft:entrance_cap 21")
	settle(t, h, logs, "S1")
	a := linesBetween(logs["alice"], "", "S1")
	for _, want := range []string{
		"Failed to place structure",
		`There is no structure with type "minecraft:nope"`,
		"Generated jigsaw at 0, 200, 0",
		`There is no template pool with type "minecraft:nope"`,
		"Integer must be between 1 and 20, found 21",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	igloo := false
	for _, l := range a {
		igloo = igloo || strings.HasPrefix(l, `Generated structure "minecraft:igloo" at 3, `)
	}
	if !igloo {
		t.Errorf("no igloo success line in\n%s", strings.Join(a, "\n"))
	}
	onHub(t, h, func() {
		pieces, ok := h.world.Gen().PlaceStructurePieces("igloo", 3, 3)
		if !ok {
			t.Error("igloo pieces")
			return
		}
		top := pieces[0]
		for _, c := range top.Tmpl.PlaceCells(top.OX, top.OY, top.OZ, 0, worldgen.MirrorNone, nil) {
			if got := h.world.At(c.X, c.Y, c.Z); got != c.State {
				t.Errorf("igloo top: %d,%d,%d is %d, want %d", c.X, c.Y, c.Z, got, c.State)
				return
			}
		}
		placed := 0
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				for y := 190; y <= 205; y++ {
					if h.world.At(x, y, z) != worldgen.Air {
						placed++
					}
				}
			}
		}
		if placed == 0 {
			t.Error("the jigsaw placed no blocks")
		}
	})
}
