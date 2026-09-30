package worldgen

// Ground cover as 26.3 places it: every flower, grass, fern, dead bush,
// cactus, berry bush and bamboo patch of the vegetal-decoration step that
// the old per-column hash scatter (stampGroundCover) used to stand in for.
//
// 26.3 folded RandomPatchFeature into the placements: a patch is an origin
// (rarity, in_square, a heightmap, the biome) followed by count(tries),
// a trapezoid offset (xz, y) and an air filter, and then the plain feature
// — a simple_block with its state provider, a block_column, bamboo — at
// every try that passes. Each placement here is that, with the 26.3
// counts, spreads, filters, providers and biome lists:
//
//   - the flowers: flower_default and flower_warm (poppy two to one over
//     dandelion), flower_plains (the noise-threshold tulips, one origin in
//     thirty-two of four or fifteen), flower_swamp (blue orchids),
//     flower_meadow (the dual-noise meadow mix), flower_cherry (pink petals),
//     flower_flower_forest (the noise-banded flower forest), the forest
//     flowers (lilac, rose bush, peony or lily of the valley), and the pale
//     garden's closed eyeblossoms (flower_pale_garden and
//     pale_garden_flowers). Torchflowers and pitcher plants are not
//     generated in 26.3 (sniffers dig them up), nor are wither roses;
//   - patch_grass_* with each biome's count, the jungle's grass-and-fern
//     mix off podzol, the taigas' fern-led mix, and the dappled forest's
//     red shrubs;
//   - patch_dead_bush, _2 and _badlands; patch_cactus_desert and
//     _decorated (a block column of one to three cacti, a flower on one in
//     four, cut short under anything but air); patch_berry_common and
//     _rare on grass blocks;
//   - bamboo: the bamboo jungle's noise-counted stalks with podzol discs
//     (bamboo_some_podzol), the jungle's sparse bamboo_light, and
//     bamboo_vegetation's default grass patches (its tree slots are drawn
//     and left to the tree pass);
//   - the mushroom fields' own mushrooms (brown and red _taiga) and the
//     dappled forest's brown mushrooms, which the old scatter also drew.
//
// Positions are tachyne's, not a vanilla seed's. The noise providers are
// the 26.3 formulas over tachyne's Perlin rescaled to NormalNoise's target
// deviation. Heightmaps are the terrain's (as in vegetation.go): an origin
// over water is the first cell above the sea.
//
// Every draw comes from this feature's own stream, per origin chunk, and
// is made whether or not the world lets a try place anything, so every
// chunk pass that replays a neighbour walks the same stream and no other
// feature moves. Every plant goes through the build guard.

const groundCoverSalt = 0x6C0CE726

// normalNoiseScale rescales tachyne's Perlin.Noise3 (standard deviation
// 0.287, measured) to NormalNoise's TARGET_DEVIATION of one third, so the
// providers' thresholds and state bands cut the same shares.
const normalNoiseScale = (1.0 / 3.0) / 0.287

// The providers' noises. Vanilla seeds them all from 2345 (they are the
// same in every world); tachyne keeps one field each.
var (
	flowerForestNoise = NewPerlin(2345 ^ 0xF0F0)
	meadowNoise       = NewPerlin(2345 ^ 0x3E4D)
	meadowSlowNoise   = NewPerlin(2345 ^ 0x5105)
)

