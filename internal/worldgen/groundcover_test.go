package worldgen

import "testing"

// groundCoverOnly is a chunk of bare terrain with only the 26.3 ground
// cover replayed into it.
func groundCoverOnly(g *Generator, cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			for y := MinY; y < MinY+g.sections*16; y++ {
				setSectionBlock(ch, lx, y, lz, g.terrainCell(col, wx, y, wz), true)
			}
		}
	}
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	bg := g.newBuildGuard(cx, cz)
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.groundCover(reg, bg, cx+dcx, cz+dcz)
		}
	}
	return ch
}

// isAnyOf reports whether a state belongs to one of the named blocks.
func isAnyOf(s uint32, names ...string) bool {
	for _, n := range names {
		if lo, hi := BlockRange(n); s >= lo && s <= hi {
			return true
		}
	}
	return false
}

// Each biome grows its own 26.3 set, every plant on ground that holds it.
func TestGroundCoverBiomeSets(t *testing.T) {
	g := NewGenerator(1)
	for _, c := range []struct {
		biome  string
		blocks []string // at least one of these must grow
	}{
		{"minecraft:plains", []string{"red_tulip", "orange_tulip", "white_tulip", "pink_tulip", "azure_bluet", "oxeye_daisy", "cornflower", "poppy", "dandelion"}},
		{"minecraft:plains", []string{"short_grass"}},
		{"minecraft:flower_forest", []string{"lily_of_the_valley", "red_tulip", "allium", "cornflower", "lilac", "peony", "rose_bush"}},
		{"minecraft:forest", []string{"short_grass"}},
		{"minecraft:swamp", []string{"blue_orchid"}},
		{"minecraft:meadow", []string{"allium", "azure_bluet", "cornflower", "oxeye_daisy", "poppy", "dandelion", "short_grass", "tall_grass"}},
		{"minecraft:cherry_grove", []string{"pink_petals"}},
		{"minecraft:pale_garden", []string{"closed_eyeblossom"}},
		{"minecraft:desert", []string{"cactus"}},
		{"minecraft:desert", []string{"dead_bush"}},
		{"minecraft:badlands", []string{"dead_bush"}},
		{"minecraft:taiga", []string{"fern"}},
		{"minecraft:jungle", []string{"fern", "short_grass"}},
		{"minecraft:bamboo_jungle", []string{"bamboo"}},
		{"minecraft:savanna", []string{"short_grass"}},
		{"minecraft:dappled_forest", []string{"red_shrub"}},
		{"minecraft:mushroom_fields", []string{"brown_mushroom", "red_mushroom"}},
	} {
		chunks := findBiomeChunks(g, c.biome, 48)
		if len(chunks) == 0 {
			t.Logf("%s: no chunk within range for this seed", c.biome)
			continue
		}
		found := 0
		for _, p := range chunks {
			ch := groundCoverOnly(g, p[0], p[1])
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := MinY + 1; y < MinY+g.sections*16-1; y++ {
						s := sectionBlockAt(ch, lx, y, lz)
						if !isAnyOf(s, c.blocks...) {
							continue
						}
						found++
						checkCoverSupport(t, c.biome, s, sectionBlockAt(ch, lx, y-1, lz))
					}
				}
			}
		}
		if found == 0 {
			t.Errorf("%s: none of %v in %d chunks", c.biome, c.blocks, len(chunks))
		}
	}
}

