package worldgen

// Three small features vanilla places everywhere and the engine lacked.
// Mushrooms: in the nether wastes and forests one chunk in two grows a
// brown and one in two a red patch (96 tries about a random cell, on any
// solid block); in the overworld the same patches sit on the surface in
// the dark — one chunk in 256 brown and 512 red in most biomes, one in 4
// and 256 in the taigas, three in four and one in 171 in the old-growth
// taigas, two a chunk and one in 64 in the swamps (VegetationPlacements
// .getMushroomPlacement; MushroomBlock wants light under 13, read here as
// a canopy overhead). Underwater magma: 44–52 tries a chunk below the sea
// floor for a water column's floor, half the solid cells within one of it
// that are boxed in by full blocks turning to magma (UnderwaterMagma
// Feature 5/1/0.5).

var (
	brownMushroomPatch = [2]uint32{BrownMushroom, RedMushroom}
)

// mushroomPatch is the mushroom placed feature's tail: 96 tries with a
// triangle offset (7, 3) about the origin, the mushroom on any air cell over
// a block that holds it and, in the overworld, under cover.
func (g *Generator) mushroomPatch(r TreeRNG, read func(x, y, z int) uint32, set func(x, y, z int, s uint32), x, y, z int, mushroom uint32, needCover bool) {
	for i := 0; i < 96; i++ {
		px, py, pz := x+triangleOffset(r, 7), y+triangleOffset(r, 3), z+triangleOffset(r, 7)
		if read(px, py, pz) != Air {
			continue
		}
		below := read(px, py-1, pz)
		if below == Air || IsFluid(below) || !Collides(below) || IsReplaceable(below) {
			continue
		}
		if needCover {
			covered := 0
			for dy := 1; dy <= 16 && covered < 3; dy++ {
				if s := read(px, py+dy, pz); s != Air {
					covered++
				}
			}
			if covered < 3 {
				continue
			}
		}
		set(px, py, pz, mushroom)
	}
}

// netherMushrooms adds BROWN/RED_MUSHROOM_NETHER to a nether chunk's draws.
func (g *Generator) netherMushrooms(r TreeRNG, ox, oz int, biome string, d FungusDriver) {
	if biome == "minecraft:soul_sand_valley" || biome == "minecraft:basalt_deltas" {
		return // no addDefaultMushrooms in those two
	}
	for _, m := range brownMushroomPatch {
		if r.Intn(2) != 0 {
			continue
		}
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := MinY + 1 + r.Intn(NetherCeiling-MinY-2)
		if g.netherBiome(x, z) == biome {
			g.mushroomPatch(r, d.Read, d.Set, x, y, z, m, false)
		}
	}
}

// overworldMushrooms adds the surface mushroom patches to an overworld
// chunk's draws, by the biome's rates.
func (g *Generator) overworldMushrooms(r TreeRNG, reg *owRegion, ox, oz int) {
	biome := reg.col(ox+8, oz+8).biome.Name
	brownRarity, redRarity, brownCount := 256, 512, 1
	switch biome {
	case "minecraft:taiga", "minecraft:snowy_taiga":
		brownRarity, redRarity = 4, 256
	case "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga":
		brownRarity, redRarity, brownCount = 4, 171, 3
	case "minecraft:swamp", "minecraft:mangrove_swamp":
		brownRarity, redRarity, brownCount = 0, 64, 2
	}
	place := func(m uint32, rarity, count int) {
		for i := 0; i < count; i++ {
			if rarity > 0 && r.Intn(rarity) != 0 {
				continue
			}
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			c := reg.col(x, z)
			if c.biome.Name != biome {
				continue
			}
			g.mushroomPatch(r, reg.read, reg.set, x, c.h, z, m, true)
		}
	}
	place(BrownMushroom, brownRarity, brownCount)
	place(RedMushroom, redRarity, 1)
}

// underwaterMagma is CavePlacements.UNDERWATER_MAGMA: 44–52 origins a
// chunk at least two below the sea floor, each a water column's floor
// (found within five), and magma on half the boxed-in solid cells within
// one of it.
func (g *Generator) underwaterMagma(r TreeRNG, reg *owRegion, ox, oz int) {
	fullBlock := func(s uint32) bool { return s != Air && !IsFluid(s) && Collides(s) && !IsReplaceable(s) }
	for i, n := 0, 44+r.Intn(9); i < n; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := MinY + r.Intn(256-MinY+1)
		if y > reg.col(x, z).h-2 { // SurfaceRelativeThresholdFilter(OCEAN_FLOOR_WG, ∞, −2)
			continue
		}
		floor, _, hasFloor, _, ok := reg.columnScan(x, y, z, 5, func(s uint32) bool { return s == Water }, func(s uint32) bool { return s != Water })
		if !ok || !hasFloor {
			continue
		}
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					if r.Float64() >= 0.5 {
						continue
					}
					px, py, pz := x+dx, floor+dy, z+dz
					s := reg.read(px, py, pz)
					if s == Water || s == Air || !fullBlock(reg.read(px, py-1, pz)) {
						continue
					}
					boxed := true
					for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						if !fullBlock(reg.read(px+o[0], py, pz+o[1])) {
							boxed = false
							break
						}
					}
					if boxed {
						reg.set(px, py, pz, MagmaBlock)
					}
				}
			}
		}
	}
}