// Biome lists, from each placed feature's entries in the 26.3 biome files.
var (
	gcFlowerDefault = biomeSet("beach", "birch_forest", "cold_ocean", "dark_forest", "deep_cold_ocean", "deep_frozen_ocean",
		"deep_lukewarm_ocean", "deep_ocean", "desert", "forest", "frozen_ocean", "frozen_river", "ice_spikes",
		"lukewarm_ocean", "ocean", "old_growth_birch_forest", "old_growth_pine_taiga", "old_growth_spruce_taiga",
		"river", "snowy_beach", "snowy_plains", "snowy_taiga", "stony_shore", "taiga", "warm_ocean",
		"windswept_forest", "windswept_gravelly_hills", "windswept_hills", "windswept_savanna")
	gcFlowerPlains       = biomeSet("deep_dark", "dripstone_caves", "plains", "sunflower_plains")
	gcFlowerSwamp        = biomeSet("swamp")
	gcFlowerWarm         = biomeSet("bamboo_jungle", "jungle", "savanna", "savanna_plateau", "sparse_jungle")
	gcFlowerMeadow       = biomeSet("meadow")
	gcFlowerCherry       = biomeSet("cherry_grove")
	gcFlowerFlowerForest = biomeSet("flower_forest")
	gcForestFlowers      = biomeSet("birch_forest", "dark_forest", "forest", "old_growth_birch_forest")
	gcPaleGarden         = biomeSet("pale_garden")
	gcDappledForest      = biomeSet("dappled_forest")
	gcGrassBadlands      = biomeSet("badlands", "beach", "cold_ocean", "deep_cold_ocean", "deep_frozen_ocean",
		"deep_lukewarm_ocean", "deep_ocean", "desert", "eroded_badlands", "flower_forest", "frozen_ocean",
		"frozen_river", "ice_spikes", "lukewarm_ocean", "ocean", "river", "snowy_beach", "snowy_plains",
		"stony_shore", "warm_ocean", "windswept_forest", "windswept_gravelly_hills", "windswept_hills", "wooded_badlands")
	gcGrassForest   = biomeSet("birch_forest", "dappled_forest", "dark_forest", "forest", "old_growth_birch_forest", "pale_garden")
	gcGrassJungle   = biomeSet("bamboo_jungle", "jungle", "sparse_jungle")
	gcGrassMeadow   = biomeSet("meadow")
	gcGrassNormal   = biomeSet("mangrove_swamp", "swamp", "windswept_savanna")
	gcGrassPlain    = biomeSet("cherry_grove", "deep_dark", "dripstone_caves", "plains", "sunflower_plains")
	gcGrassSavanna  = biomeSet("savanna", "savanna_plateau")
	gcGrassTaiga    = biomeSet("old_growth_pine_taiga", "old_growth_spruce_taiga")
	gcGrassTaiga2   = biomeSet("snowy_taiga", "taiga")
	gcDeadBush      = biomeSet("mangrove_swamp", "old_growth_pine_taiga", "old_growth_spruce_taiga", "swamp")
	gcDesert        = biomeSet("desert")
	gcBadlands      = biomeSet("badlands", "eroded_badlands", "wooded_badlands")
	gcBerryCommon   = biomeSet("old_growth_pine_taiga", "old_growth_spruce_taiga", "taiga")
	gcBerryRare     = biomeSet("snowy_taiga")
	gcBambooJungle  = biomeSet("bamboo_jungle")
	gcJungle        = biomeSet("jungle")
	gcMushroomField = biomeSet("mushroom_fields") // taiga and snowy_taiga have theirs in overworldMushrooms
)

