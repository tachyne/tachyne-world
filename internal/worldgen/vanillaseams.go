package worldgen

// The seams between the vanilla generator's parts (terrain core, biomes,
// placement, Nether/End), so each is written against an interface rather
// than another part's internals.

// ClimatePoint is vanilla's Climate.TargetPoint: the six climate
// parameters at a point, each quantized (Climate.quantizeCoord: the float
// value times 10000, as a long).
type ClimatePoint struct {
	Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness int64
}

// VanillaClimate is the noise router's climate sampler (Climate.Sampler).
type VanillaClimate interface {
	Sample(x, y, z int) ClimatePoint
}

// VanillaBiomes resolves biomes the way the client sees them: a block
// position's biome after BiomeManager's fuzzed zoom over the 4x4x4 noise
// biomes. Names are full ids ("minecraft:plains").
type VanillaBiomes interface {
	BiomeAt(x, y, z int) string
}

// VanillaNoiseBiomes is the biome source itself: the biome of one 4x4x4
// quart cell (BiomeSource.getNoiseBiome at quart coordinates), with no
// zoom. Structure starts and the stronghold ring search ask this.
type VanillaNoiseBiomes interface {
	NoiseBiome(qx, qy, qz int) string
}

// VanillaHeightmap is one of vanilla's Heightmap.Types.
type VanillaHeightmap uint8

const (
	HeightmapWorldSurfaceWG VanillaHeightmap = iota
	HeightmapWorldSurface
	HeightmapOceanFloorWG
	HeightmapOceanFloor
	HeightmapMotionBlocking
	HeightmapMotionBlockingNoLeaves
)

// VanillaHeights is the terrain's heights before any feature: for a
// column, ChunkGenerator.getBaseHeight — the first free y above the
// highest block the heightmap counts (so a column of solid ground to y=70
// answers 71). The *_WG heightmaps and the others agree before decoration
// except that OCEAN_FLOOR skips fluids.
type VanillaHeights interface {
	BaseHeight(x, z int, hm VanillaHeightmap) int
}

// VanillaColumns is ChunkGenerator.getBaseColumn: the terrain's block at a
// cell before surface rules, carvers and features — stone (the dimension's
// default block), fluid or air. Structure starts that look for solid ground
// (ruined portals, nether fossils) read it; a VanillaHeights may implement
// it too.
type VanillaColumns interface {
	BaseBlock(x, y, z int) uint32
}
