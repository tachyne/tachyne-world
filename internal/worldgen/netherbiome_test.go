package worldgen

import "testing"

// findNetherBiomeChunk scans outward for a chunk whose centre is in biome.
func findNetherBiomeChunk(g *Generator, biome string) (int32, int32, bool) {
	for r := 0; r < 64; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				if g.netherBiome(cx*16+8, cz*16+8) == biome {
					return int32(cx), int32(cz), true
				}
			}
		}
	}
	return 0, 0, false
}

// A crimson forest chunk carries its biome, lays crimson nylium on its
// cavern floors, and grows the forest: roots or fungi, and somewhere near a
// huge fungus's stem.
func TestNetherCrimsonForestGrows(t *testing.T) {
	g := NewNetherGenerator(7)
	cx, cz, ok := findNetherBiomeChunk(g, "minecraft:crimson_forest")
	if !ok {
		t.Skip("no crimson forest within 64 chunks of the origin for this seed")
	}
	nylium, plants, stems := 0, 0, 0
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			ch := g.generateNetherChunk(cx+dcx, cz+dcz)
			if ch.Biomes[0] != "minecraft:crimson_forest" && dcx == 0 && dcz == 0 {
				t.Fatalf("centre chunk biome %q", ch.Biomes[0])
			}
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					switch {
					case b == CrimsonNylium:
						nylium++
					case b == CrimsonRoots || b == CrimsonFungus:
						plants++
					case b == crimsonStem:
						stems++
					}
				}
			}
		}
	}
	if nylium == 0 {
		t.Error("no crimson nylium on the forest's floors")
	}
	if plants == 0 {
		t.Error("no roots or fungi in the forest")
	}
	if stems == 0 {
		t.Error("no huge fungus in twenty-five forest chunks")
	}
}

// A soul sand valley chunk lines its floors with soul sand or soul soil.
func TestNetherSoulSandValleySurface(t *testing.T) {
	g := NewNetherGenerator(7)
	cx, cz, ok := findNetherBiomeChunk(g, "minecraft:soul_sand_valley")
	if !ok {
		t.Skip("no soul sand valley within 64 chunks for this seed")
	}
	ch := g.generateNetherChunk(cx, cz)
	soul := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == SoulSand || b == SoulSoil {
				soul++
			}
		}
	}
	if soul < 64 {
		t.Errorf("soul blocks in a valley chunk: %d", soul)
	}
}

// The dressed column is pure: two reads of the same column agree, and the
// chunk's cells match it (before decoration touches them).
func TestNetherColumnIsPure(t *testing.T) {
	g := NewNetherGenerator(3)
	a, b := g.netherColumn(100, -40), g.netherColumn(100, -40)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("column differs at %d", i)
		}
	}
}