// The providers' states.
var (
	gcPlainLow  = statesOf(flowerPlainLow...)
	gcPlainHigh = statesOf(flowerPlainHigh...)
	// flower_meadow's dual-noise states; the tall grass is a double plant.
	gcMeadowStates = []uint32{tallGrassHalves[0], BlockID("allium"), BlockID("poppy"), BlockID("azure_bluet"),
		BlockID("dandelion"), BlockID("cornflower"), BlockID("oxeye_daisy"), BlockID("short_grass")}
	gcFlowerForestStates = statesOf("dandelion", "poppy", "allium", "azure_bluet", "red_tulip", "orange_tulip",
		"white_tulip", "pink_tulip", "oxeye_daisy", "cornflower", "lily_of_the_valley")
	// forest_flowers' simple_random_selector: lilac, rose bush, peony, lily
	// of the valley.
	gcForestFlowerStates = []uint32{withProps("lilac", "half", "lower"), withProps("rose_bush", "half", "lower"),
		withProps("peony", "half", "lower"), BlockID("lily_of_the_valley")}
	gcPinkPetals = segmentStates("pink_petals", "flower_amount", 4)
	// The double plants a provider can hand SimpleBlockFeature, each with
	// its two halves.
	gcDoubles = map[uint32][2]uint32{
		tallGrassHalves[0]:                      tallGrassHalves,
		withProps("lilac", "half", "lower"):     {withProps("lilac", "half", "lower"), withProps("lilac", "half", "upper")},
		withProps("rose_bush", "half", "lower"): {withProps("rose_bush", "half", "lower"), withProps("rose_bush", "half", "upper")},
		withProps("peony", "half", "lower"):     {withProps("peony", "half", "lower"), withProps("peony", "half", "upper")},
	}
	gcBerryBush                = withProps("sweet_berry_bush", "age", "3")
	gcPodzolLo, gcPodzolHi     = BlockRange("podzol")
	gcMyceliumLo, gcMyceliumHi = BlockRange("mycelium")
	// BambooFeature's states: an age-1 trunk, and the leafy top.
	gcBambooTrunk            = withProps("bamboo", "age", "1", "leaves", "none", "stage", "0")
	gcBambooFinalLarge       = withProps("bamboo", "age", "1", "leaves", "large", "stage", "1")
	gcBambooTopLarge         = withProps("bamboo", "age", "1", "leaves", "large", "stage", "0")
	gcBambooTopSmall         = withProps("bamboo", "age", "1", "leaves", "small", "stage", "0")
	gcSandLo, gcSandHi       = BlockRange("sand")
	gcRedSandLo, gcRedSandHi = BlockRange("red_sand")
	gcGravelLo, gcGravelHi   = BlockRange("gravel")
	gcBambooLo, gcBambooHi   = BlockRange("bamboo")
	gcSaplingLo, gcSaplingHi = BlockRange("bamboo_sapling")
)

// statesOf maps block names to their default states.
func statesOf(names ...string) []uint32 {
	out := make([]uint32, len(names))
	for i, n := range names {
		out[i] = BlockID(n)
	}
	return out
}

// normalNoise3 is a NormalNoise-like sample: Perlin rescaled to one third.
func normalNoise3(p *Perlin, x, y, z float64) float64 {
	return normalNoiseScale * p.Noise3(x, y, z)
}

// noiseState is NoiseProvider.getRandomState: the noise mapped from
// [-1, 1] onto the list.
func noiseState(states []uint32, n float64) uint32 {
	v := (1 + n) / 2
	if v < 0 {
		v = 0
	} else if v > 0.9999 {
		v = 0.9999
	}
	return states[int(v*float64(len(states)))]
}

// flowerForestState is flower_flower_forest's NoiseProvider (scale 1/48).
func flowerForestState(x, y, z int) uint32 {
	const s = 0.020833334
	return noiseState(gcFlowerForestStates, normalNoise3(flowerForestNoise, float64(x)*s, float64(y)*s, float64(z)*s))
}

// meadowState is flower_meadow's DualNoiseProvider: a slow noise (octave
// -10) picks how many of the states are in play here (one to three) and
// which, the fast one (octave -3) picks among them.
func meadowState(x, y, z int) uint32 {
	slow := func(x, y, z int) float64 {
		return normalNoise3(meadowSlowNoise, float64(x)/1024, float64(y)/1024, float64(z)/1024)
	}
	t := (slow(x, y, z) + 1) / 2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	variety := int(1 + t*3) // clampedMap(v, -1, 1, 1, 3+1)
	possible := make([]uint32, variety)
	for i := range possible {
		possible[i] = noiseState(gcMeadowStates, slow(x+i*54545, y, z+i*34234))
	}
	return noiseState(possible, normalNoise3(meadowNoise, float64(x)/8, float64(y)/8, float64(z)/8))
}

// biomeInfoNoise is Biome.BIOME_INFO_NOISE: the flower-count noise
// flowerNoiseBelow tests, scaled so its measured 2% quantile sits at
// vanilla's -0.8.
func (g *Generator) biomeInfoNoise(x, z float64) float64 {
	return g.detail.Noise2(x+0.43, z+0.71) * (-0.8 / flowerNoiseThreshold)
}

