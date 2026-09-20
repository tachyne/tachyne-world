package worldgen

import "testing"

// countStates tallies the given states over a run of generated chunks.
func countStates(t *testing.T, g *Generator, from, to int32, want map[uint32]string) map[string]int {
	t.Helper()
	byState := map[uint32]string{}
	for st, name := range want {
		byState[st] = name
	}
	total := map[string]int{}
	for cx := from; cx <= to; cx++ {
		for cz := from; cz <= to; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s := range ch.Sections {
				for _, st := range ch.Sections[s] {
					if name, ok := byState[st]; ok {
						total[name]++
					}
				}
			}
		}
	}
	return total
}

// The stone variants fill out a cave wall: granite, diorite and andesite
// through the middle of the world and tuff under it.
func TestStoneVariantBlobsGenerate(t *testing.T) {
	g := NewGenerator(1)
	total := countStates(t, g, -3, 3, map[uint32]string{
		stoneGranite: "granite", stoneDiorite: "diorite",
		stoneAndesite: "andesite", stoneTuff: "tuff",
	})
	for _, name := range []string{"granite", "diorite", "andesite", "tuff"} {
		if total[name] == 0 {
			t.Errorf("no %s in 49 chunks — the stone-variant blobs are missing", name)
		}
	}
}

// The spring rule: stone above and below, and of the four sides plus the
// floor exactly four stone with one way out.
func TestSpringRule(t *testing.T) {
	// A little world: stone everywhere, with the cells we open listed.
	world := func(open map[[3]int]bool) func(x, y, z int) uint32 {
		return func(x, y, z int) uint32 {
			if open[[3]int{x, y, z}] {
				return Air
			}
			return Stone
		}
	}
	if !springQualifies(world(map[[3]int]bool{{0, 0, 0}: true, {1, 0, 0}: true}), 0, 0, 0, false) {
		t.Error("an air cell in stone with one open side is a spring")
	}
	if springQualifies(world(map[[3]int]bool{{0, 0, 0}: true}), 0, 0, 0, false) {
		t.Error("walled in on every side, the fluid has nowhere to go")
	}
	if springQualifies(world(map[[3]int]bool{{0, 0, 0}: true, {1, 0, 0}: true, {-1, 0, 0}: true}), 0, 0, 0, false) {
		t.Error("two ways out is a cave, not a spring")
	}
	if springQualifies(world(map[[3]int]bool{{0, 0, 0}: true, {1, 0, 0}: true, {0, -1, 0}: true}), 0, 0, 0, false) {
		t.Error("nothing underneath: requires_block_below")
	}
	if springQualifies(world(map[[3]int]bool{{0, 0, 0}: true, {1, 0, 0}: true, {0, 1, 0}: true}), 0, 0, 0, false) {
		t.Error("nothing above either")
	}
	// Lava springs take a shorter list of stone: no snow or ice.
	snow := func(x, y, z int) uint32 {
		switch {
		case x == 0 && y == 0 && z == 0:
			return Air
		case x == 1 && y == 0 && z == 0:
			return Air
		case y == 1:
			return PackedIce
		}
		return Stone
	}
	if springQualifies(snow, 0, 0, 0, true) {
		t.Error("lava does not spring from packed ice")
	}
	if !springQualifies(snow, 0, 0, 0, false) {
		t.Error("water does: packed ice is one of its valid blocks")
	}
}

// Springs actually appear in a generated world: a fluid source walled in
// stone above and below is one, and nothing else makes that shape.
func TestSpringsGenerate(t *testing.T) {
	skipHeavy(t)
	g := NewGenerator(5)
	water, lava := 0, 0
	for cx := int32(-6); cx <= 6; cx++ {
		for cz := int32(-6); cz <= 6; cz++ {
			ch := g.GenerateChunk(cx, cz)
			at := func(x, y, z int) uint32 {
				yi := y - MinY
				if yi < 0 || yi/16 >= len(ch.Sections) || x < 0 || x > 15 || z < 0 || z > 15 {
					return Air
				}
				return ch.Sections[yi/16][((yi%16)*16+z)*16+x]
			}
			for y := MinY + 1; y < 190; y++ {
				for x := 1; x < 15; x++ {
					for z := 1; z < 15; z++ {
						s := at(x, y, z)
						if s != Water && s != Lava {
							continue
						}
						if springStone(at(x, y+1, z), s == Lava) && springStone(at(x, y-1, z), s == Lava) {
							if s == Water {
								water++
							} else {
								lava++
							}
						}
					}
				}
			}
		}
	}
	if water == 0 {
		t.Error("no water springs in 169 chunks")
	}
	if lava == 0 {
		t.Error("no lava springs in 169 chunks")
	}
}

// veryBiasedToBottom keeps most draws near the floor, which is what makes
// lava a deep hazard rather than a surface one.
func TestVeryBiasedToBottomLeansLow(t *testing.T) {
	r := newTreeRNG(99, 0, 0)
	low, total := 0, 4000
	for i := 0; i < total; i++ {
		y := veryBiasedToBottom(r, -64, 248, 8)
		if y < -64 || y > 248 {
			t.Fatalf("out of range: %d", y)
		}
		if y < 0 {
			low++
		}
	}
	if low < total/2 {
		t.Errorf("most draws should sit below y=0, got %d of %d", low, total)
	}
}

// Wild sugar cane grows on a shore: air over sand or dirt with water beside
// it, two to four canes tall.
func TestCanePatchNeedsWaterBeside(t *testing.T) {
	g := NewGenerator(3)
	ch := g.GenerateChunk(0, 0)
	reg := &owRegion{g: g, ch: ch, baseX: 0, baseZ: 0, cols: map[[2]int]column{}}
	r := newTreeRNG(1, 0, 0)
	// A dry column takes none.
	before := ch.Sections
	_ = before
	g.canePatch(r, reg, 8, 8)
	dry := 0
	for s := range ch.Sections {
		for _, st := range ch.Sections[s] {
			if st == sugarCane {
				dry++
			}
		}
	}
	if dry > 0 && !waterNear(reg, 8, 8) {
		t.Error("cane should not grow where there is no water beside it")
	}
}

// waterNear reports water within a few blocks of a column's surface.
func waterNear(reg *owRegion, x, z int) bool {
	y := reg.col(x, z).h
	for dx := -5; dx <= 5; dx++ {
		for dz := -5; dz <= 5; dz++ {
			if IsWater(reg.read(x+dx, y-1, z+dz)) {
				return true
			}
		}
	}
	return false
}
