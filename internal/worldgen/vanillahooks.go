package worldgen

// The vanilla generator's terrain fills a chunk's per-section biomes from
// the vanilla biome source (each section's most common quart biome).
func init() { vtSectionBiomes = VanillaSectionBiomes }
