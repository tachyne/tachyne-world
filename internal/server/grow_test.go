package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// All tests place blocks high in open air (y=200) so skyLit/logNearby behave
// deterministically regardless of the seed-1 terrain below.

func TestCropGrowsInLight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 5, 200, 5
	h.world.SetBlock(x, y, z, worldgen.BlockBase("wheat")) // wheat, age 0
	for i := 0; i < 8 && h.world.At(x, y, z) < (worldgen.BlockBase("wheat")+7); i++ {
		h.tickCrop(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got <= worldgen.BlockBase("wheat") {
		t.Errorf("wheat did not grow in light: state %d", got)
	}
}

func TestStackPlantGrowsUpward(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 5, 200, 5
	h.world.SetBlock(x, y, z, caneMax) // sugar cane at age 15 (ready to grow)
	h.tickStackPlant(players, x, y, z, caneMax, caneMin)
	if got := h.world.At(x, y+1, z); got != caneMin {
		t.Errorf("cane did not grow a new stalk above: got %d want %d", got, caneMin)
	}
	if got := h.world.At(x, y, z); got != caneMin {
		t.Errorf("grown cane did not reset its age: got %d", got)
	}
}

func TestLeafDecaysWhenIsolated(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 5, 200, 5
	h.world.SetBlock(x, y, z, worldgen.OakLeaves) // default = non-persistent
	h.tickLeaf(players, x, y, z, worldgen.OakLeaves)
	if got := h.world.At(x, y, z); got != worldgen.Air {
		t.Errorf("isolated non-persistent leaf should decay, got %d", got)
	}
}

func TestPersistentLeafSurvives(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 5, 200, 5
	persistentOakLeaf := worldgen.BlockBase("oak_leaves") // distance 1, persistent=true
	h.world.SetBlock(x, y, z, persistentOakLeaf)
	h.tickLeaf(players, x, y, z, persistentOakLeaf)
	if got := h.world.At(x, y, z); got != persistentOakLeaf {
		t.Errorf("persistent leaf should not decay, got %d", got)
	}
}

func TestLeafSurvivesNearLog(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 5, 200, 5
	h.world.SetBlock(x+1, y, z, worldgen.OakLog) // a log right next to it
	h.world.SetBlock(x, y, z, worldgen.OakLeaves)
	h.tickLeaf(players, x, y, z, worldgen.OakLeaves)
	if got := h.world.At(x, y, z); got != worldgen.OakLeaves {
		t.Errorf("leaf next to a log should survive, got %d", got)
	}
}

// TestSpeciesLeavesSurviveNearTheirLog guards the whole-forest-strip bug:
// every tree species' canopy must survive next to its OWN log — logNearby has
// to recognise all wood types, not just oak (spruce/birch trees were rotting
// to bare trunks because logNearby only knew oak).
func TestSpeciesLeavesSurviveNearTheirLog(t *testing.T) {
	cases := []struct {
		name      string
		log, leaf uint32
	}{
		{"spruce", worldgen.SpruceLog, worldgen.SpruceLeaves},
		{"birch", worldgen.BirchLog, worldgen.BirchLeaves},
		{"jungle", worldgen.JungleLog, worldgen.JungleLeaves},
		{"acacia", worldgen.AcaciaLog, worldgen.AcaciaLeaves},
		{"dark_oak", worldgen.DarkOakLog, worldgen.DarkOakLeaves},
		{"cherry", worldgen.CherryLog, worldgen.CherryLeaves},
		{"mangrove", worldgen.MangroveLog, worldgen.MangroveLeaves},
	}
	for _, c := range cases {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		x, y, z := 5, 200, 5
		h.world.SetBlock(x+1, y, z, c.log)
		h.world.SetBlock(x, y, z, c.leaf)
		h.tickLeaf(players, x, y, z, c.leaf)
		if got := h.world.At(x, y, z); got != c.leaf {
			t.Errorf("%s leaf next to its log decayed (got %d) — logNearby misses %s logs", c.name, got, c.name)
		}
	}
}
