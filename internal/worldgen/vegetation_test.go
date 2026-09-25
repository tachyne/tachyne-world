package worldgen

import "testing"

// findBiomeChunks lists up to n chunks whose centre is in the biome, by
// widening rings about the origin.
func findBiomeChunks(g *Generator, biome string, n int) [][2]int32 {
	var out [][2]int32
	for r := 0; r < 256 && len(out) < n; r++ {
		for cx := -r; cx <= r && len(out) < n; cx++ {
			for cz := -r; cz <= r && len(out) < n; cz++ {
				if (cx == -r || cx == r || cz == -r || cz == r) && g.resolveBiome(cx*16+8, cz*16+8).Name == biome {
					out = append(out, [2]int32{int32(cx), int32(cz)})
				}
			}
		}
	}
	return out
}

// vegetationOnly is a chunk of bare terrain with only the vegetation
// patches replayed into it — what they place, apart from the trees and
// ground cover that also grow some of the same plants.
func vegetationOnly(g *Generator, cx, cz int32) *Chunk {
	return vegetationOnlyPrep(g, cx, cz, nil)
}

// vegetationOnlyPrep is vegetationOnly with the bare terrain handed to prep
// before the patches run.
func vegetationOnlyPrep(g *Generator, cx, cz int32, prep func(ch *Chunk)) *Chunk {
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
	if prep != nil {
		prep(ch)
	}
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	bg := g.newBuildGuard(cx, cz)
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.overworldVegetation(reg, bg, cx+dcx, cz+dcz)
		}
	}
	return ch
}

// Each patch grows in its biomes, standing on what holds it: a double
// plant with its upper half over it, a lily pad on water, dry grass on
// sand or terracotta, a vine against a face.
func TestVegetationPatchesGrow(t *testing.T) {
	g := NewGenerator(1)
	for _, c := range []struct {
		biome, block string
	}{
		{"minecraft:savanna", "tall_grass"},
		{"minecraft:plains", "tall_grass"},
		{"minecraft:taiga", "large_fern"},
		{"minecraft:sunflower_plains", "sunflower"},
		{"minecraft:dark_forest", "leaf_litter"},
		{"minecraft:forest", "bush"},
		{"minecraft:swamp", "firefly_bush"},
		{"minecraft:desert", "short_dry_grass"},
		{"minecraft:badlands", "tall_dry_grass"},
		{"minecraft:birch_forest", "wildflowers"},
		{"minecraft:meadow", "wildflowers"},
		{"minecraft:pale_garden", "pale_moss_block"},
		{"minecraft:jungle", "vine"},
	} {
		chunks := findBiomeChunks(g, c.biome, 64)
		if len(chunks) == 0 {
			t.Logf("%s: no chunk within range for this seed", c.biome)
			continue
		}
		lo, hi := BlockRange(c.block)
		found := 0
		for _, p := range chunks {
			ch := vegetationOnly(g, p[0], p[1])
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := MinY + 1; y < MinY+g.sections*16-1; y++ {
						s := sectionBlockAt(ch, lx, y, lz)
						if s < lo || s > hi {
							continue
						}
						found++
						checkVegetationSupport(t, c.block, s, sectionBlockAt(ch, lx, y-1, lz), sectionBlockAt(ch, lx, y+1, lz))
					}
				}
			}
		}
		if found == 0 {
			t.Errorf("%s: no %s in %d chunks", c.biome, c.block, len(chunks))
		}
	}
}

// tachyne's swamps stand a few blocks above the sea, so their lily pads
// need water the terrain seldom gives them: flood the swamp's ground and
// the pads float on it.
func TestWaterlilyFloatsOnSwampWater(t *testing.T) {
	g := NewGenerator(1)
	lilies := 0
	for _, p := range findBiomeChunks(g, "minecraft:swamp", 16) {
		ch := vegetationOnlyPrep(g, p[0], p[1], func(ch *Chunk) {
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					setSectionBlock(ch, lx, g.Height(int(p[0])*16+lx, int(p[1])*16+lz)-1, lz, Water, true)
				}
			}
		})
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := MinY + 1; y < MinY+g.sections*16; y++ {
					if sectionBlockAt(ch, lx, y, lz) == LilyPad {
						lilies++
						if b := sectionBlockAt(ch, lx, y-1, lz); b != Water {
							t.Errorf("lily pad over %d", b)
						}
					}
				}
			}
		}
	}
	if lilies == 0 {
		t.Error("no lily pads on flooded swamp ground")
	}
}

func checkVegetationSupport(t *testing.T, block string, s, below, above uint32) {
	t.Helper()
	switch block {
	case "tall_grass", "large_fern", "sunflower":
		halves := map[string][2]uint32{"tall_grass": tallGrassHalves, "large_fern": largeFernHalves, "sunflower": sunflowerHalves}[block]
		if s == halves[0] && (!supportsVegetation(below) || above != halves[1]) {
			t.Errorf("%s lower half on %d under %d", block, below, above)
		}
		if s == halves[1] && below != halves[0] {
			t.Errorf("%s upper half over %d", block, below)
		}
	case "short_dry_grass", "tall_dry_grass":
		if !supportsDryVegetation(below) {
			t.Errorf("%s on %d", block, below)
		}
	case "leaf_litter":
		if below != GrassBlock {
			t.Errorf("leaf litter on %d", below)
		}
	case "bush", "firefly_bush", "wildflowers":
		if !supportsVegetation(below) {
			t.Errorf("%s on %d", block, below)
		}
	}
}

// Vanilla's flower noise takes the lower count in about one area in fifty;
// the stand-in threshold must keep that share within reason.
func TestFlowerNoiseShare(t *testing.T) {
	g := NewGenerator(1)
	below, n := 0, 0
	for x := -200; x < 200; x++ {
		for z := -200; z < 200; z += 4 {
			n++
			if g.flowerNoiseBelow(x*16, z*16) {
				below++
			}
		}
	}
	if share := float64(below) / float64(n); share < 0.005 || share > 0.06 {
		t.Errorf("flower noise below the threshold in %.3f of chunks, want about 0.02", share)
	}
}

// The build guard: a chunk whose tall grass is replaced by a player's roof
// over every column grows none of it under the roof, through the whole
// generation path — and the same chunk without the roof does.
func TestVegetationSkipsRoofedGround(t *testing.T) {
	g := NewGenerator(1)
	tall := func(ch *Chunk, cx, cz int32) int {
		n := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				h := g.Height(int(cx)*16+lx, int(cz)*16+lz)
				for y := h; y < h+4; y++ {
					if s := sectionBlockAt(ch, lx, y, lz); s == tallGrassHalves[0] || s == tallGrassHalves[1] {
						n++
					}
				}
			}
		}
		return n
	}
	var cx, cz int32
	ok := false
	for _, p := range findBiomeChunks(g, "minecraft:savanna", 40) {
		if tall(g.GenerateChunk(p[0], p[1]), p[0], p[1]) > 0 {
			cx, cz, ok = p[0], p[1], true
			break
		}
	}
	if !ok {
		t.Skip("no savanna chunk with tall grass for this seed")
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
	if n := tall(g.GenerateChunk(cx, cz), cx, cz); n != 0 {
		t.Errorf("%d tall grass cells grew under the roof", n)
	}
}
