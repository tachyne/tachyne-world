package worldgen

// The void: FlatLevelSource under the flat preset "the_void" — one layer of
// air, the minecraft:the_void biome, and that biome's one feature,
// VoidStartPlatformFeature, so whoever arrives at the origin has somewhere
// to stand. Everything else is air: no structures, no caves, no mobs (the
// biome's spawn lists are empty). It is the generator for a dimension that
// exists to be built in.

// voidBiome is the void's one biome.
const voidBiome = "minecraft:the_void"

// voidPlatformY is VoidStartPlatformFeature's level: PLATFORM_OFFSET's y (3)
// above the decoration origin, which is the bottom of the world
// (ChunkGenerator.applyBiomeDecoration places from the lowest section).
const voidPlatformY = MinY + 3

// NewVoidGenerator builds the void's generator.
func NewVoidGenerator(seed int64) *Generator {
	g := NewGenerator(seed)
	g.void = true
	return g
}

// voidPlatformAt reports whether a column carries the start platform:
// within checkerboard distance 16 of (8, 8), in the chunks within one of
// the chunk holding it (PLATFORM_RADIUS, PLATFORM_RADIUS_CHUNKS) — which
// the radius never reaches past, so the chunk test changes nothing.
func voidPlatformAt(x, z int) bool {
	return max(abs(x-8), abs(z-8)) <= 16
}

// voidBlock is the void's block at a cell: the platform's stone, cobblestone
// at its centre, air everywhere else.
func voidBlock(x, y, z int) uint32 {
	if y != voidPlatformY || !voidPlatformAt(x, z) {
		return Air
	}
	if x == 8 && z == 8 {
		return Cobblestone
	}
	return Stone
}

// voidHeight is the first air above a column: over the platform, the cell
// above it; elsewhere the bottom of the world, where there is nothing.
func voidHeight(x, z int) int {
	if voidPlatformAt(x, z) {
		return voidPlatformY + 1
	}
	return MinY
}

// generateVoidChunk is one chunk of the void.
func (g *Generator) generateVoidChunk(cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	for s := range ch.Biomes {
		ch.Biomes[s] = voidBiome
	}
	s, ly := (voidPlatformY-MinY)/16, (voidPlatformY-MinY)%16
	for lz := 0; lz < 16; lz++ {
		for lx := 0; lx < 16; lx++ {
			if b := voidBlock(int(cx)*16+lx, voidPlatformY, int(cz)*16+lz); b != Air {
				ch.Sections[s][(ly*16+lz)*16+lx] = b
			}
		}
	}
	ch.computeHeightmap()
	return ch
}
