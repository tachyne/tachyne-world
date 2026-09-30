package worldgen

import "testing"

// wetChunk is a chunk of bare terrain with water let into the ground every
// fourth diagonal, so a cane patch finds shore wherever it lands.
func wetChunk(g *Generator, cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			for y := MinY; y < MinY+g.sections*16; y++ {
				setSectionBlock(ch, lx, y, lz, g.terrainCell(col, wx, y, wz), true)
			}
			if (wx+wz)&3 == 0 && col.h > MinY {
				setSectionBlock(ch, lx, col.h-1, lz, Water, true)
			}
		}
	}
	return ch
}

// surrounded reports whether the chunk and its eight neighbours sample as
// the biome all over, so every patch origin a chunk pass replays is in it.
func surrounded(g *Generator, cx, cz int32, biome string) bool {
	for x := int(cx)*16 - 16; x < int(cx)*16+32; x += 4 {
		for z := int(cz)*16 - 16; z < int(cz)*16+32; z += 4 {
			if g.columnAt(x, z).biome.Name != biome {
				return false
			}
		}
	}
	return true
}

// surroundedChunks is up to n chunks deep inside the biome.
func surroundedChunks(g *Generator, biome string, n int) [][2]int32 {
	var out [][2]int32
	for _, c := range findBiomeChunks(g, biome, 8*n) {
		if len(out) < n && surrounded(g, c[0], c[1], biome) {
			out = append(out, c)
		}
	}
	return out
}

// runBiomePatches replays the single-biome patches of a chunk's 3×3 origins.
func runBiomePatches(g *Generator, ch *Chunk, cx, cz int32, bg *buildGuard) {
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.biomePatches(reg, bg, cx+dcx, cz+dcz)
		}
	}
}

func chunkStateCount(ch *Chunk, st uint32) int {
	n := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == st {
				n++
			}
		}
	}
	return n
}

// The desert, badlands and swamp each grow their own cane patch; a biome
// with none of them (the plains, whose cane is the plain patch's) gets
// nothing from these.
func TestBiomeCanePatches(t *testing.T) {
	g := NewGenerator(1)
	for _, c := range []struct {
		biome string
		want  bool
	}{
		{"minecraft:desert", true},
		{"minecraft:badlands", true},
		{"minecraft:swamp", true},
		{"minecraft:plains", false},
		{"minecraft:jungle", false},
	} {
		chunks := surroundedChunks(g, c.biome, 12)
		if len(chunks) == 0 {
			t.Logf("%s: no chunk deep inside it for this seed", c.biome)
			continue
		}
		cane := 0
		for _, p := range chunks {
			ch := wetChunk(g, p[0], p[1])
			runBiomePatches(g, ch, p[0], p[1], nil)
			cane += chunkStateCount(ch, sugarCane)
			// Every cane stands on cane or on its ground, with water beside
			// the bottom one's ground.
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := MinY + 1; y < MinY+g.sections*16; y++ {
						if sectionBlockAt(ch, lx, y, lz) != sugarCane {
							continue
						}
						if b := sectionBlockAt(ch, lx, y-1, lz); b != sugarCane && !caneGround(b) {
							t.Errorf("%s: cane on %d", c.biome, b)
						}
					}
				}
			}
		}
		t.Logf("%s: %d cane over %d chunks", c.biome, cane, len(chunks))
		if c.want && cane == 0 {
			t.Errorf("%s: its cane patch placed nothing", c.biome)
		}
		if !c.want && cane != 0 {
			t.Errorf("%s: %d cane from another biome's patch", c.biome, cane)
		}
	}
}

// The plain cane patch leaves the desert (and the other biomes with their
// own) alone, and pumpkins leave the dappled forest — while the plains keep
// both.
func TestPlainPatchesLeaveTheirBiomes(t *testing.T) {
	g := NewGenerator(1)
	grow := func(biome string, seeds int) (cane, pumpkins, chunks int) {
		for _, p := range surroundedChunks(g, biome, 4) {
			chunks++
			// One chunk takes every run: the counts only need to be zero
			// or not.
			ch := wetChunk(g, p[0], p[1])
			reg := &owRegion{g: g, ch: ch, baseX: int(p[0]) * 16, baseZ: int(p[1]) * 16, cols: map[[2]int]column{}}
			for seed := 0; seed < seeds; seed++ {
				g.overworldPatches(newTreeRNG(int64(seed), reg.baseX, reg.baseZ), reg, reg.baseX, reg.baseZ)
			}
			cane += chunkStateCount(ch, sugarCane)
			pumpkins += chunkStateCount(ch, pumpkin)
		}
		return
	}
	for _, c := range []struct {
		biome          string
		cane, pumpkins bool
		seeds          int
	}{
		{"minecraft:plains", true, true, 400},
		{"minecraft:desert", false, true, 400},
		{"minecraft:dappled_forest", false, false, 3000},
	} {
		cane, pumpkins, chunks := grow(c.biome, c.seeds)
		t.Logf("%s: %d cane, %d pumpkins over %d chunks × %d draws", c.biome, cane, pumpkins, chunks, c.seeds)
		if chunks == 0 {
			continue
		}
		if (cane > 0) != c.cane {
			t.Errorf("%s: plain cane patch placed %d", c.biome, cane)
		}
		if !c.pumpkins && pumpkins > 0 {
			t.Errorf("%s: %d pumpkins", c.biome, pumpkins)
		}
	}
}

