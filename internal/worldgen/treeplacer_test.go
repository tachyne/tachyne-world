package worldgen

import (
	"math/rand"
	"testing"
)

// grown collects one tree into a map so its shape can be asserted on.
type grown struct {
	logs   map[[3]int]uint32
	leaves map[[3]int]bool
}

func growTree(name string, seed int64) (*grown, *TreeConfig) {
	c := TreeFeatures[name]
	g := &grown{logs: map[[3]int]uint32{}, leaves: map[[3]int]bool{}}
	rng := rand.New(rand.NewSource(seed))
	set := func(x, y, z int, state uint32, leaf bool) {
		if leaf {
			if _, isLog := g.logs[[3]int{x, y, z}]; !isLog {
				g.leaves[[3]int{x, y, z}] = true
			}
			return
		}
		g.logs[[3]int{x, y, z}] = state
		delete(g.leaves, [3]int{x, y, z})
	}
	PlaceTree(c, 0, 0, 0, rng, TreeDriver{
		Set:        set,
		Free:       func(x, y, z int) bool { return true },
		Read:       func(x, y, z int) uint32 { return Air },
		DirtGround: func(x, y, z int) bool { return false },
		SurfaceTop: func(x, z int) int { return -999 },
	})
	return g, c
}

// trunkColumns counts the distinct (x,z) a tree put logs in — 1 for a normal
// trunk, 4 for a mega trunk, more once branches are involved.
func (g *grown) trunkColumns() map[[2]int]bool {
	cols := map[[2]int]bool{}
	for p := range g.logs {
		cols[[2]int{p[0], p[2]}] = true
	}
	return cols
}

func (g *grown) height() int {
	minY, maxY := 1<<30, -(1 << 30)
	for p := range g.logs {
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	return maxY - minY + 1
}

// Every tree the table knows about grows: logs, leaves, and a height inside the
// range its own parameters allow. The old hand-rolled generator could only make
// one shape, so "it grew something" was never evidence of anything.
func TestEveryTreeFeatureGrows(t *testing.T) {
	for name, c := range TreeFeatures {
		anyLeaves := false
		for seed := int64(1); seed <= 12; seed++ {
			g, _ := growTree(name, seed)
			if len(g.logs) == 0 {
				t.Errorf("%s (seed %d): grew no logs", name, seed)
				continue
			}
			if len(g.leaves) > 0 {
				anyLeaves = true
			}
			// getTreeHeight is base + rand(A+1) + rand(B+1); a trunk may be
			// taller than that (branches climb) but never shorter than base.
			if h := g.height(); h < c.BaseHeight-1 {
				t.Errorf("%s (seed %d): height %d below base %d", name, seed, h, c.BaseHeight)
			}
		}
		if !anyLeaves {
			t.Errorf("%s: never grew any leaves in 12 tries", name)
		}
	}
}

// The mega species stand on four columns, which is the bug that started this:
// the generator built them on one while the sapling grower built four.
func TestMegaSpeciesStandOnFourColumns(t *testing.T) {
	for _, name := range []string{"dark_oak", "pale_oak", "mega_spruce", "mega_jungle_tree"} {
		c, ok := TreeFeatures[name]
		if !ok {
			t.Fatalf("%s missing from the feature table", name)
		}
		g, _ := growTree(name, 5)
		cols := g.trunkColumns()
		if len(cols) < 4 {
			t.Errorf("%s: %d trunk columns, want at least the 2x2", name, len(cols))
		}
		// …and the four are a contiguous square at the origin.
		for _, c2 := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
			if !cols[c2] {
				t.Errorf("%s: no log column at %v", name, c2)
			}
		}
		_ = c
	}
}

// Trees that vanilla gives branches to actually grow sideways. This is the
// thing a single-column-plus-blob generator can never do, and the reason a
// tachyne forest read as wrong without being obviously wrong.
func TestBranchingTreesGrowSideways(t *testing.T) {
	for _, name := range []string{"acacia", "cherry", "mangrove", "fancy_oak", "mega_jungle_tree"} {
		wide := false
		for seed := int64(1); seed <= 30 && !wide; seed++ {
			g, _ := growTree(name, seed)
			for p := range g.logs {
				if p[0] < -1 || p[0] > 2 || p[2] < -1 || p[2] > 2 {
					wide = true
					break
				}
			}
		}
		if !wide {
			t.Errorf("%s never grew a log away from its trunk — branches are missing", name)
		}
	}
	// …while a straight-trunked oak stays in its column.
	for seed := int64(1); seed <= 20; seed++ {
		g, _ := growTree("oak", seed)
		for p := range g.logs {
			if p[0] != 0 || p[2] != 0 {
				t.Fatalf("a plain oak put a log at %v — it should be one column", p)
			}
		}
	}
}

// A branch laid sideways carries the matching log axis, or the tree renders as
// a stack of upright logs lying in a row.
func TestSidewaysBranchesCarryTheirAxis(t *testing.T) {
	c := TreeFeatures["cherry"]
	sideways := false
	for seed := int64(1); seed <= 30 && !sideways; seed++ {
		g, _ := growTree("cherry", seed)
		for _, state := range g.logs {
			if state != c.Log { // base state is axis=x; +1 is y, +2 is z
				sideways = true
				break
			}
		}
	}
	if !sideways {
		t.Error("a cherry's branches should carry a horizontal log axis")
	}
}

// Height responds to the parameters rather than being fixed — a fancy oak
// (base 3, randA 11) must vary far more than a birch (base 5, randA 2).
func TestHeightVariesWithTheConfiguredRange(t *testing.T) {
	spread := func(name string) int {
		lo, hi := 1<<30, 0
		for seed := int64(1); seed <= 40; seed++ {
			g, _ := growTree(name, seed)
			h := g.height()
			if h < lo {
				lo = h
			}
			if h > hi {
				hi = h
			}
		}
		return hi - lo
	}
	fancy, birch := spread("fancy_oak"), spread("birch")
	if fancy <= birch {
		t.Errorf("fancy oak spread %d should exceed birch %d", fancy, birch)
	}
}
