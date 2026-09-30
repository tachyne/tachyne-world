package worldgen

import "testing"

// springPocket is a chunk of air with one pocket of rock at (x, y, z): the
// cell and the one to its east open, everything else around it rock — a
// spring's shape.
func springPocket(g *Generator, cx, cz int32, y int, rock uint32) (*owRegion, int, int, int) {
	ch := NewChunk(g.sections)
	x, z := int(cx)*16+8, int(cz)*16+8
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				setSectionBlock(ch, 8+dx, y+dy, 8+dz, rock, true)
			}
		}
	}
	setSectionBlock(ch, 8, y, 8, Air, true)
	setSectionBlock(ch, 9, y, 8, Air, true)
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	return reg, x, y, z
}

// firstChunk is findBiomeChunks' first hit, or a skip.
func firstChunk(t *testing.T, g *Generator, biome string) (int32, int32) {
	t.Helper()
	cs := findBiomeChunks(g, biome, 1)
	if len(cs) == 0 {
		t.Skipf("no %s chunk for this seed", biome)
	}
	return cs[0][0], cs[0][1]
}

// spring_lava_frozen: lava out of snow and packed ice in the frozen peaks
// (and groves, jagged peaks, snowy slopes), never out of stone, never in
// another biome, and never beside a player's build.
func TestFrozenLavaSpring(t *testing.T) {
	g := NewGenerator(1)
	fcx, fcz := firstChunk(t, g, "minecraft:frozen_peaks")
	pcx, pcz := firstChunk(t, g, "minecraft:plains")
	y := SeaLevel + 20 // above the sea: the surface biome's cell

	for _, rock := range []uint32{SnowBlock, PackedIce, PowderSnow} {
		reg, x, y, z := springPocket(g, fcx, fcz, y, rock)
		g.frozenSpringAt(reg, nil, x, y, z)
		if s := reg.read(x, y, z); s != Lava {
			t.Errorf("frozen peaks, pocket of %d: got %d, want lava", rock, s)
		}
	}
	reg, x, y, z := springPocket(g, fcx, fcz, y, Stone)
	g.frozenSpringAt(reg, nil, x, y, z)
	if s := reg.read(x, y, z); s != Air {
		t.Errorf("frozen spring in stone: got %d, want nothing (stone is not its valid block)", s)
	}
	reg, x, y, z = springPocket(g, pcx, pcz, y, SnowBlock)
	g.frozenSpringAt(reg, nil, x, y, z)
	if s := reg.read(x, y, z); s != Air {
		t.Errorf("frozen spring in the plains: got %d, want nothing", s)
	}
	// The ordinary lava spring still never springs from snow.
	reg, x, y, z = springPocket(g, fcx, fcz, y, SnowBlock)
	g.springAt(reg, x, y, z, Lava, lavaStone, noSprings, nil)
	if s := reg.read(x, y, z); s != Air {
		t.Errorf("spring_lava out of snow: got %d", s)
	}
	// The build guard: a build block three columns off and a few below, or
	// a dug cell beside it, and the spring is left out.
	for name, bg := range map[string]*buildGuard{
		"build below": {g: g, cols: map[[2]int][]int{{x + 3, z}: {y - 5}}, dug: map[[3]int]bool{}},
		"dug beside":  {g: g, cols: map[[2]int][]int{}, dug: map[[3]int]bool{{x, y, z - 1}: true}},
	} {
		reg, x, y, z := springPocket(g, fcx, fcz, y, SnowBlock)
		g.frozenSpringAt(reg, bg, x, y, z)
		if s := reg.read(x, y, z); s != Air {
			t.Errorf("%s: frozen spring placed beside a build", name)
		}
	}
	far := &buildGuard{g: g, cols: map[[2]int][]int{{x + 9, z}: {y}}, dug: map[[3]int]bool{}}
	reg, x, y, z = springPocket(g, fcx, fcz, y, SnowBlock)
	g.frozenSpringAt(reg, far, x, y, z)
	if s := reg.read(x, y, z); s != Lava {
		t.Error("a build nine columns away should not stop the spring")
	}
}

// Through the chunk's draws: snow riddled with shafts above the sea in the
// frozen peaks springs lava; the same snow in the plains springs none.
func TestFrozenLavaSpringsDraw(t *testing.T) {
	g := NewGenerator(1)
	count := func(biome string) (lava, chunks int) {
		for _, c := range findBiomeChunks(g, biome, 64) {
			ch := NewChunk(g.sections)
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					s := SnowBlock
					if lx%3 == 0 && lz%3 == 0 {
						s = Air
					}
					for y := SeaLevel; y < MinY+g.sections*16; y++ {
						setSectionBlock(ch, lx, y, lz, s, true)
					}
				}
			}
			reg := &owRegion{g: g, ch: ch, baseX: int(c[0]) * 16, baseZ: int(c[1]) * 16, cols: map[[2]int]column{}}
			ox, oz := reg.baseX, reg.baseZ
			g.overworldSprings(newTreeRNG(g.seed^0xCA7E, ox, oz), reg, nil, ox, oz)
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := SeaLevel; y < MinY+g.sections*16; y++ {
						if sectionBlockAt(ch, lx, y, lz) == Lava {
							lava++
						}
					}
				}
			}
			chunks++
		}
		return lava, chunks
	}
	fl, fn := count("minecraft:frozen_peaks")
	pl, pn := count("minecraft:plains")
	t.Logf("lava springs in snow: frozen peaks %d over %d chunks, plains %d over %d", fl, fn, pl, pn)
	if fn == 0 || pn == 0 {
		t.Skip("biomes not found for this seed")
	}
	if fl == 0 {
		t.Error("no frozen lava springs in the frozen peaks' snow")
	}
	if pl != 0 {
		t.Errorf("%d lava springs in the plains' snow", pl)
	}
}

// The springs' biome filter is asked at the spring's own cell: a spring in
// the deep dark (a cave biome under any surface) is left out, one in the
// dripstone caves is not.
func TestSpringsSkipTheDeepDark(t *testing.T) {
	g := NewGenerator(1)
	find := func(biome string) (int32, int32, int, bool) {
		for cx := int32(-96); cx <= 96; cx += 3 {
			for cz := int32(-96); cz <= 96; cz += 3 {
				for y := -60; y <= -34; y += 2 {
					if g.caveBiomeAt(int(cx)*16+8, y, int(cz)*16+8) == biome {
						return cx, cz, y, true
					}
				}
			}
		}
		return 0, 0, 0, false
	}
	dcx, dcz, dy, ok := find("minecraft:deep_dark")
	if !ok {
		t.Skip("no deep dark within range for this seed")
	}
	reg, x, y, z := springPocket(g, dcx, dcz, dy, Deepslate)
	g.springAt(reg, x, y, z, Water, waterStone, noSprings, nil)
	if s := reg.read(x, y, z); s != Air {
		t.Errorf("water spring in the deep dark at %d,%d,%d", x, y, z)
	}
	ccx, ccz, cy, ok := find("minecraft:dripstone_caves")
	if !ok {
		t.Skip("no dripstone caves within range for this seed")
	}
	reg, x, y, z = springPocket(g, ccx, ccz, cy, Deepslate)
	g.springAt(reg, x, y, z, Water, waterStone, noSprings, nil)
	if s := reg.read(x, y, z); s != Water {
		t.Errorf("no water spring in the dripstone caves at %d,%d,%d", x, y, z)
	}
}
