package worldgen

import "testing"

// setEditOverlay gives g a fake edit overlay holding edits.
func setEditOverlay(g *Generator, edits map[[3]int]uint32) {
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(cx, cz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == cx && int32(floorDiv16(p[2])) == cz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
}

// findBiomeSeed finds a seed and a chunk centred in the biome.
func findBiomeSeed(t *testing.T, name string) (int64, int32, int32) {
	t.Helper()
	for seed := int64(1); seed <= 12; seed++ {
		if cx, cz, ok := findBiomeChunk(NewGenerator(seed), func(n string) bool { return n == name }); ok {
			return seed, cx, cz
		}
	}
	t.Skipf("no %s near the origin of seeds 1-12", name)
	return 0, 0, 0
}

// Desert sand has sandstone under it, sand never hangs over a cave, and the
// frozen oceans have holes in their floor.
func TestSurfaceRuleDepths(t *testing.T) {
	seed, cx, cz := findBiomeSeed(t, "minecraft:desert")
	g := NewGenerator(seed)
	deep, hanging := 0, 0
	for x := int(cx)*16 - 64; x < int(cx)*16+64; x++ {
		for z := int(cz)*16 - 64; z < int(cz)*16+64; z++ {
			c := g.columnAt(x, z)
			if c.biome.Name != "minecraft:desert" {
				continue
			}
			if c.surf.deep == Sandstone && c.surf.deepN > 0 && c.block(c.h-5) == Sandstone {
				deep++
			}
			for y := c.h - 4; y < c.h; y++ {
				if s := g.terrainCell(c, x, y, z); (s == Sand || s == RedSand || s == Gravel) && g.terrainCell(c, x, y-1, z) == Air {
					hanging++
				}
			}
		}
	}
	if deep == 0 {
		t.Error("no desert column has sandstone under its sand")
	}
	if hanging != 0 {
		t.Errorf("%d sand cells over a cave", hanging)
	}
	{
		fseed, fcx, fcz := findBiomeSeed(t, "minecraft:frozen_ocean")
		fg := NewGenerator(fseed)
		holes, cols := 0, 0
		for x := int(fcx)*16 - 128; x < int(fcx)*16+128; x++ {
			for z := int(fcz)*16 - 128; z < int(fcz)*16+128; z++ {
				c := fg.columnAt(x, z)
				if !isFrozenOcean(c.biome.Name) || c.h < SeaLevel-6 {
					continue
				}
				cols++
				if c.surf.top == Water || c.surf.top == Air {
					holes++
				}
			}
		}
		t.Logf("frozen ocean: %d holes in %d shallow columns", holes, cols)
		if cols > 2000 && (holes == 0 || holes*10 > cols) {
			t.Errorf("frozen ocean holes: %d of %d shallow columns", holes, cols)
		}
	}
}
