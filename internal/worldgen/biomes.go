package worldgen

import "math"

// Biome placement. Vanilla 1.21.5 uses a 6-parameter multi-noise climate; we
// use a practical temperature × humidity × elevation model that reproduces the
// full overworld biome set at a coarser grain. A biome carries everything the
// generator needs: its registry name (the client colours/fog from that), the
// surface blocks to lay, and which trees + ground cover to decorate with.
//
// The elevation bands (ocean → shore → lowland → hill → peak) are read from the
// terrain Height; within the lowland band a temperature/humidity matrix picks
// the land biome, and a low-frequency "variety" field selects sub-variants
// (plains↔sunflower_plains, forest↔flower_forest, taiga↔old_growth, …).

// treeKind selects the trunk/canopy a biome decorates with (treeNone = open).
type treeKind int

const (
	treeNone treeKind = iota
	treeOak
	treeBirch
	treeSpruce   // conical
	treeJungle   // tall
	treeAcacia   // sparse, bushy
	treeDarkOak  // thick, dense
	treeCherry   // pink canopy
	treeMangrove // swamp
)

// floraKind selects the ground-cover style stampGroundCover paints.
type floraKind int

const (
	floraNone floraKind = iota
	floraPlains
	floraFlower
	floraDesert
	floraBadlands
	floraTaiga
	floraJungle
	floraSwamp
	floraSavanna
	floraDarkForest
	floraMushroom
	floraSnowy
)

// Biome is a resolved biome: its name plus the generation data for its columns.
type Biome struct {
	Name        string
	Top, Sub    uint32
	Tree        treeKind
	TreeDensity float64 // multiplier on the base tree probability
	Flora       floraKind
}

