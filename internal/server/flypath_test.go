package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// airBox hollows out a region so a flier has somewhere to fly, and returns the
// world. Everything below the box is solid so the world has a floor.
func airBox(t *testing.T, w *world.World, x0, y0, z0, x1, y1, z1 int) {
	t.Helper()
	stone, _ := worldgen.BlockRange("stone")
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			w.SetBlock(x, y0-1, z, stone)
			for y := y0; y <= y1; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
}

func TestFlyPathCrossesOpenAir(t *testing.T) {
	w := world.New(1)
	airBox(t, w, -2, 70, -2, 20, 78, 6)
	from, to := blockPos{0, 71, 0}, blockPos{18, 71, 2}
	path := findFlyPath(w, from, to, flyPathBudget)
	if len(path) == 0 {
		t.Fatal("no route across open air")
	}
	if got := path[len(path)-1]; got != to {
		t.Errorf("route ends at %v, want the goal %v", got, to)
	}
	// Every step must be a single cell move — no teleporting across the map.
	prev := from
	for _, n := range path {
		if abs(n.x-prev.x) > 1 || abs(n.y-prev.y) > 1 || abs(n.z-prev.z) > 1 {
			t.Fatalf("route jumps from %v to %v", prev, n)
		}
		prev = n
	}
}

// The point of pathing: an obstacle between the bee and its goal is flown
// around, not into. A straight-line steer fails this outright.
//
// The obstacle is deliberately modest. Vanilla gives a bee only 256 visited
// nodes (FOLLOW_RANGE 16 x 16), which a long detour exhausts — that is exactly
// why BeeGoToHiveGoal carries a give-up timer and a blacklist rather than
// assuming a route always exists. Asserting that a huge maze is solved would
// claim more than vanilla does.
func TestFlyPathGoesAroundAWall(t *testing.T) {
	w := world.New(1)
	airBox(t, w, -2, 70, -6, 14, 80, 6)
	stone, _ := worldgen.BlockRange("stone")
	// A pillar squarely on the straight line, with air all around it.
	for y := 68; y <= 76; y++ {
		for dz := -1; dz <= 1; dz++ {
			w.SetBlock(6, y, dz, stone)
		}
	}
	from, to := blockPos{0, 71, 0}, blockPos{12, 71, 0}
	path := findFlyPath(w, from, to, flyPathBudget)
	if len(path) == 0 {
		t.Fatal("no route around the wall")
	}
	for _, n := range path {
		if worldgen.Collides(w.At(n.x, n.y, n.z)) {
			t.Fatalf("route passes through solid block at %v", n)
		}
	}
	if got := path[len(path)-1]; got != to {
		t.Errorf("route ends at %v, want %v", got, to)
	}
}

// A sealed goal has no route at all — the caller must be told so it can give
// up rather than press against the wall for ever.
func TestFlyPathReportsNoRoute(t *testing.T) {
	w := world.New(1)
	airBox(t, w, -2, 70, -2, 10, 76, 6)
	stone, _ := worldgen.BlockRange("stone")
	for dx := -1; dx <= 1; dx++ { // brick the goal in
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(8+dx, 72+dy, 2+dz, stone)
			}
		}
	}
	if path := findFlyPath(w, blockPos{0, 71, 0}, blockPos{8, 72, 2}, flyPathBudget); len(path) != 0 {
		t.Errorf("found a route into a sealed cell: %v", path)
	}
}

// A diagonal may not cut the corner between two solid blocks.
func TestFlyPathDoesNotCutCorners(t *testing.T) {
	w := world.New(1)
	airBox(t, w, -2, 70, -2, 6, 76, 6)
	stone, _ := worldgen.BlockRange("stone")
	// Block both orthogonal components of the north-east diagonal out of the
	// start, leaving only the diagonal cell itself open.
	w.SetBlock(1, 71, 0, stone)
	w.SetBlock(0, 71, 1, stone)
	from := blockPos{0, 71, 0}
	path := findFlyPath(w, from, blockPos{1, 71, 1}, flyPathBudget)
	for i, n := range path {
		if i == 0 && n == (blockPos{1, 71, 1}) {
			t.Error("squeezed diagonally between two solid blocks on the first step")
		}
	}
}

// The hive block itself is solid, so a route to one aims at the open cell in
// front of it rather than failing.
func TestFlyPathToASolidGoalAimsBeside(t *testing.T) {
	w := world.New(1)
	airBox(t, w, -2, 70, -2, 12, 78, 6)
	stone, _ := worldgen.BlockRange("stone")
	hive := blockPos{9, 72, 2}
	w.SetBlock(hive.x, hive.y, hive.z, stone)
	path := findFlyPath(w, blockPos{0, 71, 0}, hive, flyPathBudget)
	if len(path) == 0 {
		t.Fatal("no route to a hive")
	}
	end := path[len(path)-1]
	if worldgen.Collides(w.At(end.x, end.y, end.z)) {
		t.Errorf("route ends inside the solid hive at %v", end)
	}
	if flyDist(end, hive) > 2 {
		t.Errorf("route ends %v, too far from the hive at %v", end, hive)
	}
}
