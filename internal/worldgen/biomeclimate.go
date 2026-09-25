package worldgen

// BiomeClimate is a biome's Biome.ClimateSettings as the weather reads it:
// the base temperature, whether it has the FROZEN temperature modifier and
// whether storms bring it any precipitation at all (hasPrecipitation).
// Unknown biomes read as plains.
func BiomeClimate(biome string) (temp float64, frozen, precip bool) {
	t, ok := biomeTemperature[biome]
	if !ok {
		t = 0.8
	}
	return t, frozenModifier[biome], !noPrecipitation[biome]
}

// noPrecipitation is every biome with has_precipitation false.
var noPrecipitation = map[string]bool{
	"minecraft:badlands": true, "minecraft:basalt_deltas": true, "minecraft:crimson_forest": true,
	"minecraft:desert": true, "minecraft:end_barrens": true, "minecraft:end_highlands": true,
	"minecraft:end_midlands": true, "minecraft:eroded_badlands": true, "minecraft:nether_wastes": true,
	"minecraft:savanna": true, "minecraft:savanna_plateau": true, "minecraft:small_end_islands": true,
	"minecraft:soul_sand_valley": true, "minecraft:the_end": true, "minecraft:the_void": true,
	"minecraft:warped_forest": true, "minecraft:windswept_savanna": true, "minecraft:wooded_badlands": true,
}
