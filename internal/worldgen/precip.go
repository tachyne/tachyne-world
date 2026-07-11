package worldgen

// Precipitation classification, from the vanilla biome climate data
// (data/worldgen/biome/OverworldBiomes: every biome's base temperature and
// hasPrecipitation flag). A biome without precipitation never rains or snows;
// otherwise the height-adjusted temperature picks snow (< 0.15) or rain.
// Vanilla adjusts temperature above the snow line (sea level + 17) by
// -(noise + y - snowLine) * 0.05 / 40 with a ±8 noise term; we drop the noise
// for a deterministic snow line (same slope, no dithered edge).

// Precipitation kinds (vanilla Biome.Precipitation).
const (
	PrecipNone = iota
	PrecipRain
	PrecipSnow
)

// noPrecipBiomes: vanilla hasPrecipitation(false) — desert/savanna/badlands
// families. Storms pass over them with no rain and no lightning.
var noPrecipBiomes = map[string]bool{
	"minecraft:desert":            true,
	"minecraft:savanna":           true,
	"minecraft:savanna_plateau":   true,
	"minecraft:windswept_savanna": true,
	"minecraft:badlands":          true,
	"minecraft:wooded_badlands":   true,
	"minecraft:eroded_badlands":   true,
}

// biomeBaseTemp: vanilla base temperatures for the overworld biomes our
// generator emits (unlisted biomes fall back to plains-warm 0.8, which rains).
var biomeBaseTemp = map[string]float32{
	"minecraft:plains":                   0.8,
	"minecraft:sunflower_plains":         0.8,
	"minecraft:meadow":                   0.5,
	"minecraft:cherry_grove":             0.5,
	"minecraft:forest":                   0.7,
	"minecraft:flower_forest":            0.7,
	"minecraft:birch_forest":             0.6,
	"minecraft:old_growth_birch_forest":  0.6,
	"minecraft:dark_forest":              0.7,
	"minecraft:pale_garden":              0.7,
	"minecraft:taiga":                    0.25,
	"minecraft:old_growth_pine_taiga":    0.3,
	"minecraft:old_growth_spruce_taiga":  0.25,
	"minecraft:windswept_hills":          0.2,
	"minecraft:windswept_forest":         0.2,
	"minecraft:windswept_gravelly_hills": 0.2,
	"minecraft:stony_peaks":              1.0,
	"minecraft:jungle":                   0.95,
	"minecraft:sparse_jungle":            0.95,
	"minecraft:bamboo_jungle":            0.95,
	"minecraft:swamp":                    0.8,
	"minecraft:mangrove_swamp":           0.8,
	"minecraft:mushroom_fields":          0.9,
	"minecraft:beach":                    0.8,
	"minecraft:stony_shore":              0.2,
	"minecraft:river":                    0.5,
	"minecraft:ocean":                    0.5,
	"minecraft:deep_ocean":               0.5,
	"minecraft:cold_ocean":               0.5,
	"minecraft:deep_cold_ocean":          0.5,
	"minecraft:lukewarm_ocean":           0.5,
	"minecraft:deep_lukewarm_ocean":      0.5,
	"minecraft:warm_ocean":               0.5,
	"minecraft:deep_frozen_ocean":        0.5,

	// cold enough to snow at any height (temperature < 0.15)
	"minecraft:snowy_plains": 0.0,
	"minecraft:ice_spikes":   0.0,
	"minecraft:snowy_taiga":  -0.5,
	"minecraft:snowy_beach":  0.05,
	"minecraft:frozen_river": 0.0,
	"minecraft:frozen_ocean": 0.0,
	"minecraft:grove":        -0.2,
	"minecraft:snowy_slopes": -0.3,
	"minecraft:jagged_peaks": -0.7,
	"minecraft:frozen_peaks": -0.7,
}

// PrecipitationAt reports what falls from a storm at a biome and height:
// nothing in dry biomes, snow where the height-adjusted temperature is below
// vanilla's 0.15 threshold, rain everywhere else.
func PrecipitationAt(biome string, y int) int {
	if noPrecipBiomes[biome] {
		return PrecipNone
	}
	t, ok := biomeBaseTemp[biome]
	if !ok {
		t = 0.8
	}
	if snowLine := SeaLevel + 17; y > snowLine {
		t -= float32(y-snowLine) * 0.05 / 40
	}
	if t < 0.15 {
		return PrecipSnow
	}
	return PrecipRain
}
