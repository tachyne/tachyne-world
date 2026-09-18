package worldgen

import "testing"

// Mushrooms grow in the nether's wastes and under the overworld's cover;
// a frozen ocean's surface is mostly ice with patches of open water.
func TestSmallFeatures(t *testing.T) {
	n := NewNetherGenerator(11)
	shrooms := 0
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			ch := n.generateNetherChunk(cx, cz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b == BrownMushroom || b == RedMushroom {
						shrooms++
					}
				}
			}
		}
	}
	if shrooms == 0 {
		t.Error("no mushrooms in forty-nine nether chunks")
	}
	g := NewGenerator(5)
	if cx, cz, ok := findBiomeChunk(g, func(n string) bool { return n == "minecraft:frozen_ocean" }); ok {
		ice, water := 0, 0
		for dcx := int32(-2); dcx <= 2; dcx++ {
			for dcz := int32(-2); dcz <= 2; dcz++ {
				ch := g.GenerateChunk(cx+dcx, cz+dcz)
				for lx := 0; lx < 16; lx++ {
					for lz := 0; lz < 16; lz++ {
						if g.resolveBiome(int(cx+dcx)*16+lx, int(cz+dcz)*16+lz).Name != "minecraft:frozen_ocean" {
							continue
						}
						switch sectionBlockAt(ch, lx, SeaLevel-1, lz) {
						case Ice:
							ice++
						case Water:
							water++
						}
					}
				}
			}
		}
		if ice == 0 || water == 0 {
			t.Errorf("a frozen ocean should be ice with open patches: ice %d water %d", ice, water)
		}
		t.Logf("frozen ocean surface: ice %d, open water %d", ice, water)
	}
	// underwater magma: a boxed-in sea-floor cell under water turns to magma
	ch := NewChunk(SectionCount)
	reg := &owRegion{g: g, ch: ch, baseX: 300000, baseZ: 300000}
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := 20; y <= 30; y++ {
				setSectionBlock(ch, x, y, z, Stone, true)
			}
			for y := 31; y <= 40; y++ {
				setSectionBlock(ch, x, y, z, Water, true)
			}
		}
	}
	r := newTreeRNG(5, 300000, 300000)
	magma := 0
	for i := 0; i < 40 && magma == 0; i++ {
		floor, _, hasFloor, _, ok := reg.columnScan(300008, 33, 300008, 5, func(s uint32) bool { return s == Water }, func(s uint32) bool { return s != Water })
		if !ok || !hasFloor || floor != 30 {
			t.Fatalf("water column scan: floor %d has %v ok %v", floor, hasFloor, ok)
		}
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if r.Float64() < 0.5 {
					reg.set(300008+dx, 30, 300008+dz, MagmaBlock)
				}
			}
		}
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				if sectionBlockAt(ch, x, 30, z) == MagmaBlock {
					magma++
				}
			}
		}
	}
	if magma == 0 {
		t.Error("no magma placed")
	}
}
