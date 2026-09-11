package worldgen

import "testing"

// oceanChunks finds chunks whose centre column sits in one of the named
// biomes, scanning outward from the origin.
func oceanChunks(g *Generator, want map[string]bool, max int) [][2]int32 {
	var out [][2]int32
	for r := int32(0); r <= 60 && len(out) < max; r += 2 {
		for cx := -r; cx <= r && len(out) < max; cx += 2 {
			for cz := -r; cz <= r && len(out) < max; cz += 2 {
				if abs32(cx) != r && abs32(cz) != r {
					continue
				}
				if want[g.BiomeName(int(cx)*16+8, int(cz)*16+8)] {
					out = append(out, [2]int32{cx, cz})
				}
			}
		}
	}
	return out
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestSeafloorVegetation: oceans grow seagrass and kelp forests, the
// plants are well-formed (tall seagrass in pairs, kelp bodies capped by a
// head, nothing breaking the surface), and frozen oceans stay bare.
func TestSeafloorVegetation(t *testing.T) {
	g := NewGenerator(1)
	kelpBiomes := map[string]bool{"minecraft:ocean": true, "minecraft:deep_ocean": true, "minecraft:cold_ocean": true, "minecraft:deep_cold_ocean": true, "minecraft:lukewarm_ocean": true, "minecraft:deep_lukewarm_ocean": true}
	chunks := oceanChunks(g, kelpBiomes, 12)
	if len(chunks) == 0 {
		t.Fatal("no ocean chunk near the origin for seed 1")
	}
	seagrass, kelp := 0, 0
	for _, c := range chunks {
		ch := g.GenerateChunk(c[0], c[1])
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := MinY; y < SeaLevel+2; y++ {
					s := sectionBlockAt(ch, lx, y, lz)
					above := sectionBlockAt(ch, lx, y+1, lz)
					switch {
					case s == Seagrass:
						seagrass++
					case s == TallSeagrassLower:
						seagrass++
						if above != TallSeagrassUpper {
							t.Fatalf("tall seagrass at (%d,%d,%d) has %d above, not its upper half", lx, y, lz, above)
						}
					case s == TallSeagrassUpper:
						if sectionBlockAt(ch, lx, y-1, lz) != TallSeagrassLower {
							t.Fatalf("stray upper seagrass half at (%d,%d,%d)", lx, y, lz)
						}
					case s == KelpPlant:
						kelp++
						if above != KelpPlant && !IsKelpHead(above) {
							t.Fatalf("kelp body at (%d,%d,%d) has %d above, not kelp", lx, y, lz, above)
						}
					case IsKelpHead(s):
						if above != Water {
							t.Fatalf("kelp head at (%d,%d,%d) has %d above, not water", lx, y, lz, above)
						}
						if y >= SeaLevel-1 {
							t.Fatalf("kelp head at y=%d breaks the surface", y)
						}
					}
					if (s == Seagrass || s == TallSeagrassLower || s == KelpPlant || IsKelpHead(s)) && !IsSturdyTop(sectionBlockAt(ch, lx, y-1, lz)) && sectionBlockAt(ch, lx, y-1, lz) != KelpPlant {
						t.Fatalf("water plant at (%d,%d,%d) rooted on %d", lx, y, lz, sectionBlockAt(ch, lx, y-1, lz))
					}
				}
			}
		}
	}
	if seagrass == 0 {
		t.Fatal("ocean chunks grew no seagrass")
	}
	if kelp == 0 {
		t.Fatal("no kelp in a dozen ocean chunks")
	}
	t.Logf("%d chunks: %d seagrass, %d kelp segments", len(chunks), seagrass, kelp)
	frozen := oceanChunks(g, map[string]bool{"minecraft:frozen_ocean": true, "minecraft:deep_frozen_ocean": true}, 2)
	for _, c := range frozen {
		ch := g.GenerateChunk(c[0], c[1])
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				// The biome filter runs per column: a warmer neighbour biome
				// inside the chunk may legitimately grow something.
				if b := g.BiomeName(int(c[0])*16+lx, int(c[1])*16+lz); b != "minecraft:frozen_ocean" && b != "minecraft:deep_frozen_ocean" {
					continue
				}
				for y := MinY; y < SeaLevel; y++ {
					if s := sectionBlockAt(ch, lx, y, lz); s == Seagrass || s == TallSeagrassLower || s == KelpPlant || IsKelpHead(s) {
						t.Fatalf("frozen ocean chunk %v grew a plant at (%d,%d,%d)", c, lx, y, lz)
					}
				}
			}
		}
	}
	warm := oceanChunks(g, map[string]bool{"minecraft:warm_ocean": true}, 48)
	pickles, coralBlocks, coralTops := 0, 0, 0
	for _, c := range warm {
		ch := g.GenerateChunk(c[0], c[1])
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := MinY; y < SeaLevel; y++ {
					s := sectionBlockAt(ch, lx, y, lz)
					switch {
					case s >= CoralBlock0 && s < CoralBlock0+5:
						coralBlocks++
					case s >= CoralPlant0 && s < CoralFan0+10:
						coralTops++
						if (s-CoralPlant0)%2 != 0 {
							t.Fatalf("a generated coral plant must be waterlogged: state %d", s)
						}
						if under := sectionBlockAt(ch, lx, y-1, lz); !(under >= CoralBlock0 && under < CoralBlock0+5) && !IsSturdyTop(under) {
							t.Fatalf("coral top at (%d,%d,%d) sits on %d", lx, y, lz, under)
						}
					case s >= CoralWallFan0 && s < CoralWallFan0+40:
						if (s-CoralWallFan0)%2 != 0 {
							t.Fatalf("a generated wall fan must be waterlogged: state %d", s)
						}
					}
					if s >= SeaPickle && s < SeaPickle+8 {
						pickles++
						if (s-SeaPickle)%2 != 0 {
							t.Fatalf("a generated sea pickle must be waterlogged: state %d", s)
						}
					}
					if s == KelpPlant || IsKelpHead(s) {
						t.Fatal("warm oceans grow no kelp")
					}
				}
			}
		}
	}
	if len(warm) >= 32 && pickles == 0 {
		t.Fatalf("no sea pickles in %d warm ocean chunks", len(warm))
	}
	if len(warm) >= 32 && coralBlocks == 0 {
		t.Fatalf("no coral in %d warm ocean chunks", len(warm))
	}
	t.Logf("%d warm chunks, %d pickles, %d coral blocks, %d coral tops", len(warm), pickles, coralBlocks, coralTops)
}
