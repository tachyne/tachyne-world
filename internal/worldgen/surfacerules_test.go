package worldgen

import "testing"

func countTops(g *Generator, want func(string) bool, chunks int32, pick func(uint32) bool) (int, bool) {
	cx, cz, ok := findBiomeChunk(g, want)
	if !ok {
		return 0, false
	}
	n := 0
	for dcx := -chunks; dcx <= chunks; dcx++ {
		for dcz := -chunks; dcz <= chunks; dcz++ {
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					wx, wz := int(cx+dcx)*16+lx, int(cz+dcz)*16+lz
					if !want(g.resolveBiome(wx, wz).Name) {
						continue
					}
					h := g.Height(wx, wz)
					if pick(sectionBlockAt(ch, lx, h-1, lz)) {
						n++
					}
				}
			}
		}
	}
	return n, true
}

// The surface rules: snowy plains are grass under snow, the frozen peaks
// carry packed ice, the badlands' heights are terracotta and their lows
// red sand, and the windswept savanna has coarse dirt among its grass.
func TestSurfaceRules(t *testing.T) {
	g := NewGenerator(5)
	is := func(name string) func(string) bool { return func(n string) bool { return n == name } }
	if n, ok := countTops(g, is("minecraft:snowy_plains"), 1, func(s uint32) bool {
		info, ok := InfoForState(s)
		return ok && info.HasProperty("snowy") // grass, snowy or not
	}); ok && n == 0 {
		t.Error("snowy plains have no grass under their snow")
	}
	if n, ok := countTops(g, is("minecraft:frozen_peaks"), 2, func(s uint32) bool { return s == PackedIce }); ok && n == 0 {
		t.Error("frozen peaks have no packed ice")
	}
	if n, ok := countTops(g, is("minecraft:windswept_savanna"), 2, func(s uint32) bool { return s == CoarseDirt }); ok && n == 0 {
		t.Error("windswept savanna has no coarse dirt")
	}
	if cx, cz, ok := findBiomeChunk(g, is("minecraft:badlands")); ok {
		high, low, highTerra, lowRed := 0, 0, 0, 0
		for dcx := int32(-3); dcx <= 3; dcx++ {
			for dcz := int32(-3); dcz <= 3; dcz++ {
				ch := g.GenerateChunk(cx+dcx, cz+dcz)
				for lx := 0; lx < 16; lx++ {
					for lz := 0; lz < 16; lz++ {
						wx, wz := int(cx+dcx)*16+lx, int(cz+dcz)*16+lz
						if g.resolveBiome(wx, wz).Name != "minecraft:badlands" {
							continue
						}
						h := g.Height(wx, wz)
						top := sectionBlockAt(ch, lx, h-1, lz)
						if h-1 >= 74 {
							high++
							if top != RedSand && top != Air {
								highTerra++
							}
						} else if h >= SeaLevel-1 {
							low++
							if top == RedSand {
								lowRed++
							}
						}
					}
				}
			}
		}
		if high > 0 && highTerra == 0 {
			t.Error("badlands above y=74 have no terracotta surface")
		}
		if low > 0 && lowRed == 0 {
			t.Error("low badlands have no red sand")
		}
	}
}
