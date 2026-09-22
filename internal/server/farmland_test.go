package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// clearBox wipes the ±4×±1 farmland scan box to air so farmlandNearWater is
// deterministic regardless of the generated terrain under the test column.
func clearBox(w interface {
	SetBlock(x, y, z int, s uint32)
}, x, y, z int) {
	for dy := -1; dy <= 2; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
}

func TestFarmland(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world
	farmland := worldgen.BlockBase("farmland")

	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival

		// Hydration: dry soil (moisture 0) with a water source in range → 7.
		clearBox(w, 5, 100, 5)
		w.SetBlock(5, 100, 5, farmland) // moisture 0
		w.SetBlock(7, 100, 5, worldgen.WaterBase)
		h.farmlandRandomTick(h.playersRef, 0, 5, 100, 5, w.At(5, 100, 5))
		if got := w.At(5, 100, 5); got != farmland+7 {
			t.Errorf("hydrate: moisture state %d, want %d", got-farmland, 7)
		}

		// Dehydration: moist soil (moisture 5), no water/rain → 4.
		clearBox(w, 5, 100, 40)
		w.SetBlock(5, 100, 40, farmland+5)
		h.farmlandRandomTick(h.playersRef, 0, 5, 100, 40, w.At(5, 100, 40))
		if got := w.At(5, 100, 40); got != farmland+4 {
			t.Errorf("dehydrate: moisture state %d, want 4", got-farmland)
		}

		// Dry-out: bone-dry soil (moisture 0), nothing growing → reverts to dirt.
		clearBox(w, 5, 100, 80)
		w.SetBlock(5, 100, 80, farmland) // moisture 0
		h.farmlandRandomTick(h.playersRef, 0, 5, 100, 80, w.At(5, 100, 80))
		if got := w.At(5, 100, 80); got != worldgen.Dirt {
			t.Errorf("dry-out: block %d, want dirt %d", got, worldgen.Dirt)
		}

		// Maintained: dry soil with wheat on top stays tilled (won't revert).
		clearBox(w, 5, 100, 120)
		w.SetBlock(5, 100, 120, farmland) // moisture 0
		w.SetBlock(5, 101, 120, worldgen.BlockBase("wheat"))
		h.farmlandRandomTick(h.playersRef, 0, 5, 100, 120, w.At(5, 100, 120))
		if got := w.At(5, 100, 120); got != farmland {
			t.Errorf("maintained: block %d, want farmland (unchanged)", got)
		}

		// Trampling: a hard landing turns soil to dirt and pops the crop above.
		clearBox(w, 5, 100, 160)
		w.SetBlock(5, 100, 160, farmland+7)
		w.SetBlock(5, 101, 160, worldgen.BlockBase("wheat")+7) // mature crop
		h.tramplePlayer(h.playersRef, tr, 5, 100, 160, 10)     // dist 10 → prob ~9.5, certain
		if got := w.At(5, 100, 160); got != worldgen.Dirt {
			t.Errorf("trample: soil %d, want dirt", got)
		}
		if got := w.At(5, 101, 160); got != worldgen.Air {
			t.Errorf("trample: crop %d not popped", got)
		}
	})
}

// FarmBlock.canSurvive and DirtPathBlock.canSurvive are both about what sits
// ABOVE: put a solid block on a tilled row or a trodden path and it goes back
// to dirt where it stands (their shared turnToDirt), instead of surviving
// forever under somebody's chest.
func TestTilledSoilRevertsUnderASolidBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	const x, y, z = 66, 180, 66
	flatFloor(w, x, y, z, 2)

	for _, soil := range []struct {
		name  string
		state uint32
	}{
		{"farmland", farmlandMin + 7},
		{"a dirt path", dirtPathState},
	} {
		w.SetBlock(x, y, z, soil.state)
		h.setBlockAt(players, 0, blockPos{x, y + 1, z}, worldgen.BlockBase("chest"))
		if got := w.At(x, y, z); got != worldgen.Dirt {
			t.Errorf("%s under a chest is %d, want dirt (%d)", soil.name, got, worldgen.Dirt)
		}

		// The lids vanilla does not count: the fence gate both classes name,
		// a carpet (too flat to be solid), and the cell a pushed block rides
		// in while the piston is still moving it.
		for _, lid := range []struct {
			name  string
			state uint32
		}{
			{"a fence gate", worldgen.BlockBase("oak_fence_gate")},
			{"a carpet", worldgen.BlockBase("white_carpet")},
			{"a travelling block", movingPistonState([3]int{1, 0, 0}, false)},
		} {
			w.SetBlock(x, y, z, soil.state)
			h.setBlockAt(players, 0, blockPos{x, y + 1, z}, lid.state)
			if got := w.At(x, y, z); got != soil.state {
				t.Errorf("%s under %s is %d, want it left alone (%d)",
					soil.name, lid.name, got, soil.state)
			}
			h.setBlockAt(players, 0, blockPos{x, y + 1, z}, worldgen.Air)
		}
	}
}

// Vanilla's solidity is a threshold on the collision box, not "does it
// collide": the bounds must average 0.729 of a block or stand a block tall.
// These are the cases the hand-written list used to get wrong.
func TestSolidLidFollowsVanillasThreshold(t *testing.T) {
	for _, c := range []struct {
		block string
		lid   bool
	}{
		{"stone", true}, {"oak_slab", true}, {"chest", true}, {"oak_door", true},
		{"cactus", true}, {"cake", true}, // 14/16 x 8/16 x 14/16 averages 0.75
		{"white_carpet", false}, {"candle", false}, {"player_head", false},
		{"pitcher_crop", false}, {"lily_pad", false}, {"flower_pot", false},
		{"comparator", false}, {"repeater", false}, {"sea_pickle", false},

		// The force flags beat the threshold, and these five were all on the
		// wrong side of it until the table started asking the game instead of
		// reimplementing calculateSolid with a hand-kept list of two. Each is
		// read straight off its registration in vanilla's Blocks:
		{"ladder", false},            // forceSolidOff — despite a 0.729 average
		{"turtle_egg", true},         // forceSolidOn — despite a tiny box
		{"amethyst_cluster", true},   // forceSolidOn
		{"conduit", true},            // forceSolidOn
		{"shulker_box", true},        // forceSolidOn, via shulkerBoxProperties —
		{"purple_shulker_box", true}, // the old list had this one backwards
	} {
		if got := solidLid(worldgen.BlockBase(c.block)); got != c.lid {
			t.Errorf("%s: solidLid=%v, want %v", c.block, got, c.lid)
		}
	}
}
