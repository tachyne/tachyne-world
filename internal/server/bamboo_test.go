package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bamboo and mushrooms.

// The state layout is arithmetic, so pin it against the registry ordering:
// age × leaves × stage with stage varying fastest.
func TestBambooStateLayout(t *testing.T) {
	lo, hi := worldgen.BlockRange("bamboo")
	if bambooBase != lo {
		t.Fatalf("bambooBase %d, want %d", bambooBase, lo)
	}
	if got := bambooState(1, bambooLeavesLarge, 1); got != hi {
		t.Errorf("age1/large/stage1 = %d, want the range end %d", got, hi)
	}
	for age := 0; age <= 1; age++ {
		for leaves := 0; leaves <= 2; leaves++ {
			for stage := 0; stage <= 1; stage++ {
				s := bambooState(age, leaves, stage)
				if bambooAge(s) != age || bambooLeaves(s) != leaves || bambooStage(s) != stage {
					t.Errorf("state %d round-tripped to (%d,%d,%d), want (%d,%d,%d)",
						s, bambooAge(s), bambooLeaves(s), bambooStage(s), age, leaves, stage)
				}
				if !isBamboo(s) {
					t.Errorf("state %d not recognised as bamboo", s)
				}
			}
		}
	}
}

// A lit stalk grows upward, and stops at the 16-segment cap.
func TestBambooGrowsAndStopsAtMaxHeight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 200, 100, 200

	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, bambooState(0, bambooLeavesSmall, 0))
	for k := 1; k <= 20; k++ {
		h.world.SetBlock(x, y+k, z, worldgen.Air)
	}

	for i := 0; i < 40000; i++ {
		for k := 0; k <= 18; k++ {
			s := h.world.At(x, y+k, z)
			if isBamboo(s) {
				h.tickBamboo(players, 0, x, y+k, z, s)
			}
		}
	}
	height := 0
	for k := 0; k < 20 && isBamboo(h.world.At(x, y+k, z)); k++ {
		height++
	}
	if height < 2 {
		t.Fatalf("bamboo never grew (height %d)", height)
	}
	if height > bambooMaxHeight {
		t.Errorf("bamboo grew to %d, past the %d cap", height, bambooMaxHeight)
	}
}

// Bamboo needs light 9 above it, like the other lit growers.
func TestBambooNeedsLight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 60, 40, 60 // underground

	for dx := -2; dx <= 2; dx++ {
		for dy := -1; dy <= 4; dy++ {
			for dz := -2; dz <= 2; dz++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	h.world.SetBlock(x, y, z, bambooState(0, bambooLeavesSmall, 0))
	h.world.SetBlock(x, y+1, z, worldgen.Air)

	if b := h.plantBrightness(0, x, y+1, z, 0); b >= bambooLightMin {
		t.Skipf("setup: brightness %d above the stalk, wanted dark", b)
	}
	for i := 0; i < 5000; i++ {
		h.tickBamboo(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y+1, z); got != worldgen.Air {
		t.Errorf("bamboo grew in the dark: state %d", got)
	}
}

// A capped segment (stage 1) is the top of the stalk and must not grow.
func TestCappedBambooDoesNotGrow(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 220, 100, 220

	capped := bambooState(1, bambooLeavesLarge, 1)
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, capped)
	h.world.SetBlock(x, y+1, z, worldgen.Air)

	for i := 0; i < 5000; i++ {
		h.tickBamboo(players, 0, x, y, z, capped)
	}
	if got := h.world.At(x, y+1, z); got != worldgen.Air {
		t.Errorf("a stage-1 segment grew: state %d above", got)
	}
}

// Mushrooms spread to nearby dark ground, and stop once crowded.
func TestMushroomSpreadsInTheDark(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 40, 40, 40

	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 3; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
			h.world.SetBlock(x+dx, y+4, z+dz, worldgen.Stone) // roof: keep it dark
		}
	}
	shroom := worldgen.BlockBase("brown_mushroom")
	h.world.SetBlock(x, y, z, shroom)

	spread := false
	for i := 0; i < 60000 && !spread; i++ {
		h.tickMushroom(players, 0, x, y, z, shroom)
		for dx := -4; dx <= 4 && !spread; dx++ {
			for dz := -4; dz <= 4 && !spread; dz++ {
				if (dx != 0 || dz != 0) && h.world.At(x+dx, y, z+dz) == shroom {
					spread = true
				}
			}
		}
	}
	if !spread {
		t.Error("mushroom never spread to nearby dark ground")
	}
}

// The population cap stops a mushroom carpet: five already nearby means no more.
func TestMushroomStopsWhenCrowded(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 80, 40, 80
	shroom := worldgen.BlockBase("brown_mushroom")

	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			h.world.SetBlock(x+dx, y, z+dz, worldgen.Air)
			h.world.SetBlock(x+dx, y+2, z+dz, worldgen.Stone)
		}
	}
	// Five in the box, including this one: already at the limit.
	for i := 0; i < 5; i++ {
		h.world.SetBlock(x+i, y, z, shroom)
	}
	before := 0
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			if h.world.At(x+dx, y, z+dz) == shroom {
				before++
			}
		}
	}
	for i := 0; i < 20000; i++ {
		h.tickMushroom(players, 0, x, y, z, shroom)
	}
	after := 0
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			if h.world.At(x+dx, y, z+dz) == shroom {
				after++
			}
		}
	}
	if after != before {
		t.Errorf("crowded mushrooms still spread: %d -> %d", before, after)
	}
}