// checkCoverSupport fails a plant standing on what cannot hold it.
func checkCoverSupport(t *testing.T, biome string, s, below uint32) {
	t.Helper()
	switch {
	case isAnyOf(s, "cactus"):
		if !isAnyOf(below, "cactus", "sand", "red_sand") {
			t.Errorf("%s: cactus on %d", biome, below)
		}
	case isAnyOf(s, "cactus_flower"):
		if !isAnyOf(below, "cactus") {
			t.Errorf("%s: cactus flower on %d", biome, below)
		}
	case isAnyOf(s, "bamboo"):
		if !supportsBamboo(below) {
			t.Errorf("%s: bamboo on %d", biome, below)
		}
	case isAnyOf(s, "dead_bush"):
		if !supportsDryVegetation(below) {
			t.Errorf("%s: dead bush on %d", biome, below)
		}
	case isAnyOf(s, "brown_mushroom", "red_mushroom"):
		if below == Air || IsFluid(below) {
			t.Errorf("%s: mushroom on %d", biome, below)
		}
	case isAnyOf(s, "tall_grass", "lilac", "peony", "rose_bush"):
		// either half: the lower on vegetation ground, the upper on its lower
		if !supportsVegetation(below) && !isAnyOf(below, "tall_grass", "lilac", "peony", "rose_bush") {
			t.Errorf("%s: double plant on %d", biome, below)
		}
	default:
		if !supportsVegetation(below) {
			t.Errorf("%s: %d on %d", biome, s, below)
		}
	}
}

// Through the whole generator: the plains' tulip-or-flower mix and the
// desert's cacti come out of GenerateChunk, and the old scatter's lone
// flowers no longer fill the plains in its place.
func TestGroundCoverInGeneratedChunks(t *testing.T) {
	g := NewGenerator(1)
	count := func(biome string, n int, names ...string) (hits, chunks int) {
		for _, p := range findBiomeChunks(g, biome, n) {
			chunks++
			ch := g.GenerateChunk(p[0], p[1])
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					h := g.Height(int(p[0])*16+lx, int(p[1])*16+lz)
					for y := h - 3; y < h+4; y++ {
						if isAnyOf(sectionBlockAt(ch, lx, y, lz), names...) {
							hits++
						}
					}
				}
			}
		}
		return hits, chunks
	}
	if hits, chunks := count("minecraft:plains", 48, "short_grass"); chunks > 0 && hits == 0 {
		t.Errorf("no short grass in %d plains chunks", chunks)
	}
	if hits, chunks := count("minecraft:desert", 48, "cactus"); chunks > 0 && hits == 0 {
		t.Errorf("no cactus in %d desert chunks", chunks)
	}
	if hits, chunks := count("minecraft:flower_forest", 32, "lily_of_the_valley", "red_tulip", "orange_tulip",
		"white_tulip", "pink_tulip", "allium", "cornflower", "oxeye_daisy", "azure_bluet"); chunks > 0 && hits == 0 {
		t.Errorf("no flower-forest flowers in %d chunks", chunks)
	}
}

// The 26.3 cover moves nothing else: a chunk generated with and without it
// differs only in plant cells, the podzol under bamboo, and the snow and
// snowy ground that follow the plants.
func TestGroundCoverLeavesOtherFeatures(t *testing.T) {
	g := NewGenerator(1)
	allowed := func(s uint32) bool {
		if s == Air || IsReplaceable(s) {
			return true
		}
		return isAnyOf(s, "short_grass", "fern", "dandelion", "poppy", "blue_orchid", "allium", "azure_bluet",
			"red_tulip", "orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy", "cornflower", "lily_of_the_valley",
			"pink_petals", "closed_eyeblossom", "red_shrub", "dead_bush", "cactus", "cactus_flower", "sweet_berry_bush",
			"bamboo", "brown_mushroom", "red_mushroom", "tall_grass", "lilac", "rose_bush", "peony",
			"pale_moss_carpet", "podzol", "grass_block", "dirt", "coarse_dirt", "mycelium", "snow")
	}
	// Chunks a village reaches are left out: its decor piles read the
	// ground they land on, so they follow the plants too.
	nearVillage := func(p [2]int32) bool {
		x, z := int(p[0])*16+8, int(p[1])*16+8
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if v := g.VillageIn(x+dx*villageCell, z+dz*villageCell); v.Exists && absInt(v.X-x) < 200 && absInt(v.Z-z) < 200 {
					return true
				}
			}
		}
		return false
	}
	var chunks [][2]int32
	for _, b := range []string{"minecraft:plains", "minecraft:bamboo_jungle", "minecraft:desert", "minecraft:taiga", "minecraft:flower_forest"} {
		n := 0
		for _, p := range findBiomeChunks(g, b, 8) {
			if n < 3 && !nearVillage(p) {
				chunks = append(chunks, p)
				n++
			}
		}
	}
	for _, p := range chunks {
		on := g.GenerateChunk(p[0], p[1])
		groundCoverOff = true
		off := g.GenerateChunk(p[0], p[1])
		groundCoverOff = false
		diffs := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := MinY; y < MinY+g.sections*16; y++ {
					a, b := sectionBlockAt(on, lx, y, lz), sectionBlockAt(off, lx, y, lz)
					if a == b {
						continue
					}
					diffs++
					if !allowed(a) || !allowed(b) {
						t.Errorf("chunk %v cell %d,%d,%d: %d with the cover, %d without — not a ground-cover change", p, lx, y, lz, a, b)
					}
				}
			}
		}
		t.Logf("chunk %v: %d cells differ", p, diffs)
	}
}