// coverTries is the tail every 26.3 patch placement shares: count(tries),
// a trapezoid offset (xz, dy) about the origin and the air filter, then
// the feature. state is the provider, drawn for every try whether or not
// it places; place writes the cell (after its own survival check). Only
// cells in the region's chunk, on an air cell the build guard allows, are
// handed to place.
func (g *Generator) coverTries(r TreeRNG, reg *owRegion, bg *buildGuard, x, y, z, tries, xz, dy int,
	state func(x, y, z int) uint32, place func(x, y, z int, s uint32)) {
	for i := 0; i < tries; i++ {
		px, py, pz := x+triangleOffset(r, xz), y+triangleOffset(r, dy), z+triangleOffset(r, xz)
		s := state(px, py, pz)
		if !reg.inChunk(px, pz) || reg.read(px, py, pz) != Air || bg.decorationBlocked(px, py, pz) {
			continue
		}
		place(px, py, pz, s)
	}
}

// groundCover replays chunk (ncx, ncz)'s ground-cover placements into the
// region's chunk (see the file comment).
func (g *Generator) groundCover(reg *owRegion, bg *buildGuard, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^groundCoverSalt, ox, oz)

	// origin is in_square, the heightmap (the terrain, the sea's surface
	// over water) and the biome filter.
	origin := func(biomes map[string]bool) (x, y, z int, ok bool) {
		x, z = ox+r.Intn(16), oz+r.Intn(16)
		c := reg.col(x, z)
		return x, max(c.h, SeaLevel), z, biomes[c.biome.Name]
	}
	rarity := func(chance int) bool { return chance <= 1 || r.Intn(chance) == 0 }

	// Providers.
	fixed := func(s uint32) func(int, int, int) uint32 { return func(int, int, int) uint32 { return s } }
	uniform := func(states []uint32) func(int, int, int) uint32 {
		return func(int, int, int) uint32 { return states[r.Intn(len(states))] }
	}
	// weighted is a two-entry weighted provider: a with weight wa, b with wb.
	weighted := func(a uint32, wa int, b uint32, wb int) func(int, int, int) uint32 {
		return func(int, int, int) uint32 {
			if r.Intn(wa+wb) < wa {
				return a
			}
			return b
		}
	}
	// flower_plain's NoiseThresholdProvider: tulips below the threshold,
	// else one in three a high flower and the rest dandelions.
	plainFlower := func(x, y, z int) uint32 {
		const s = 0.005
		if normalNoise3(flowerPlainNoise, float64(x)*s, float64(y)*s, float64(z)*s) < -0.8 {
			return gcPlainLow[r.Intn(len(gcPlainLow))]
		}
		if r.Float64() < 0.33333334 {
			return gcPlainHigh[r.Intn(len(gcPlainHigh))]
		}
		return Dandelion
	}

	// Features: SimpleBlockFeature on its survival rule.
	simple := func(soil func(uint32) bool) func(x, y, z int, s uint32) {
		return func(x, y, z int, s uint32) {
			if halves, ok := gcDoubles[s]; ok {
				g.plantDouble(reg, bg, x, y, z, halves)
				return
			}
			if soil(reg.read(x, y-1, z)) {
				reg.set(x, y, z, s)
			}
		}
	}
	plant := simple(supportsVegetation)
	dry := simple(supportsDryVegetation)
	onGrassBlock := simple(func(s uint32) bool { return s >= grassBlockLo && s <= grassBlockHi })
	notPodzol := simple(func(s uint32) bool { return supportsVegetation(s) && !(s >= gcPodzolLo && s <= gcPodzolHi) })

	// patch is origins × (rarity, origin, tries).
	patch := func(origins, chance int, biomes map[string]bool, tries, xz, dy int,
		state func(int, int, int) uint32, place func(int, int, int, uint32)) {
		for i := 0; i < origins; i++ {
			if !rarity(chance) {
				continue
			}
			if x, y, z, ok := origin(biomes); ok {
				g.coverTries(r, reg, bg, x, y, z, tries, xz, dy, state, place)
			}
		}
	}
	lowNoise := g.flowerNoiseBelow(ox, oz) // noise_threshold_count at the chunk's corner
	threshold := func(below, above int) int {
		if lowNoise {
			return below
		}
		return above
	}

	// The flowers.
	patch(1, 32, gcFlowerDefault, 64, 7, 3, weighted(Poppy, 2, Dandelion, 1), plant)
	patch(threshold(15, 4), 32, gcFlowerPlains, 64, 6, 2, plainFlower, plant)
	patch(1, 32, gcFlowerSwamp, 64, 6, 2, fixed(BlueOrchid), plant)
	patch(1, 16, gcFlowerWarm, 64, 7, 3, weighted(Poppy, 2, Dandelion, 1), plant)
	patch(1, 1, gcFlowerMeadow, 96, 6, 2, meadowState, plant)
	patch(threshold(5, 10), 1, gcFlowerCherry, 96, 6, 2, uniform(gcPinkPetals), plant)
	patch(3, 2, gcFlowerFlowerForest, 96, 6, 2, flowerForestState, plant)
	// flower_forest_flowers (0-3 of one pick) and forest_flowers (one in
	// five a patch): rarity 7, one origin, the clamped count, then per copy
	// the selector's pick and 96 tries.
	forestFlowers := func(biomes map[string]bool, lo, hi int) {
		if !rarity(7) {
			return
		}
		x, y, z, ok := origin(biomes)
		n := min(max(lo+r.Intn(hi-lo+1), 0), hi)
		if !ok {
			return
		}
		for i := 0; i < n; i++ {
			pick := gcForestFlowerStates[r.Intn(len(gcForestFlowerStates))]
			g.coverTries(r, reg, bg, x, y, z, 96, 7, 3, fixed(pick), plant)
		}
	}
	forestFlowers(gcFlowerFlowerForest, -1, 3)
	forestFlowers(gcForestFlowers, -3, 1)
	// flower_pale_garden: one closed eyeblossom at the origin, one chunk in
	// thirty-two; pale_garden_flowers: a patch of them, one in eight.
	if rarity(32) {
		if x, y, z, ok := origin(gcPaleGarden); ok && reg.inChunk(x, z) && reg.read(x, y, z) == Air && !bg.decorationBlocked(x, y, z) {
			plant(x, y, z, ClosedEyeblossom)
		}
	}
	patch(1, 8, gcPaleGarden, 96, 7, 3, fixed(ClosedEyeblossom), plant)

	// The grass.
	patch(1, 1, gcGrassBadlands, 32, 7, 3, fixed(ShortGrass), plant)
	patch(2, 1, gcGrassForest, 32, 7, 3, fixed(ShortGrass), plant)
	patch(25, 1, gcGrassJungle, 32, 7, 3, weighted(ShortGrass, 3, Fern, 1), notPodzol)
	patch(threshold(5, 10), 1, gcGrassMeadow, 16, 7, 3, fixed(ShortGrass), plant)
	patch(5, 1, gcGrassNormal, 32, 7, 3, fixed(ShortGrass), plant)
	patch(threshold(5, 10), 1, gcGrassPlain, 32, 7, 3, fixed(ShortGrass), plant)
	patch(20, 1, gcGrassSavanna, 32, 7, 3, fixed(ShortGrass), plant)
	patch(7, 1, gcGrassTaiga, 32, 7, 3, weighted(ShortGrass, 1, Fern, 4), plant)
	patch(1, 1, gcGrassTaiga2, 32, 7, 3, weighted(ShortGrass, 1, Fern, 4), plant)
	patch(1, 1, gcDappledForest, 8, 7, 3, fixed(RedShrub), plant) // patch_red_shrub

	// Dead bushes, cacti and berry bushes.
	patch(1, 1, gcDeadBush, 4, 7, 3, fixed(DeadBush), dry)
	patch(2, 1, gcDesert, 4, 7, 3, fixed(DeadBush), dry)
	patch(20, 1, gcBadlands, 4, 7, 3, fixed(DeadBush), dry)
	g.cactusPatch(r, reg, bg, rarity(6), origin, gcDesert)
	g.cactusPatch(r, reg, bg, rarity(13), origin, gcBadlands)
	patch(1, 32, gcBerryCommon, 96, 7, 3, fixed(gcBerryBush), onGrassBlock)
	patch(1, 384, gcBerryRare, 96, 7, 3, fixed(gcBerryBush), onGrassBlock)

	// The mushrooms the old scatter drew: the mushroom fields' (brown one
	// chunk in four, red one in 256) and the dappled forest's (one in two).
	mushrooms := func(chance int, biomes map[string]bool, m uint32) {
		if !rarity(chance) {
			return
		}
		x, y, z, ok := origin(biomes)
		if !ok {
			return
		}
		g.mushroomPatch(r, reg.read, func(x, y, z int, s uint32) {
			if reg.inChunk(x, z) && !bg.decorationBlocked(x, y, z) && mushroomHolds(reg, x, y, z) {
				reg.set(x, y, z, s)
			}
		}, x, y, z, m, false)
	}
	mushrooms(4, gcMushroomField, BrownMushroom)
	mushrooms(256, gcMushroomField, RedMushroom)
	mushrooms(2, gcDappledForest, BrownMushroom)

	// Bamboo. bamboo_vegetation first: 30 origins (31 one chunk in ten) on
	// dry ground; its fancy oak, jungle bush and mega jungle slots are the
	// tree pass's, its default a jungle grass patch.
	origins := 30
	if r.Intn(10) == 9 {
		origins = 31
	}
	for i := 0; i < origins; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		c := reg.col(x, z)
		if c.h < SeaLevel || !gcBambooJungle[c.biome.Name] { // surface_water_depth_filter(0), OCEAN_FLOOR
			continue
		}
		if r.Float64() < 0.05 || r.Float64() < 0.15 || r.Float64() < 0.7 {
			continue // a tree slot
		}
		g.coverTries(r, reg, bg, x, c.h, z, 32, 7, 3, weighted(ShortGrass, 3, Fern, 1), notPodzol)
	}
	// bamboo (bamboo_some_podzol): BIOME_INFO_NOISE-counted stalks in the
	// bamboo jungle; bamboo_light (bamboo_no_podzol): one chunk in four of
	// the jungle.
	for i, n := 0, int(ceilPos((g.biomeInfoNoise(float64(ox)/80, float64(oz)/80)+0.3)*160)); i < n; i++ {
		if x, y, z, ok := origin(gcBambooJungle); ok {
			g.bambooAt(r, reg, bg, x, y, z, 0.2)
		}
	}
	if rarity(4) {
		if x, y, z, ok := origin(gcJungle); ok {
			g.bambooAt(r, reg, bg, x, y, z, 0)
		}
	}
}

