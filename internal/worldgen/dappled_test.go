package worldgen

import "testing"

// 26.3 gave the coldest-driest middle-biome cell to the dappled forest, and
// the forest grows poplars in its three colours, spruce and fallen poplars,
// on grass with coarse-dirt patches and red shrubs.
func TestDappledForestGenerates(t *testing.T) {
	if landBiome(1, 0, 0) != "minecraft:dappled_forest" || landBiome(1, 1, 0) != "minecraft:plains" {
		t.Fatalf("cold/driest = %s, cold/dry = %s", landBiome(1, 0, 0), landBiome(1, 1, 0))
	}
	g := NewGenerator(1)
	poplarLo, poplarHi, _ := BlockRangeOK("poplar_log")
	leafNames := []string{"red_poplar_leaves", "orange_poplar_leaves", "yellow_poplar_leaves"}
	colours := map[string]bool{}
	var logs, coarse, shrubs, chunks int
	for cz := int32(-60); cz < 60 && chunks < 30; cz += 3 {
		for cx := int32(-60); cx < 60 && chunks < 30; cx += 3 {
			if g.BiomeName(int(cx)*16+8, int(cz)*16+8) != "minecraft:dappled_forest" {
				continue
			}
			chunks++
			ch := g.GenerateChunk(cx, cz)
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					h := g.Height(int(cx)*16+lx, int(cz)*16+lz)
					switch sectionBlockAt(ch, lx, h-1, lz) {
					case CoarseDirt:
						coarse++
					}
					if sectionBlockAt(ch, lx, h, lz) == RedShrub {
						shrubs++
					}
					for y := h; y < h+24; y++ {
						s := sectionBlockAt(ch, lx, y, lz)
						if s >= poplarLo && s <= poplarHi {
							logs++
						}
						for _, n := range leafNames {
							if lo, hi, _ := BlockRangeOK(n); s >= lo && s <= hi {
								colours[n] = true
							}
						}
					}
				}
			}
		}
	}
	if chunks == 0 {
		t.Fatal("no dappled forest within ±960 blocks of spawn")
	}
	t.Logf("%d dappled chunks: %d poplar logs, leaf colours %v, %d coarse dirt, %d red shrubs", chunks, logs, colours, coarse, shrubs)
	if logs == 0 || len(colours) < 3 {
		t.Errorf("the dappled forest grew %d poplar logs in colours %v", logs, colours)
	}
	if coarse == 0 || shrubs == 0 {
		t.Errorf("no coarse dirt (%d) or red shrubs (%d)", coarse, shrubs)
	}
}
