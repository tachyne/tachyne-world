package worldgen

import "testing"

// findIgloos lists up to n igloos over the cells about the origin.
func findIgloos(g *Generator, n int) []Igloo {
	var out []Igloo
	for i := -60; i < 60 && len(out) < n; i++ {
		for j := -60; j < 60 && len(out) < n; j++ {
			if ig := g.IglooIn(i*iglooCell+160, j*iglooCell+160); ig.Exists {
				out = append(out, ig)
			}
		}
	}
	return out
}

// genAt is the generated block at a world cell.
func genAt(g *Generator, cache map[[2]int32]*Chunk, x, y, z int) uint32 {
	k := [2]int32{int32(x >> 4), int32(z >> 4)}
	ch := cache[k]
	if ch == nil {
		ch = g.GenerateChunk(k[0], k[1])
		cache[k] = ch
	}
	return sectionBlockAt(ch, x&15, y, z&15)
}

// Igloos face all four ways, as vanilla's do, and whichever way one faces
// its trapdoor, its ladder shaft and its basement ladder share the top
// piece's pivot column, and its chest is where the loot routing looks.
func TestIglooRotations(t *testing.T) {
	g := NewGenerator(1)
	igloos := findIgloos(g, 400)
	if len(igloos) == 0 {
		t.Skip("no igloos in the search window")
	}
	byRot := [4]int{}
	checked := [4][2]bool{} // per rotation: with and without basement
	chestLo, chestHi := BlockRange("chest")
	trapLo, trapHi := BlockRange("oak_trapdoor")
	for _, ig := range igloos {
		if ig.Legacy {
			t.Fatalf("igloo at %d,%d kept the old layout with no edits", ig.X, ig.Z)
		}
		byRot[ig.Rot]++
		b := 0
		if ig.Basement {
			b = 1
		}
		if checked[ig.Rot][b] {
			continue
		}
		checked[ig.Rot][b] = true
		cache := map[[2]int32]*Chunk{}
		px, pz := ig.X+iglooTopPivot[0], ig.Z+iglooTopPivot[1]
		hatch := genAt(g, cache, px, ig.Y, pz)
		if !ig.Basement {
			overCave := genAt(g, cache, px, ig.Y-1, pz) == Air && hatch >= trapLo && hatch <= trapHi
			if hatch != SnowBlock && !overCave {
				t.Errorf("rot %d, no basement: the hatch cell holds %d, want snow", ig.Rot, hatch)
			}
			continue
		}
		if hatch < trapLo || hatch > trapHi {
			t.Errorf("rot %d: no trapdoor on the pivot column (got %d)", ig.Rot, hatch)
		}
		for y := ig.Y - 3; y > ig.Y-3*ig.Depth; y -= 3 {
			if s := genAt(g, cache, px, y, pz); !isLadder(s) {
				t.Errorf("rot %d: no ladder at %d,%d,%d (got %d)", ig.Rot, px, y, pz, s)
				break
			}
		}
		if s := genAt(g, cache, ig.ChestX, ig.ChestY, ig.ChestZ); s < chestLo || s > chestHi {
			t.Errorf("rot %d: no chest at the routed position %d,%d,%d (got %d)", ig.Rot, ig.ChestX, ig.ChestY, ig.ChestZ, s)
		}
	}
	t.Logf("igloos by rotation: %v", byRot)
	for rot, n := range byRot {
		if n == 0 {
			t.Errorf("no igloo faces rotation %d in %d", rot, len(igloos))
		}
	}
}

// An igloo a player has touched — here one block dug out of its dome, in
// the old layout — keeps that layout, rotation 0 on its old ground.
func TestIglooKeepsLayoutWhenTouched(t *testing.T) {
	g := NewGenerator(1)
	var ig Igloo
	for _, c := range findIgloos(g, 400) {
		if c.Rot != 0 {
			ig = c
			break
		}
	}
	if !ig.Exists {
		t.Skip("no rotated igloo in the search window")
	}
	old := g.IglooIn(ig.X, ig.Z)
	dug := [3]int{ig.X + 3, ig.Y + 2, ig.Z + 1} // inside the rotation-0 dome
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		if int32(floorDiv16(dug[0])) == ecx && int32(floorDiv16(dug[2])) == ecz {
			fn(dug[0], dug[1], dug[2], Air)
		}
	})
	got := g.IglooIn(ig.X, ig.Z)
	if !got.Legacy || got.Rot != 0 || got.Y != g.Height(ig.X, ig.Z)-1 {
		t.Errorf("touched igloo: rot %d legacy %v y %d, want the old layout (was rot %d)", got.Rot, got.Legacy, got.Y, old.Rot)
	}
	if got.Basement && (got.ChestX != ig.X+1 || got.ChestZ != ig.Z-2+6) {
		t.Errorf("touched igloo's chest at %d,%d, want the old %d,%d", got.ChestX, got.ChestZ, ig.X+1, ig.Z+4)
	}
	// A build far from both layouts does not hold it back.
	far := [3]int{ig.X + 40, ig.Y, ig.Z + 40}
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		if int32(floorDiv16(far[0])) == ecx && int32(floorDiv16(far[2])) == ecz {
			fn(far[0], far[1], far[2], BlockBase("oak_planks"))
		}
	})
	if got := g.IglooIn(ig.X, ig.Z); got.Legacy || got.Rot != ig.Rot {
		t.Error("a build forty blocks off kept the igloo from turning")
	}
}

// rotAboutPivot is vanilla's pivot rotation: the pivot stays put and a
// point one east of it goes south under a clockwise quarter turn.
func TestRotAboutPivot(t *testing.T) {
	p := [2]int{3, 5}
	for rot := 0; rot < 4; rot++ {
		if x, z := rotAboutPivot(3, 5, rot, p); x != 3 || z != 5 {
			t.Errorf("rot %d moved the pivot to %d,%d", rot, x, z)
		}
	}
	for rot, want := range [4][2]int{{4, 5}, {3, 6}, {2, 5}, {3, 4}} {
		if x, z := rotAboutPivot(4, 5, rot, p); x != want[0] || z != want[1] {
			t.Errorf("rot %d: east of the pivot went to %d,%d, want %v", rot, x, z, want)
		}
	}
}
