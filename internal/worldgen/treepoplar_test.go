package worldgen

import "testing"

// A poplar: an upright trunk, a ring of one to four sideways branches on one
// layer beside it, a canopy no wider than its largest radius, log spokes
// bracing the wider canopies, and shelf mushrooms only low on the trunk.
func TestPoplarShape(t *testing.T) {
	up := axisLog(TreeFeatures["red_poplar"].Log, 1)
	shelfLo, shelfHi, _ := BlockRangeOK("shelf_mushroom")
	spokes, shelves := 0, 0
	for seed := int64(1); seed <= 120; seed++ {
		g, c := growTree("red_poplar", seed)
		if c == nil || len(g.leaves) == 0 {
			t.Fatalf("seed %d: no poplar", seed)
		}
		h := 0
		for g.logs[[3]int{0, h, 0}] == up {
			h++
		}
		if h < c.BaseHeight {
			t.Fatalf("seed %d: trunk %d tall, base height %d", seed, h, c.BaseHeight)
		}
		// Sideways logs beside the trunk, by layer: the lowest layer is the
		// branch ring, anything above is a canopy spoke.
		beside := map[int]int{}
		for p, st := range g.logs {
			if p[0] == 0 && p[2] == 0 {
				continue
			}
			switch {
			case st >= shelfLo && st <= shelfHi:
				shelves++
				// Beside one of the tree's logs (the trunk or, on a short
				// tree, a branch), dy 1..4 from the lowest trunk position —
				// the dirt the trunk converted under itself (y -1) here.
				nextToLog := false
				for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if st, ok := g.logs[[3]int{p[0] + o[0], p[1], p[2] + o[1]}]; ok && st != 0 && !(st >= shelfLo && st <= shelfHi) {
						nextToLog = true
					}
				}
				if !nextToLog || p[1] < 0 || p[1] > 3 {
					t.Errorf("seed %d: shelf mushroom at %v, not low on the trunk", seed, p)
				}
			case st == axisLog(c.Log, 0) || st == axisLog(c.Log, 2):
				if (st == axisLog(c.Log, 0)) != (p[0] != 0) && abs(p[0])+abs(p[2]) == 1 {
					t.Errorf("seed %d: log at %v lies across its direction", seed, p)
				}
				if abs(p[0])+abs(p[2]) == 1 {
					beside[p[1]]++
				} else {
					spokes++
				}
			default:
				t.Errorf("seed %d: unexpected block %d at %v", seed, st, p)
			}
		}
		low := 1 << 30
		for y := range beside {
			low = min(low, y)
		}
		if low != h-c.AboveBranchesMin-1 {
			t.Errorf("seed %d: branch ring at %d, want %d (the top less %d, less one)", seed, low, h-c.AboveBranchesMin-1, c.AboveBranchesMin)
		}
		if n := beside[low]; n < 1 || n > 4 {
			t.Errorf("seed %d: %d branches, want 1..4", seed, n)
		}
		spokes += len(beside) - 1
		for p := range g.leaves {
			if abs(p[0]) > 8 || abs(p[2]) > 8 {
				t.Fatalf("seed %d: leaf at %v, wider than the largest radius", seed, p)
			}
		}
	}
	if spokes == 0 {
		t.Error("no poplar in 120 grew a log spoke in its canopy")
	}
	if shelves == 0 {
		t.Error("no poplar in 120 grew a shelf mushroom (probability 0.4)")
	}
}

// The fallen poplar: a stump, a log run, brown mushrooms only.
func TestFallenPoplar(t *testing.T) {
	c := FallenTrees["fallen_poplar_tree"]
	if c == nil || !c.MushBrownOnly || c.ShelfProb != 0.8 {
		t.Fatalf("fallen poplar: %+v", c)
	}
	red := blockBase("red_mushroom")
	for seed := int64(1); seed <= 60; seed++ {
		d := map[[3]int]uint32{}
		PlaceFallenTree(c, 0, 0, 0, newTreeRNG(seed, 0, 0), TreeDriver{
			Set:  func(x, y, z int, st uint32, leaf bool) { d[[3]int{x, y, z}] = st },
			Free: func(x, y, z int) bool { return y >= 0 },
			Read: func(x, y, z int) uint32 { return Air },
		})
		for p, st := range d {
			if st == red {
				t.Fatalf("seed %d: a red mushroom on a fallen poplar at %v", seed, p)
			}
		}
	}
}
