package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// End-to-end through the real handlePlace, the same entry point a gateway's
// Place frame lands on. The table tests cover the ported vanilla tables; this
// covers the wiring — that a hoe click actually reaches tryTill and mutates the
// world, rather than falling through to the "nothing placeable in hand" branch.
func TestHoeTillsThroughHandlePlace(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world

	hoe := itemByName["iron_hoe"]
	if hoe == 0 {
		t.Fatal("iron_hoe missing from the item table")
	}
	p.setHotbarSlot(0, hoe)
	p.held = 0

	// Every tillable, clicked on its top face with clear sky above.
	cases := []struct{ from, want string }{
		{"dirt", "farmland"},
		{"grass_block", "farmland"},
		{"dirt_path", "farmland"},
		{"coarse_dirt", "dirt"},
		{"rooted_dirt", "dirt"},
	}
	for i, c := range cases {
		x, y, z := 3+i, 70, 3
		w.SetBlock(x, y, z, worldgen.BlockBase(c.from))
		w.SetBlock(x, y+1, z, worldgen.Air)

		s.handlePlace(p, placeBody(x, y, z, 1)) // face 1 = top

		want := worldgen.BlockBase(c.want)
		if got := w.Block(x, y, z); got != want {
			t.Errorf("%s: got state %d, want %s (%d)", c.from, got, c.want, want)
		}
	}
}

// A grass block carries a `snowy` property, so the state in the world is not
// necessarily the base state the table is keyed on. Tilling must work at every
// state of a tillable block, not just its default.
func TestHoeTillsNonDefaultBlockStates(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world

	p.setHotbarSlot(0, itemByName["iron_hoe"])
	p.held = 0

	lo, hi := worldgen.BlockRange("grass_block")
	if hi <= lo {
		t.Skip("grass_block has a single state in this version")
	}
	for state := lo; state <= hi; state++ {
		x, y, z := 40, 70, int(state-lo)
		w.SetBlock(x, y, z, state)
		w.SetBlock(x, y+1, z, worldgen.Air)

		s.handlePlace(p, placeBody(x, y, z, 1))

		if got := w.Block(x, y, z); got != farmlandMin {
			t.Errorf("grass_block state %d (base %d): got %d, want farmland %d",
				state, lo, got, farmlandMin)
		}
	}
}

// onlyIfAirAbove: a covered block does not till, and the underside never does.
func TestHoeRespectsAirAbove(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world

	p.setHotbarSlot(0, itemByName["iron_hoe"])
	p.held = 0

	// Covered by stone: no till.
	x, y, z := 6, 70, 6
	w.SetBlock(x, y, z, worldgen.BlockBase("dirt"))
	w.SetBlock(x, y+1, z, worldgen.Stone)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if got := w.Block(x, y, z); got != worldgen.BlockBase("dirt") {
		t.Errorf("covered dirt tilled anyway: state %d", got)
	}

	// Clicked from below (face 0 = DOWN): no till even with air above.
	x2, y2, z2 := 7, 70, 7
	w.SetBlock(x2, y2, z2, worldgen.BlockBase("dirt"))
	w.SetBlock(x2, y2+1, z2, worldgen.Air)
	s.handlePlace(p, placeBody(x2, y2, z2, 0))
	if got := w.Block(x2, y2, z2); got != worldgen.BlockBase("dirt") {
		t.Errorf("dirt tilled from the DOWN face: state %d", got)
	}
}

// Planting through the same entry point: seeds on farmland become the crop.
func TestSeedsPlantThroughHandlePlace(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world

	seeds := itemByName["wheat_seeds"]
	if seeds == 0 {
		t.Fatal("wheat_seeds missing from the item table")
	}
	p.setHotbarSlot(0, seeds)
	p.held = 0

	x, y, z := 9, 70, 9
	w.SetBlock(x, y, z, farmlandMin)
	w.SetBlock(x, y+1, z, worldgen.Air)

	s.handlePlace(p, placeBody(x, y, z, 1)) // click farmland's top -> plant above

	if got, want := w.Block(x, y+1, z), worldgen.BlockBase("wheat"); got != want {
		t.Fatalf("seeds on farmland: got %d, want wheat %d", got, want)
	}

	// Same seeds on bare dirt must be refused.
	x2, y2, z2 := 11, 70, 11
	w.SetBlock(x2, y2, z2, worldgen.BlockBase("dirt"))
	w.SetBlock(x2, y2+1, z2, worldgen.Air)
	s.handlePlace(p, placeBody(x2, y2, z2, 1))
	if got := w.Block(x2, y2+1, z2); got != worldgen.Air {
		t.Errorf("seeds rooted on bare dirt: state %d", got)
	}
}

var _ = world.New // keep the import if the fixture changes

// A shovel flattens ground into a dirt path (ShovelItem.FLATTENABLES), at any
// state of each flattenable block.
func TestShovelFlattensThroughHandlePlace(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world

	p.setHotbarSlot(0, itemByName["iron_shovel"])
	p.held = 0

	path := worldgen.BlockBase("dirt_path")
	col := 0
	for _, name := range []string{"grass_block", "dirt", "podzol", "coarse_dirt",
		"mycelium", "rooted_dirt"} {
		lo, hi := worldgen.BlockRange(name)
		for st := lo; st <= hi; st++ {
			x, y, z := 60+col, 70, 60
			col++
			w.SetBlock(x, y, z, st)
			w.SetBlock(x, y+1, z, worldgen.Air)

			s.handlePlace(p, placeBody(x, y, z, 1))

			if got := w.Block(x, y, z); got != path {
				t.Errorf("%s state %d: got %d, want dirt_path %d", name, st, got, path)
			}
		}
	}

	// Covered ground does not flatten, and neither does the DOWN face.
	x, y, z := 80, 70, 60
	w.SetBlock(x, y, z, worldgen.BlockBase("dirt"))
	w.SetBlock(x, y+1, z, worldgen.Stone)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if got := w.Block(x, y, z); got != worldgen.BlockBase("dirt") {
		t.Errorf("covered dirt flattened anyway: %d", got)
	}

	x2, y2, z2 := 82, 70, 60
	w.SetBlock(x2, y2, z2, worldgen.BlockBase("dirt"))
	w.SetBlock(x2, y2+1, z2, worldgen.Air)
	s.handlePlace(p, placeBody(x2, y2, z2, 0))
	if got := w.Block(x2, y2, z2); got != worldgen.BlockBase("dirt") {
		t.Errorf("dirt flattened from the DOWN face: %d", got)
	}
}
