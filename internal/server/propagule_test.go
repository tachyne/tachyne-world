package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MangrovePropaguleBlock: ripens while hanging, grows a tree once planted.

// The layout is age × hanging × stage × waterlogged with waterlogged fastest
// and each boolean's `true` first — pin every combination.
func TestPropaguleStateLayout(t *testing.T) {
	lo, hi := worldgen.BlockRange("mangrove_propagule")
	if propaguleBase != lo || propaguleHi != hi {
		t.Fatalf("range %d..%d, want %d..%d", propaguleBase, propaguleHi, lo, hi)
	}
	if got := propaguleState(0, true, 0, true); got != lo {
		t.Errorf("age0/hanging/stage0/wet = %d, want the range start %d", got, lo)
	}
	if got := propaguleState(propaguleMaxAge, false, 1, false); got != hi {
		t.Errorf("max/planted/stage1/dry = %d, want the range end %d", got, hi)
	}
	seen := map[uint32]bool{}
	for age := 0; age <= propaguleMaxAge; age++ {
		for _, hang := range []bool{true, false} {
			for stage := 0; stage <= 1; stage++ {
				for _, wet := range []bool{true, false} {
					s := propaguleState(age, hang, stage, wet)
					if seen[s] {
						t.Fatalf("state %d produced twice", s)
					}
					seen[s] = true
					if propaguleAge(s) != age || propaguleHanging(s) != hang ||
						propaguleStage(s) != stage || propaguleWet(s) != wet {
						t.Errorf("state %d round-tripped to (%d,%v,%d,%v), want (%d,%v,%d,%v)",
							s, propaguleAge(s), propaguleHanging(s), propaguleStage(s),
							propaguleWet(s), age, hang, stage, wet)
					}
					if !isPropagule(s) {
						t.Errorf("state %d not recognised as a propagule", s)
					}
				}
			}
		}
	}
	if len(seen) != 40 {
		t.Errorf("covered %d states, want all 40", len(seen))
	}
}

// Hanging under mangrove leaves, it ripens to max age with no roll and no light.
func TestHangingPropaguleRipens(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 30, 40, 30 // underground: proves no light gate applies

	leaf, _ := worldgen.BlockRange("mangrove_leaves")
	h.world.SetBlock(x, y+1, z, leaf)
	h.world.SetBlock(x, y, z, propaguleState(0, true, 0, false))

	for i := 0; i < 50; i++ {
		h.tickPropagule(players, 0, x, y, z, h.world.At(x, y, z))
	}
	got := h.world.At(x, y, z)
	if propaguleAge(got) != propaguleMaxAge {
		t.Errorf("hanging propagule reached age %d, want %d", propaguleAge(got), propaguleMaxAge)
	}
	if !propaguleHanging(got) {
		t.Error("it stopped hanging while ripening")
	}
}

// With nothing to hang from it does not ripen.
func TestHangingPropaguleNeedsLeaves(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 50, 40, 50

	start := propaguleState(0, true, 0, false)
	h.world.SetBlock(x, y+1, z, worldgen.Air)
	h.world.SetBlock(x, y, z, start)

	for i := 0; i < 100; i++ {
		h.tickPropagule(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != start {
		t.Errorf("propagule ripened with nothing above: %d -> %d", start, got)
	}
}

// Planted, it advances its stage and then grows a mangrove.
func TestPlantedPropaguleGrowsAMangrove(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 70, 100, 70

	// A mangrove's roots must FIND ground or the whole tree refuses — a
	// single floating block is not a swamp, so lay a real floor.
	for dx := -10; dx <= 10; dx++ {
		for dz := -10; dz <= 10; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Dirt)
		}
	}
	h.world.SetBlock(x, y, z, propaguleState(4, false, 0, false))
	for k := 1; k <= 12; k++ {
		h.world.SetBlock(x, y+k, z, worldgen.Air)
	}

	for i := 0; i < 8000 && isPropagule(h.world.At(x, y, z)); i++ {
		h.tickPropagule(players, 0, x, y, z, h.world.At(x, y, z))
	}
	got := h.world.At(x, y, z)
	if isPropagule(got) {
		t.Fatalf("planted propagule never grew (state %d)", got)
	}
	// The trunk stands on STILTS now — one to seven blocks up, with a root
	// under it — so the propagule's own cell holds air or a root, and the
	// log is found above.
	wantLog, _ := worldgen.BlockRange("mangrove_log")
	trunkBase := 0
	for dy := 1; dy <= 8; dy++ {
		if s := h.world.At(x, y+dy, z); s >= wantLog && s <= wantLog+2 {
			trunkBase = y + dy
			break
		}
	}
	if trunkBase == 0 {
		t.Fatalf("no mangrove log above the grown propagule (cell now %d)", got)
	}
	if below := h.world.At(x, trunkBase-1, z); !worldgen.IsMangroveRoots(below) {
		t.Errorf("under the raised trunk: state %d, want mangrove roots", below)
	}
	leafLo, leafHi := worldgen.BlockRange("mangrove_leaves")
	found := false
	for dy := 1; dy <= 12 && !found; dy++ {
		for dx := -3; dx <= 3 && !found; dx++ {
			for dz := -3; dz <= 3 && !found; dz++ {
				if s := h.world.At(x+dx, y+dy, z+dz); s >= leafLo && s <= leafHi {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("no mangrove leaves above the grown trunk")
	}
}

// The propagule must not be picked up by the ordinary sapling handler, which
// would grow the wrong species and skip the hanging behaviour entirely.
func TestPropaguleIsNotASapling(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 90, 100, 90

	s := propaguleState(0, false, 0, false)
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, s)
	if h.tickSapling(players, 0, x, y, z, s) {
		t.Error("tickSapling claimed a mangrove propagule")
	}
}