// A dry run of the plain cane patch (a biome vanilla leaves out) draws
// exactly what a placing run does, so the features after it keep theirs.
func TestCanePatchDryRunDrawsTheSame(t *testing.T) {
	g := NewGenerator(1)
	chunks := surroundedChunks(g, "minecraft:desert", 2)
	if len(chunks) == 0 {
		t.Skip("no desert for this seed")
	}
	p := chunks[0]
	placed := 0
	for seed := int64(0); seed < 40; seed++ {
		wet, dry := wetChunk(g, p[0], p[1]), wetChunk(g, p[0], p[1])
		rw, rd := newTreeRNG(seed, 0, 0), newTreeRNG(seed, 0, 0)
		bx, bz := int(p[0])*16, int(p[1])*16
		g.canePatch(rw, &owRegion{g: g, ch: wet, baseX: bx, baseZ: bz, cols: map[[2]int]column{}}, bx+8, bz+8, true)
		g.canePatch(rd, &owRegion{g: g, ch: dry, baseX: bx, baseZ: bz, cols: map[[2]int]column{}}, bx+8, bz+8, false)
		placed += chunkStateCount(wet, sugarCane)
		if n := chunkStateCount(dry, sugarCane); n != 0 {
			t.Fatalf("dry run placed %d cane", n)
		}
		if a, b := rw.Intn(1<<30), rd.Intn(1<<30); a != b {
			t.Fatalf("seed %d: the dry run left the stream elsewhere", seed)
		}
	}
	if placed == 0 {
		t.Error("the placing runs placed nothing: the test proves nothing")
	}
}

// patch_melon_sparse: melons in the sparse jungle, one chunk in sixty-four,
// none from it anywhere else.
func TestSparseJungleMelons(t *testing.T) {
	g := NewGenerator(1)
	count := func(biome string) (melons, chunks int) {
		for _, p := range surroundedChunks(g, biome, 40) {
			ch := wetChunk(g, p[0], p[1])
			runBiomePatches(g, ch, p[0], p[1], nil)
			melons += chunkStateCount(ch, melon)
			chunks++
		}
		return
	}
	sm, sn := count("minecraft:sparse_jungle")
	jm, jn := count("minecraft:jungle")
	t.Logf("melons: sparse jungle %d over %d chunks, jungle %d over %d", sm, sn, jm, jn)
	if sn > 0 && sm == 0 {
		t.Error("no sparse-jungle melons")
	}
	if jm != 0 {
		t.Errorf("%d sparse-jungle melons in the jungle", jm)
	}
}

// The build guard: a desert chunk roofed over by a player grows none of
// the new cane under the roof.
func TestBiomePatchesSkipBuilds(t *testing.T) {
	g := NewGenerator(1)
	var cx, cz int32
	ok := false
	for _, p := range surroundedChunks(g, "minecraft:desert", 12) {
		ch := wetChunk(g, p[0], p[1])
		runBiomePatches(g, ch, p[0], p[1], nil)
		if chunkStateCount(ch, sugarCane) > 0 {
			cx, cz, ok = p[0], p[1], true
			break
		}
	}
	if !ok {
		t.Skip("no desert chunk growing cane for this seed")
	}
	edits := map[[3]int]uint32{}
	planks := BlockBase("oak_planks")
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			x, z := int(cx)*16+lx, int(cz)*16+lz
			edits[[3]int{x, max(g.Height(x, z), SeaLevel) + 6, z}] = planks
		}
	}
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	ch := wetChunk(g, cx, cz)
	runBiomePatches(g, ch, cx, cz, g.newBuildGuard(cx, cz))
	if n := chunkStateCount(ch, sugarCane); n != 0 {
		t.Errorf("%d cane grew under the roof", n)
	}
}
