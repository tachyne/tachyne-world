package worldgen

import "testing"

// The guard: a plant under a player's roof or on a player's floor is
// blocked, one in an open garden is not; a box with a build block in it is
// built on, one with only the world's own changes (air, water) is not.
func TestBuildGuard(t *testing.T) {
	g := NewGenerator(1)
	edits := map[[3]int]uint32{
		{3, 90, 3}:   BlockBase("oak_planks"), // a roof over (3,70,3)
		{8, 69, 8}:   BlockBase("oak_planks"), // a floor under (8,70,8)
		{12, 69, 12}: Air,                     // a dug hole under (12,70,12)
		{20, 70, 20}: WaterBase,               // the world's water: not a build
	}
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(cx, cz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == cx && int32(floorDiv16(p[2])) == cz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	bg := g.newBuildGuard(0, 0)
	for _, c := range []struct {
		x, y, z int
		blocked bool
	}{{3, 70, 3, true}, {8, 70, 8, true}, {12, 70, 12, true}, {5, 70, 5, false}, {3, 60, 3, false}} {
		if got := bg.decorationBlocked(c.x, c.y, c.z); got != c.blocked {
			t.Errorf("plant at %d,%d,%d: blocked %v, want %v", c.x, c.y, c.z, got, c.blocked)
		}
	}
	if !g.builtIn(0, 80, 0, 5, 95, 5) {
		t.Error("a box around the roof is not built in")
	}
	if g.builtIn(18, 60, 18, 22, 80, 22) {
		t.Error("water counted as a build")
	}
	if g.builtIn(-40, 0, -40, -20, 100, -20) {
		t.Error("an empty box counted as built")
	}
}
