package worldgen

import "testing"

// floorOnly is a generator whose only player edit is a floor block under
// (x, y, z).
func floorOnly(x, y, z int) *Generator {
	g := NewGenerator(1)
	g.SetEditLookup(func(a, b, c int) (uint32, bool) {
		if a == x && b == y-1 && c == z {
			return StoneBricks, true
		}
		return 0, false
	})
	return g
}

// findTree scans for a standing generated tree whose canopy reaches the next
// chunk east: a trunk base (upright log on the ground, logs above it) in the
// east of a chunk, for which a floor under the trunk changes the neighbour.
func findTree(t *testing.T, g *Generator) (x, y, z int, cx, cz int32) {
	t.Helper()
	for cz = -40; cz < 40; cz++ {
		for cx = -40; cx < 40; cx++ {
			ch := g.GenerateChunk(cx, cz)
			for lx := 13; lx < 16; lx++ {
				for lz := 4; lz < 12; lz++ {
					wx, wz := int(cx)*16+lx, int(cz)*16+lz
					h := g.Height(wx, wz)
					if !IsLog(sectionBlockAt(ch, lx, h, lz)) || !IsLog(sectionBlockAt(ch, lx, h+1, lz)) || !IsLog(sectionBlockAt(ch, lx, h+2, lz)) {
						continue
					}
					if !sameChunk(g.GenerateChunk(cx+1, cz), floorOnly(wx, h, wz).GenerateChunk(cx+1, cz)) {
						return wx, h, wz, cx, cz
					}
				}
			}
		}
	}
	t.Fatal("no tree crossing a chunk border found")
	return
}

// leavesNear counts leaves of a chunk within 5 blocks (horizontally) of a
// trunk at (x, y, z), from y up.
func leavesNear(ch *Chunk, cx, cz int32, x, y, z int) int {
	n := 0
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			if wx-x > 5 || x-wx > 5 || wz-z > 5 || z-wz > 5 {
				continue
			}
			for wy := y; wy < y+24; wy++ {
				if IsLeaves(sectionBlockAt(ch, lx, wy, lz)) {
					n++
				}
			}
		}
	}
	return n
}

func leavesIn(ch *Chunk) int {
	n := 0
	for _, sec := range ch.Sections {
		for _, s := range sec {
			if IsLeaves(s) {
				n++
			}
		}
	}
	return n
}

// A generated tree that would grow into a player's build is not grown — in
// every chunk it reaches — while a block or two in a canopy leaves it be.
func TestGeneratedTreesAvoidBuilds(t *testing.T) {
	plain := NewGenerator(1)
	x, y, z, cx, cz := findTree(t, plain)
	base := plain.GenerateChunk(cx, cz)
	lx, lz := x-int(cx)*16, z-int(cz)*16

	with := func(edits map[[3]int]uint32) *Generator {
		g := NewGenerator(1)
		g.SetEditLookup(func(x, y, z int) (uint32, bool) {
			s, ok := edits[[3]int{x, y, z}]
			return s, ok
		})
		return g
	}

	// A floor under the trunk: the whole tree goes — here, and its canopy in
	// the next chunk (where the neighbour only LOSES tree blocks).
	g := floorOnly(x, y, z)
	ch := g.GenerateChunk(cx, cz)
	if IsLog(sectionBlockAt(ch, lx, y, lz)) || IsLog(sectionBlockAt(ch, lx, y+2, lz)) {
		t.Error("a tree rooted on a player's floor was still grown")
	}
	before, after := plain.GenerateChunk(cx+1, cz), g.GenerateChunk(cx+1, cz)
	for sec := range before.Sections {
		for i, was := range before.Sections[sec] {
			if now := after.Sections[sec][i]; now != was && !IsLeaves(was) && !IsLog(was) {
				t.Fatalf("next chunk: a non-tree block changed (%d -> %d)", was, now)
			}
		}
	}

	// One torch-sized block where a leaf would be: the tree stays.
	var leaf [3]int
	found := false
	for ly := y; ly < y+20 && !found; ly++ {
		for dx := -3; dx <= 3 && !found; dx++ {
			for dz := -3; dz <= 3 && !found; dz++ {
				px, pz := lx+dx, lz+dz
				if px >= 0 && px < 16 && pz >= 0 && pz < 16 && IsLeaves(sectionBlockAt(base, px, ly, pz)) {
					leaf, found = [3]int{x + dx, ly, z + dz}, true
				}
			}
		}
	}
	if !found {
		t.Fatal("no leaf near the trunk")
	}
	g = with(map[[3]int]uint32{leaf: StoneBricks})
	if !IsLog(sectionBlockAt(g.GenerateChunk(cx, cz), lx, y, lz)) {
		t.Error("one block in the canopy removed the whole tree")
	}

	// A castle: a stone ceiling across the whole chunk three blocks up,
	// through the trunk. The tree does not grow up through the rooms.
	roof := map[[3]int]uint32{}
	for px := 0; px < 16; px++ {
		for pz := 0; pz < 16; pz++ {
			roof[[3]int{int(cx)*16 + px, y + 3, int(cz)*16 + pz}] = StoneBricks
		}
	}
	g = with(roof)
	if IsLog(sectionBlockAt(g.GenerateChunk(cx, cz), lx, y, lz)) {
		t.Error("a tree grew up through a castle's ceiling")
	}

	// No edits: generation is exactly as before.
	g = with(nil)
	if got := g.GenerateChunk(cx, cz); got.Sections[0][0] != base.Sections[0][0] || !sameChunk(got, base) {
		t.Error("with no edits the chunk must generate unchanged")
	}
}

func sameChunk(a, b *Chunk) bool {
	for s := range a.Sections {
		for i := range a.Sections[s] {
			if a.Sections[s][i] != b.Sections[s][i] {
				return false
			}
		}
	}
	return true
}