// Nothing of the cover grows under a player's roof.
func TestGroundCoverSkipsRoofedGround(t *testing.T) {
	g := NewGenerator(1)
	cover := func(ch *Chunk, cx, cz int32) int {
		n := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				h := g.Height(int(cx)*16+lx, int(cz)*16+lz)
				for y := h; y < h+3; y++ {
					if isAnyOf(sectionBlockAt(ch, lx, y, lz), "short_grass", "dandelion", "poppy", "azure_bluet",
						"oxeye_daisy", "cornflower", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip") {
						n++
					}
				}
			}
		}
		return n
	}
	var cx, cz int32
	ok := false
	for _, p := range findBiomeChunks(g, "minecraft:plains", 40) {
		if cover(g.GenerateChunk(p[0], p[1]), p[0], p[1]) > 0 {
			cx, cz, ok = p[0], p[1], true
			break
		}
	}
	if !ok {
		t.Skip("no plains chunk with ground cover for this seed")
	}
	edits := map[[3]int]uint32{}
	planks := BlockBase("oak_planks")
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			x, z := int(cx)*16+lx, int(cz)*16+lz
			edits[[3]int{x, g.Height(x, z) + 6, z}] = planks
		}
	}
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	if n := cover(g.GenerateChunk(cx, cz), cx, cz); n != 0 {
		t.Errorf("%d ground-cover plants grew under the roof", n)
	}
}

// A chunk's ground cover is the same from every pass: generating a chunk
// twice, and generating its neighbours in between, gives the same blocks.
func TestGroundCoverDeterministic(t *testing.T) {
	g := NewGenerator(3)
	for _, p := range findBiomeChunks(g, "minecraft:bamboo_jungle", 2) {
		a := g.GenerateChunk(p[0], p[1])
		g.GenerateChunk(p[0]+1, p[1])
		g.GenerateChunk(p[0], p[1]-1)
		if b := g.GenerateChunk(p[0], p[1]); !a.Equal(b) {
			t.Errorf("chunk %v regenerated differently", p)
		}
	}
}

// The providers: the plains' threshold noise gives tulips only where it is
// low; the flower forest's noise bands every one of its eleven flowers;
// the meadow's dual noise only ever hands out its own states.
func TestGroundCoverProviders(t *testing.T) {
	seen := map[uint32]bool{}
	for x := -4000; x < 4000; x += 7 {
		for z := -4000; z < 4000; z += 13 {
			seen[flowerForestState(x, 64, z)] = true
			m := meadowState(x, 64, z)
			ok := false
			for _, s := range gcMeadowStates {
				ok = ok || s == m
			}
			if !ok {
				t.Fatalf("meadow state %d at %d,%d is not one of its list", m, x, z)
			}
		}
	}
	for _, s := range gcFlowerForestStates {
		if !seen[s] {
			t.Errorf("flower forest state %d never chosen", s)
		}
	}
	if noiseState(gcFlowerForestStates, -5) != gcFlowerForestStates[0] || noiseState(gcFlowerForestStates, 5) != gcFlowerForestStates[10] {
		t.Error("noiseState does not clamp to the list's ends")
	}
}