// ceilPos is math.Ceil for the count placements, never below zero.
func ceilPos(v float64) float64 {
	if v <= 0 {
		return 0
	}
	n := float64(int64(v))
	if n < v {
		n++
	}
	return n
}

// cactusPatch is patch_cactus_desert / _decorated after its rarity roll:
// ten tries (7, 3) about one origin, each a block_column of one to three
// cacti with a flower on one in four, where the cell is air and a cactus
// would survive; the column is cut short (the flower first) where the
// cells above meet anything but air.
func (g *Generator) cactusPatch(r TreeRNG, reg *owRegion, bg *buildGuard, hit bool,
	origin func(map[string]bool) (int, int, int, bool), biomes map[string]bool) {
	if !hit {
		return
	}
	x, y, z, ok := origin(biomes)
	if !ok {
		return
	}
	for i := 0; i < 10; i++ {
		px, py, pz := x+triangleOffset(r, 7), y+triangleOffset(r, 3), z+triangleOffset(r, 7)
		cactus := 1 + r.Intn(r.Intn(3)+1) // biased_to_bottom(1, 3)
		flower := 0
		if r.Intn(4) == 3 { // weighted_list {0: 3, 1: 1}
			flower = 1
		}
		if !reg.inChunk(px, pz) || reg.read(px, py, pz) != Air || !cactusSurvives(reg, px, py, pz) ||
			bg.decorationBlocked(px, py, pz) {
			continue
		}
		total := cactus + flower
		for k := 1; k <= total; k++ { // BlockColumnFeature tests the cells over the origin
			if reg.read(px, py+k, pz) != Air {
				cut := total - (k - 1)
				d := min(flower, cut)
				flower -= d
				cactus -= cut - d
				break
			}
		}
		for k := 0; k < cactus; k++ {
			reg.set(px, py+k, pz, Cactus) // age 0
		}
		for k := 0; k < flower; k++ {
			reg.set(px, py+cactus+k, pz, cactusFlower)
		}
	}
}

