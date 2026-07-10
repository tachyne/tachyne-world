package server

import (
	"testing"

	"tachyne/internal/worldgen"
)

// gridPather is a tiny test walkability surface: a flat plane at feet-y=64 with
// a set of blocked columns (walls). Everything else is walkable.
type gridPather struct {
	blocked map[[2]int]bool
	tall    map[[2]int]bool
	hazard  map[[3]int]uint32 // block state at (x,y,z); 0 = air (feet-y is 64)
}

func (g gridPather) Walkable(x, z int) bool     { return !g.blocked[[2]int{x, z}] }
func (g gridPather) MobFeet(x, z int) int       { return 64 }
func (g gridPather) TallObstacle(x, z int) bool { return g.tall[[2]int{x, z}] }
func (g gridPather) Block(x, y, z int) uint32   { return g.hazard[[3]int{x, y, z}] }

func TestPathAroundWall(t *testing.T) {
	// A vertical wall at x=3 spanning z=-2..2, with a gap nowhere — the mob at
	// (0,0) must detour around the ends to reach (6,0).
	g := gridPather{blocked: map[[2]int]bool{}}
	for z := -3; z <= 3; z++ {
		g.blocked[[2]int{3, z}] = true
	}
	path := findPath(g, defaultMalus, 0, 0, 6, 0)
	if len(path) == 0 {
		t.Fatal("expected a path around the wall, got none")
	}
	// The path must reach the goal and never step onto a blocked column.
	last := path[len(path)-1]
	if last.x != 6 || last.z != 0 {
		t.Fatalf("path should end at the goal, ended at (%d,%d)", last.x, last.z)
	}
	for _, p := range path {
		if g.blocked[[2]int{p.x, p.z}] {
			t.Fatalf("path stepped onto a wall column (%d,%d)", p.x, p.z)
		}
	}
	// It must actually detour: some waypoint leaves the z=0 lane.
	detoured := false
	for _, p := range path {
		if p.z != 0 {
			detoured = true
		}
	}
	if !detoured {
		t.Fatal("path went straight through the wall lane without detouring")
	}
}

func TestPathStepsAreContiguous(t *testing.T) {
	g := gridPather{blocked: map[[2]int]bool{}}
	path := findPath(g, defaultMalus, 0, 0, 8, 3)
	prev := pathPoint{0, 0}
	for _, p := range path {
		if abs(p.x-prev.x) > 1 || abs(p.z-prev.z) > 1 {
			t.Fatalf("non-adjacent step %v -> %v", prev, p)
		}
		prev = p
	}
}

func TestPathUnreachableIsBestEffort(t *testing.T) {
	// Fully wall the goal off with a box; the mob can't reach it but should get a
	// best-effort path toward it (non-nil, ending adjacent to the wall).
	g := gridPather{blocked: map[[2]int]bool{}}
	for _, d := range [][2]int{{4, -1}, {4, 0}, {4, 1}, {5, -1}, {5, 1}, {6, -1}, {6, 0}, {6, 1}, {5, 2}, {5, -2}} {
		g.blocked[[2]int{d[0], d[1]}] = true
	}
	path := findPath(g, defaultMalus, 0, 0, 5, 0) // (5,0) is inside the box
	if len(path) == 0 {
		t.Fatal("unreachable goal should still yield a best-effort path toward it")
	}
	// It should have made progress in +x (toward the goal), not stayed at start.
	last := path[len(path)-1]
	if last.x <= 0 {
		t.Fatalf("best-effort path made no progress toward goal: ended (%d,%d)", last.x, last.z)
	}
}

func TestPathTallObstacleBlocks(t *testing.T) {
	// A fence line (tall obstacle, not "blocked") must still turn the path.
	g := gridPather{blocked: map[[2]int]bool{}, tall: map[[2]int]bool{}}
	for z := -3; z <= 3; z++ {
		g.tall[[2]int{3, z}] = true
	}
	path := findPath(g, defaultMalus, 0, 0, 6, 0)
	for _, p := range path {
		if g.tall[[2]int{p.x, p.z}] {
			t.Fatalf("path crossed a tall obstacle at (%d,%d)", p.x, p.z)
		}
	}
}

// TestPathAvoidsLava proves the WalkNodeEvaluator malus at work: with no walls
// (every column Walkable) but a lava strip across the direct route, the mob
// detours around it instead of pathing through the lethal middle.
func TestPathAvoidsLava(t *testing.T) {
	g := gridPather{blocked: map[[2]int]bool{}, hazard: map[[3]int]uint32{}}
	for z := -1; z <= 1; z++ {
		g.hazard[[3]int{3, 64, z}] = worldgen.LavaBase // feet-y is 64
	}
	path := findPath(g, defaultMalus, 0, 0, 6, 0)
	if len(path) == 0 {
		t.Fatal("no path found around the lava")
	}
	for _, p := range path {
		if p.x == 3 && p.z >= -1 && p.z <= 1 {
			t.Fatalf("path routes through lava at %v", p)
		}
	}
	if last := path[len(path)-1]; last.x != 6 || last.z != 0 {
		t.Fatalf("path ends at %v, want (6,0)", last)
	}
}

// TestPathPrefersSafeOverFire checks positive malus (not impassable): a fire
// strip is crossable but costly, so given an equally-short clear detour the mob
// takes the detour.
func TestPathPrefersSafeOverFire(t *testing.T) {
	g := gridPather{blocked: map[[2]int]bool{}, hazard: map[[3]int]uint32{}}
	g.hazard[[3]int{2, 64, 0}] = fireDefault // one fire block on the straight line
	path := findPath(g, defaultMalus, 0, 0, 4, 0)
	if len(path) == 0 {
		t.Fatal("no path found")
	}
	for _, p := range path {
		if p.x == 2 && p.z == 0 {
			t.Fatal("path walked through fire when a safe detour existed")
		}
	}
}

// TestStriderPathsThroughLava verifies the per-mob malus override: with the
// same lava strip that a default mob detours around, a strider (lava malus 0)
// happily paths straight through it.
func TestStriderPathsThroughLava(t *testing.T) {
	g := gridPather{blocked: map[[2]int]bool{}, hazard: map[[3]int]uint32{}}
	for z := -1; z <= 1; z++ {
		g.hazard[[3]int{3, 64, z}] = worldgen.LavaBase
	}
	// Default mob refuses the middle column; a strider takes it.
	if malusFor(entityStrider)[hzLava] < 0 {
		t.Fatal("strider profile should treat lava as passable")
	}
	dpath := findPath(g, defaultMalus, 0, 0, 6, 0)
	for _, p := range dpath {
		if p.x == 3 && p.z == 0 {
			t.Fatal("default mob should NOT path through lava at (3,0)")
		}
	}
	spath := findPath(g, striderMalus, 0, 0, 6, 0)
	straight := false
	for _, p := range spath {
		if p.x == 3 && p.z == 0 {
			straight = true
		}
	}
	if !straight {
		t.Fatal("strider should path straight through lava at (3,0)")
	}
}

// TestFireImmuneCrossesFireFreely checks a blaze pays no fire cost (malus 0),
// while a default mob avoids it.
func TestFireImmuneCrossesFireFreely(t *testing.T) {
	if malusFor(entityBlaze)[hzFire] != 0 {
		t.Fatal("blaze should have zero fire malus")
	}
	if malusFor(entityBlaze)[hzLava] < 0 {
		t.Fatal("blaze lava should be passable (costly), not impassable")
	}
	if defaultMalus[hzLava] >= 0 {
		t.Fatal("default lava must stay impassable")
	}
}