// biomeReg holds the generation data for every biome the selector can emit.
// Names not present fall back to a plains-like default (grass + sparse oak).
var biomeReg = map[string]*Biome{
	// ── Temperate/plains family ──────────────────────────────────────────
	"minecraft:plains":                  {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 0.3, Flora: floraPlains},
	"minecraft:sunflower_plains":        {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 0.3, Flora: floraFlower},
	"minecraft:meadow":                  {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 0.05, Flora: floraFlower},
	"minecraft:forest":                  {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 1.6, Flora: floraPlains},
	"minecraft:flower_forest":           {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 1.2, Flora: floraFlower},
	"minecraft:birch_forest":            {Top: GrassBlock, Sub: Dirt, Tree: treeBirch, TreeDensity: 1.6, Flora: floraPlains},
	"minecraft:old_growth_birch_forest": {Top: GrassBlock, Sub: Dirt, Tree: treeBirch, TreeDensity: 2.0, Flora: floraPlains},
	"minecraft:dark_forest":             {Top: GrassBlock, Sub: Dirt, Tree: treeDarkOak, TreeDensity: 3.0, Flora: floraDarkForest},
	"minecraft:pale_garden":             {Top: GrassBlock, Sub: Dirt, Tree: treeDarkOak, TreeDensity: 2.4, Flora: floraDarkForest},
	"minecraft:cherry_grove":            {Top: GrassBlock, Sub: Dirt, Tree: treeCherry, TreeDensity: 1.2, Flora: floraFlower},

	// ── Cold/taiga family ────────────────────────────────────────────────
	"minecraft:taiga":                   {Top: GrassBlock, Sub: Dirt, Tree: treeSpruce, TreeDensity: 1.7, Flora: floraTaiga},
	"minecraft:snowy_taiga":             {Top: SnowBlock, Sub: Dirt, Tree: treeSpruce, TreeDensity: 1.4, Flora: floraSnowy},
	"minecraft:old_growth_pine_taiga":   {Top: Podzol, Sub: Dirt, Tree: treeSpruce, TreeDensity: 2.2, Flora: floraTaiga},
	"minecraft:old_growth_spruce_taiga": {Top: Podzol, Sub: Dirt, Tree: treeSpruce, TreeDensity: 2.4, Flora: floraTaiga},
	"minecraft:grove":                   {Top: SnowBlock, Sub: Dirt, Tree: treeSpruce, TreeDensity: 1.2, Flora: floraSnowy},

	// ── Snow/ice ─────────────────────────────────────────────────────────
	"minecraft:snowy_plains": {Top: SnowBlock, Sub: Dirt, Tree: treeNone, Flora: floraSnowy},
	"minecraft:ice_spikes":   {Top: SnowBlock, Sub: Dirt, Tree: treeNone, Flora: floraNone},

	// ── Mountains/peaks ──────────────────────────────────────────────────
	"minecraft:snowy_slopes": {Top: SnowBlock, Sub: Dirt, Tree: treeSpruce, TreeDensity: 0.1, Flora: floraSnowy},
	"minecraft:frozen_peaks": {Top: SnowBlock, Sub: Stone, Tree: treeNone, Flora: floraNone},
	"minecraft:jagged_peaks": {Top: SnowBlock, Sub: Stone, Tree: treeNone, Flora: floraNone},
	"minecraft:stony_peaks":  {Top: Stone, Sub: Stone, Tree: treeNone, Flora: floraNone},

	// ── Windswept ────────────────────────────────────────────────────────
	"minecraft:windswept_hills":          {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 0.3, Flora: floraPlains},
	"minecraft:windswept_forest":         {Top: GrassBlock, Sub: Dirt, Tree: treeSpruce, TreeDensity: 1.2, Flora: floraTaiga},
	"minecraft:windswept_gravelly_hills": {Top: Gravel, Sub: Stone, Tree: treeNone, Flora: floraNone},
	"minecraft:windswept_savanna":        {Top: GrassBlock, Sub: Dirt, Tree: treeAcacia, TreeDensity: 0.15, Flora: floraSavanna},

	// ── Warm/dry ─────────────────────────────────────────────────────────
	"minecraft:savanna":         {Top: GrassBlock, Sub: Dirt, Tree: treeAcacia, TreeDensity: 0.25, Flora: floraSavanna},
	"minecraft:savanna_plateau": {Top: GrassBlock, Sub: Dirt, Tree: treeAcacia, TreeDensity: 0.2, Flora: floraSavanna},
	"minecraft:desert":          {Top: Sand, Sub: Sandstone, Tree: treeNone, Flora: floraDesert},
	"minecraft:badlands":        {Top: RedSand, Sub: Terracotta, Tree: treeNone, Flora: floraBadlands},
	"minecraft:wooded_badlands": {Top: RedSand, Sub: Terracotta, Tree: treeOak, TreeDensity: 0.4, Flora: floraBadlands},
	"minecraft:eroded_badlands": {Top: RedSand, Sub: Terracotta, Tree: treeNone, Flora: floraBadlands},

	// ── Jungle ───────────────────────────────────────────────────────────
	"minecraft:jungle":        {Top: GrassBlock, Sub: Dirt, Tree: treeJungle, TreeDensity: 3.0, Flora: floraJungle},
	"minecraft:sparse_jungle": {Top: GrassBlock, Sub: Dirt, Tree: treeJungle, TreeDensity: 0.8, Flora: floraJungle},
	"minecraft:bamboo_jungle": {Top: GrassBlock, Sub: Dirt, Tree: treeJungle, TreeDensity: 1.5, Flora: floraJungle},

	// ── Wet lowland ──────────────────────────────────────────────────────
	"minecraft:swamp":           {Top: GrassBlock, Sub: Dirt, Tree: treeOak, TreeDensity: 0.5, Flora: floraSwamp},
	"minecraft:mangrove_swamp":  {Top: Mud, Sub: Dirt, Tree: treeMangrove, TreeDensity: 1.0, Flora: floraSwamp},
	"minecraft:mushroom_fields": {Top: Mycelium, Sub: Dirt, Tree: treeNone, Flora: floraMushroom},

	// ── Shores ───────────────────────────────────────────────────────────
	"minecraft:beach":       {Top: Sand, Sub: Sand, Tree: treeNone, Flora: floraNone},
	"minecraft:snowy_beach": {Top: Sand, Sub: Sand, Tree: treeNone, Flora: floraSnowy},
	"minecraft:stony_shore": {Top: Stone, Sub: Stone, Tree: treeNone, Flora: floraNone},

	// ── Water (sea floor is sand/gravel; the biome tints the water) ───────
	"minecraft:ocean":               {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:deep_ocean":          {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:cold_ocean":          {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:deep_cold_ocean":     {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:frozen_ocean":        {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:deep_frozen_ocean":   {Top: Gravel, Sub: Gravel, Tree: treeNone, Flora: floraNone},
	"minecraft:lukewarm_ocean":      {Top: Sand, Sub: Sand, Tree: treeNone, Flora: floraNone},
	"minecraft:deep_lukewarm_ocean": {Top: Sand, Sub: Sand, Tree: treeNone, Flora: floraNone},
	"minecraft:warm_ocean":          {Top: Sand, Sub: Sand, Tree: treeNone, Flora: floraNone},

	// ── Rivers ───────────────────────────────────────────────────────────
	"minecraft:river":        {Top: Gravel, Sub: Dirt, Tree: treeNone, Flora: floraNone},
	"minecraft:frozen_river": {Top: Gravel, Sub: Dirt, Tree: treeNone, Flora: floraNone},

	// ── Underground (vertical sections) ──────────────────────────────────
	"minecraft:dripstone_caves": {Top: Stone, Sub: Stone, Tree: treeNone, Flora: floraNone},
	"minecraft:lush_caves":      {Top: GrassBlock, Sub: Dirt, Tree: treeNone, Flora: floraNone},
	"minecraft:deep_dark":       {Top: Stone, Sub: Stone, Tree: treeNone, Flora: floraNone},
}

// init stamps each biome's registry name from its map key, so the Biome struct
// literals don't have to repeat the name (the client colours/fog from Name).
func init() {
	for name, b := range biomeReg {
		b.Name = name
	}
}

var defaultBiome = biomeReg["minecraft:plains"]

// biomeFor returns the biome data for a name (plains fallback for the unknown).
func biomeFor(name string) *Biome {
	if b := biomeReg[name]; b != nil {
		return b
	}
	return defaultBiome
}

// band maps a climate value in [-1,1] onto an index 0..4 using the given
// ascending thresholds (len 4).
func band(v float64, t0, t1, t2, t3 float64) int {
	switch {
	case v < t0:
		return 0
	case v < t1:
		return 1
	case v < t2:
		return 2
	case v < t3:
		return 3
	}
	return 4
}

// climateBands returns the temperature and humidity band indices (0..4) and the
// raw variety value at a column. Temperature already includes the altitude
// lapse from climate().
func (g *Generator) climateBands(wx, wz, h int) (ti, hi int, variety float64) {
	t, hm := g.climate(wx, wz, h)
	// A gentler spread than the terrain stretch (so frozen/hot extremes stay
	// uncommon) plus a small warm bias, so the median column lands temperate —
	// the world isn't snow-dominated.
	t = t*1.8 + 0.10
	hm = hm * 1.8
	ti = band(t, -0.45, -0.15, 0.20, 0.50)
	hi = band(hm, -0.35, -0.10, 0.15, 0.40)
	variety = g.variety.FBm(float64(wx)/220, float64(wz)/220, 2, 2, 0.5) // ~±0.4
	return ti, hi, variety
}

// landBiome is the temperature × humidity matrix for lowland land, with the
// variety field selecting sub-variants. Modelled on vanilla's biome table.
func landBiome(ti, hi int, variety float64) string {
	hot := variety > 0.15 // "weird" side of the variety field
	switch ti {
	case 0: // frozen
		switch hi {
		case 0, 1:
			if variety < -0.25 {
				return "minecraft:ice_spikes"
			}
			return "minecraft:snowy_plains"
		case 2:
			return "minecraft:snowy_plains"
		default:
			return "minecraft:snowy_taiga"
		}
	case 1: // cold
		switch hi {
		case 0, 1:
			return "minecraft:plains"
		case 2:
			return "minecraft:forest"
		case 3:
			return "minecraft:taiga"
		default:
			if hot {
				return "minecraft:old_growth_spruce_taiga"
			}
			return "minecraft:old_growth_pine_taiga"
		}
	case 2: // temperate
		switch hi {
		case 0:
			if hot {
				return "minecraft:sunflower_plains"
			}
			return "minecraft:plains"
		case 1:
			return "minecraft:plains"
		case 2:
			if hot {
				return "minecraft:flower_forest"
			}
			return "minecraft:forest"
		case 3:
			if variety < -0.2 {
				return "minecraft:old_growth_birch_forest"
			}
			return "minecraft:birch_forest"
		default:
			if variety < -0.2 {
				return "minecraft:pale_garden"
			}
			return "minecraft:dark_forest"
		}
	case 3: // warm
		switch hi {
		case 0, 1:
			return "minecraft:savanna"
		case 2:
			if hot {
				return "minecraft:cherry_grove"
			}
			return "minecraft:forest"
		case 3:
			return "minecraft:jungle"
		default:
			return "minecraft:bamboo_jungle"
		}
	default: // hot
		switch hi {
		case 0:
			switch {
			case variety < -0.2:
				return "minecraft:eroded_badlands"
			case variety < 0.05:
				return "minecraft:badlands"
			default:
				return "minecraft:desert"
			}
		case 1, 2:
			if variety < -0.15 {
				return "minecraft:wooded_badlands"
			}
			return "minecraft:desert"
		case 3:
			return "minecraft:sparse_jungle"
		default:
			return "minecraft:jungle"
		}
	}
}

// resolveBiome is the single biome authority for an overworld column: it reads
// the elevation band from Height (river-carved), then oceans/shores/peaks
// directly and hands the lowland band to the climate matrix.
func (g *Generator) resolveBiome(wx, wz int) *Biome {
	baseH := g.landHeight(wx, wz)
	h := g.Height(wx, wz)
	ti, hi, variety := g.climateBands(wx, wz, h)
	if g.earth != nil && ti == 0 {
		// Earth v1: no frozen band from climate NOISE — real snow comes from
		// real altitude (the alpine tier below). A per-region base climate
		// (arctic regions earn their frost back) is the eventual replacement.
		ti = 1
	}

	// River: a column carved below sea level whose un-carved land sat above it.
	if h <= SeaLevel-1 && baseH > SeaLevel {
		if ti == 0 {
			return biomeFor("minecraft:frozen_river")
		}
		return biomeFor("minecraft:river")
	}

	// Ocean: temperature + depth pick the variant.
	if h <= SeaLevel-1 {
		deep := h <= SeaLevel-12
		switch ti {
		case 0:
			return biomeFor(oceanName("frozen_ocean", "deep_frozen_ocean", deep))
		case 1:
			return biomeFor(oceanName("cold_ocean", "deep_cold_ocean", deep))
		case 2:
			return biomeFor(oceanName("ocean", "deep_ocean", deep))
		case 3:
			return biomeFor(oceanName("lukewarm_ocean", "deep_lukewarm_ocean", deep))
		default:
			if deep {
				return biomeFor("minecraft:deep_lukewarm_ocean")
			}
			return biomeFor("minecraft:warm_ocean")
		}
	}

	// Rare mushroom island: warm, isolated land ringed low.
	if baseH <= SeaLevel+6 && variety < -0.34 && ti >= 2 {
		return biomeFor("minecraft:mushroom_fields")
	}

	// Shore: a thin band at the waterline.
	if h <= SeaLevel+2 {
		switch {
		case ti == 0:
			return biomeFor("minecraft:snowy_beach")
		case ti == 4 && variety > 0.2:
			return biomeFor("minecraft:stony_shore")
		default:
			return biomeFor("minecraft:beach")
		}
	}

	// Peaks + mountains. In earth mode the tiers are REAL metres — the noise
	// world's block thresholds sit at ~300 m real under vertical compression,
	// which alpine-ified every Cape Town hill (grove on Signal Hill,
	// frozen_peaks on Table Mountain). Snow needs genuine snowline altitude.
	if g.earth != nil {
		switch realM := float64(h-SeaLevel) * g.earth.vscale; {
		case realM >= 2600: // alpine: the real mid-latitude snowline
			if ti <= 1 {
				return biomeFor("minecraft:frozen_peaks")
			}
			if variety > 0 {
				return biomeFor("minecraft:jagged_peaks")
			}
			return biomeFor("minecraft:stony_peaks")
		case realM >= 1000: // high mountain: bare rock and alpine meadow
			if ti == 0 {
				return biomeFor("minecraft:snowy_slopes")
			}
			if variety > 0 {
				return biomeFor("minecraft:stony_peaks") // Table Mountain's rock plateau
			}
			return biomeFor("minecraft:meadow")
		case realM >= 550: // windswept upper slopes and ridgelines
			switch {
			case hi >= 3:
				return biomeFor("minecraft:windswept_forest")
			case variety > 0.15:
				return biomeFor("minecraft:windswept_gravelly_hills")
			default:
				return biomeFor("minecraft:windswept_hills")
			}
		}
		return biomeFor(landBiome(ti, hi, variety)) // lowland: the climate matrix
	}

	// Peaks + mountains, tiered by height then temperature.
	switch {
	case h >= 132:
		switch ti {
		case 0, 1:
			return biomeFor("minecraft:frozen_peaks")
		case 2, 3:
			if variety > 0 {
				return biomeFor("minecraft:jagged_peaks")
			}
			return biomeFor("minecraft:stony_peaks")
		default:
			return biomeFor("minecraft:stony_peaks")
		}
	case h >= 112:
		switch ti {
		case 0, 1:
			return biomeFor("minecraft:snowy_slopes")
		case 2:
			if variety > 0 {
				return biomeFor("minecraft:grove")
			}
			return biomeFor("minecraft:meadow")
		default:
			return biomeFor("minecraft:meadow")
		}
	case h >= 92: // windswept hills band
		switch {
		case hi >= 3:
			return biomeFor("minecraft:windswept_forest")
		case variety > 0.15:
			return biomeFor("minecraft:windswept_gravelly_hills")
		case ti >= 3:
			return biomeFor("minecraft:windswept_savanna")
		default:
			return biomeFor("minecraft:windswept_hills")
		}
	}

	// Wet + warm lowland right at the waterline → swamp.
	if h <= SeaLevel+4 && hi >= 4 && ti >= 2 {
		if ti >= 4 {
			return biomeFor("minecraft:mangrove_swamp")
		}
		return biomeFor("minecraft:swamp")
	}

	// Savanna plateau: high, warm, dry tableland.
	if h >= 84 && ti == 3 && hi <= 1 {
		return biomeFor("minecraft:savanna_plateau")
	}

	return biomeFor(landBiome(ti, hi, variety))
}

// oceanName returns the deep or shallow ocean identifier.
func oceanName(shallow, deep string, isDeep bool) string {
	if isDeep {
		return "minecraft:" + deep
	}
	return "minecraft:" + shallow
}

// netherBiome divides the Nether into its biome regions by a large-scale
// temperature/humidity split (crimson vs warped forest, soul-sand valleys,
// basalt deltas), so fog, mobs and vegetation vary across the dimension.
func (g *Generator) netherBiome(wx, wz int) string {
	t := g.temp.FBm(float64(wx)/320, float64(wz)/320, 2, 2, 0.5)
	hm := g.humid.FBm(float64(wx)/320, float64(wz)/320, 2, 2, 0.5)
	switch {
	case t < -0.18:
		return "minecraft:soul_sand_valley"
	case t > 0.22 && hm > 0.05:
		return "minecraft:warped_forest"
	case t > 0.12:
		return "minecraft:crimson_forest"
	case hm < -0.2:
		return "minecraft:basalt_deltas"
	}
	return "minecraft:nether_wastes"
}

// endBiome rings the End: the central island is the_end, the outer islands
// tier through highlands/midlands/barrens, with scattered small islands in the
// deep void.
func (g *Generator) endBiome(wx, wz int) string {
	d := math.Hypot(float64(wx), float64(wz))
	switch {
	case d < 1000:
		return "minecraft:the_end"
	case d < 1600:
		n := g.variety.FBm(float64(wx)/300, float64(wz)/300, 2, 2, 0.5)
		if n > 0.1 {
			return "minecraft:end_highlands"
		}
		if n < -0.15 {
			return "minecraft:end_barrens"
		}
		return "minecraft:end_midlands"
	default:
		return "minecraft:small_end_islands"
	}
}

// caveBiome returns the underground biome for a section whose centre is at
// world-Y cy, well below the surface. Deep, quiet regions become deep_dark;
// damp regions lush_caves; the rest dripstone_caves.
func (g *Generator) caveBiome(wx, wz, cy int) string {
	n := g.cave.FBm(float64(wx)/260, float64(cy)/120, 2, 2, 0.5) + g.cave.FBm(float64(wz)/260, 0, 1, 2, 0.5)
	switch {
	case cy < -32 && n > 0.35:
		return "minecraft:deep_dark"
	case n < -0.35:
		return "minecraft:lush_caves"
	default:
		return "minecraft:dripstone_caves"
	}
}