// bambooAt is BambooFeature at an origin: a stalk of five to sixteen
// through air on ground that holds bamboo, its top three cells leafy, and
// with the given chance a podzol disc of radius one to four under it.
// Whether the origin holds is asked of the terrain model, so every chunk
// pass that sees the disc agrees; the stalk stands in its own chunk only.
func (g *Generator) bambooAt(r TreeRNG, reg *owRegion, bg *buildGuard, x, y, z int, podzolChance float64) {
	height := r.Intn(12) + 5
	rad := 0
	if r.Float64() < podzolChance {
		rad = r.Intn(4) + 1
	}
	// BambooStalkBlock.canSurvive: the cell under the origin, as the terrain
	// has it (a cave can open the surface under the column's top block).
	c := reg.col(x, z)
	if y != c.h || !supportsBamboo(g.terrainCell(c, x, y-1, z)) {
		return
	}
	for xx := x - rad; xx <= x+rad; xx++ {
		for zz := z - rad; zz <= z+rad; zz++ {
			dx, dz := xx-x, zz-z
			if dx*dx+dz*dz > rad*rad || !reg.inChunk(xx, zz) {
				continue
			}
			gy := reg.col(xx, zz).h - 1
			if IsDirtTag(reg.read(xx, gy, zz)) && !bg.decorationBlocked(xx, gy+1, zz) {
				reg.set(xx, gy, zz, podzolState)
			}
		}
	}
	if !reg.inChunk(x, z) || reg.read(x, y, z) != Air || bg.decorationBlocked(x, y, z) {
		return
	}
	placed := 0
	for placed < height && reg.read(x, y+placed, z) == Air {
		reg.set(x, y+placed, z, gcBambooTrunk)
		placed++
	}
	if placed >= 3 {
		if reg.read(x, y+placed, z) == Air {
			reg.set(x, y+placed, z, gcBambooFinalLarge)
		}
		reg.set(x, y+placed-1, z, gcBambooTopLarge)
		reg.set(x, y+placed-2, z, gcBambooTopSmall)
	}
}

// supportsBamboo is #supports_bamboo: sand, the overworld substrate,
// bamboo, bamboo saplings and gravel.
func supportsBamboo(s uint32) bool {
	return IsDirtTag(s) || s >= gcSandLo && s <= gcSandHi || s >= gcRedSandLo && s <= gcRedSandHi ||
		s >= gcGravelLo && s <= gcGravelHi || s >= gcBambooLo && s <= gcBambooHi || s >= gcSaplingLo && s <= gcSaplingHi
}

// mushroomHolds is MushroomBlock.canSurvive at worldgen: mycelium or
// podzol hold a mushroom anywhere; any other floor only in the dark,
// which tachyne reads as cover overhead (as mushroomPatch does).
func mushroomHolds(reg *owRegion, x, y, z int) bool {
	below := reg.read(x, y-1, z)
	if below >= gcMyceliumLo && below <= gcMyceliumHi || below >= gcPodzolLo && below <= gcPodzolHi {
		return true
	}
	covered := 0
	for dy := 1; dy <= 16 && covered < 3; dy++ {
		if reg.read(x, y+dy, z) != Air {
			covered++
		}
	}
	return covered >= 3
}
