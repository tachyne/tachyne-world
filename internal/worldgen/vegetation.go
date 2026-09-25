package worldgen

// The surface patches of vanilla's vegetal-decoration step that the ground
// cover pass (stampGroundCover) never drew: the two-block tall grass, large
// ferns and sunflowers, lily pads on the swamps' water, leaf litter on the
// dark forest floor, bushes, firefly bushes by the water, dry grass in the
// deserts and badlands, wildflowers in the birch forests and meadows, the
// pale garden's moss patches, and the vines on the jungles' trees and walls.
//
// Each is 26.3's placed feature: an origin drawn in the chunk (after a
// rarity or count), the surface height there, the biome's own list, and
// then random_patch's tries — a triangle offset about the origin, the plant
// only where the cell is air and the block under it holds that plant.
//
// A patch reaches up to seven blocks past its chunk, so every chunk replays
// its eight neighbours' draws and keeps the cells that land inside it (the
// same scheme as the cave features). Every try's randomness is drawn whole,
// whatever the world says at its cell, so the replays in different chunks
// walk the same stream; the world only decides whether a drawn plant is
// written. Positions are tachyne's, not a vanilla seed's — the counts,
// spreads, predicates and biomes are vanilla's.
//
// Every plant goes through the chunk pass's build guard (buildguard.go):
// none grows on a player's floor or under a player's roof.

// Biome lists, from each placed feature's entries in the 26.3 biome files.
// The cave biomes that also carry patch_tall_grass_2 are left out: tachyne's
// cave biomes never reach the surface these patches stand on.
var (
	vegTallGrass  = biomeSet("savanna", "savanna_plateau")
	vegTallGrass2 = biomeSet("cherry_grove", "meadow", "plains", "sunflower_plains")
	vegLargeFern  = biomeSet("old_growth_pine_taiga", "old_growth_spruce_taiga", "snowy_taiga", "taiga")
	vegSunflower  = biomeSet("sunflower_plains")
	vegWaterlily  = biomeSet("mangrove_swamp", "swamp")
	vegLeafLitter = biomeSet("dark_forest")
	vegBush       = biomeSet("birch_forest", "forest", "frozen_river", "old_growth_birch_forest", "plains",
		"river", "windswept_forest", "windswept_gravelly_hills", "windswept_hills")
	vegFireflySwamp   = biomeSet("swamp")
	vegFireflyByWater = biomeSet("badlands", "bamboo_jungle", "beach", "birch_forest", "cold_ocean",
		"dark_forest", "deep_cold_ocean", "deep_frozen_ocean", "deep_lukewarm_ocean", "deep_ocean",
		"eroded_badlands", "flower_forest", "forest", "frozen_ocean", "frozen_river", "ice_spikes",
		"jungle", "lukewarm_ocean", "mangrove_swamp", "mushroom_fields", "ocean", "old_growth_birch_forest",
		"old_growth_pine_taiga", "old_growth_spruce_taiga", "pale_garden", "plains", "river", "savanna",
		"savanna_plateau", "snowy_beach", "snowy_plains", "snowy_taiga", "sparse_jungle", "stony_shore",
		"sunflower_plains", "taiga", "warm_ocean", "windswept_forest", "windswept_gravelly_hills",
		"windswept_hills", "windswept_savanna", "wooded_badlands")
	vegDryGrassBadlands  = biomeSet("badlands", "eroded_badlands", "wooded_badlands")
	vegDryGrassDesert    = biomeSet("desert")
	vegWildflowersBirch  = biomeSet("birch_forest", "old_growth_birch_forest")
	vegWildflowersMeadow = biomeSet("meadow")
	vegPaleMoss          = biomeSet("pale_garden")
	vegVines             = biomeSet("bamboo_jungle", "jungle", "sparse_jungle")
)

// The states the patches place.
var (
	tallGrassHalves = [2]uint32{withProps("tall_grass", "half", "lower"), withProps("tall_grass", "half", "upper")}
	largeFernHalves = [2]uint32{withProps("large_fern", "half", "lower"), withProps("large_fern", "half", "upper")}
	sunflowerHalves = [2]uint32{withProps("sunflower", "half", "lower"), withProps("sunflower", "half", "upper")}
	bushState       = blockID("bush")
	fireflyBush     = blockID("firefly_bush")
	dryGrassStates  = [2]uint32{blockID("short_dry_grass"), blockID("tall_dry_grass")}
	// The weighted providers, every entry weight one: leaf litter facing
	// each way with one to three segments, wildflowers with one to four.
	leafLitterStates           = segmentStates("leaf_litter", "segment_amount", 3)
	wildflowerStates           = segmentStates("wildflowers", "flower_amount", 4)
	farmlandLo, farmlandHi     = BlockRange("farmland")
	grassBlockLo, grassBlockHi = BlockRange("grass_block")
)

// segmentStates lists a segmentable block's states for each facing and
// amount 1..n.
func segmentStates(name, prop string, n int) []uint32 {
	var out []uint32
	for a := 1; a <= n; a++ {
		for _, f := range [4]string{"north", "east", "south", "west"} {
			out = append(out, withProps(name, "facing", f, prop, itoaSmall(a)))
		}
	}
	return out
}

// supportsVegetation is #supports_vegetation — VegetationBlock.mayPlaceOn:
// the overworld substrate (#dirt, #mud, #moss_blocks, #grass_blocks, which
// IsDirtTag spans) and farmland.
func supportsVegetation(s uint32) bool {
	return IsDirtTag(s) || s >= farmlandLo && s <= farmlandHi
}

// supportsDryVegetation is #supports_dry_vegetation: #sand, #terracotta and
// #supports_vegetation — where short and tall dry grass root.
func supportsDryVegetation(s uint32) bool {
	return supportsVegetation(s) || dryVegetationGround[s]
}

// dryVegetationGround is #sand and #terracotta, every state.
var dryVegetationGround = func() map[uint32]bool {
	m := map[uint32]bool{}
	for _, n := range []string{"sand", "red_sand", "suspicious_sand", "terracotta", "white_terracotta",
		"orange_terracotta", "magenta_terracotta", "light_blue_terracotta", "yellow_terracotta",
		"lime_terracotta", "pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta",
		"purple_terracotta", "blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta",
		"black_terracotta"} {
		lo, hi := BlockRange(n)
		for st := lo; st <= hi; st++ {
			m[st] = true
		}
	}
	return m
}()

// flowerNoiseBelow is the NoiseThresholdCountPlacement test at a chunk's
// corner. Vanilla samples a dedicated simplex at x/200, z/200 and takes
// the lower count below -0.8, which about one chunk in fifty is; tachyne's
// Perlin spreads differently, so the threshold is set where the same share
// of chunks falls below it (measured: -0.58 is its 2% quantile).
func (g *Generator) flowerNoiseBelow(x, z int) bool {
	return g.detail.Noise2(float64(x)/200+0.43, float64(z)/200+0.71) < flowerNoiseThreshold
}

const flowerNoiseThreshold = -0.58

// overworldVegetation replays chunk (ncx, ncz)'s vegetation patches into
// the region's chunk, in the vegetal step's order.
func (g *Generator) overworldVegetation(reg *owRegion, bg *buildGuard, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^0x7E6E7A, ox, oz)
	// origin is in_square, the heightmap and the biome filter. Every
	// heightmap these use counts water, so over water the origin is the
	// first cell above the sea.
	origin := func(biomes map[string]bool) (x, y, z int, ok bool) {
		x, z = ox+r.Intn(16), oz+r.Intn(16)
		c := reg.col(x, z)
		y = c.h
		if y < SeaLevel {
			y = SeaLevel
		}
		return x, y, z, biomes[c.biome.Name]
	}
	// rarityPatch is rarity_filter(chance) and one patch.
	rarityPatch := func(chance int, biomes map[string]bool, tries, xz, dy int, plant func(x, y, z, pick int), picks int) {
		if r.Intn(chance) != 0 {
			return
		}
		if x, y, z, ok := origin(biomes); ok {
			g.scatterPatch(r, reg, bg, x, y, z, tries, xz, dy, picks, plant)
		}
	}
	double := func(halves [2]uint32) func(x, y, z, _ int) {
		return func(x, y, z, _ int) { g.plantDouble(reg, bg, x, y, z, halves) }
	}
	single := func(states []uint32, soil func(uint32) bool) func(x, y, z, pick int) {
		return func(x, y, z, pick int) {
			if soil(reg.read(x, y-1, z)) {
				reg.set(x, y, z, states[pick])
			}
		}
	}
	// patch_tall_grass (the savannas), patch_tall_grass_2 (the plains and
	// meadows: seven origins at one in thirty-two, none in the noise's low
	// patches) and patch_large_fern (the taigas).
	rarityPatch(5, vegTallGrass, 96, 7, 3, double(tallGrassHalves), 1)
	if !g.flowerNoiseBelow(ox, oz) {
		for i := 0; i < 7; i++ {
			rarityPatch(32, vegTallGrass2, 96, 7, 3, double(tallGrassHalves), 1)
		}
	}
	rarityPatch(5, vegLargeFern, 96, 7, 3, double(largeFernHalves), 1)
	// wildflowers_birch_forest: three origins, each at even odds.
	for i := 0; i < 3; i++ {
		rarityPatch(2, vegWildflowersBirch, 64, 6, 2, single(wildflowerStates, supportsVegetation), len(wildflowerStates))
	}
	// patch_sunflower, patch_bush, and the dry grass.
	rarityPatch(3, vegSunflower, 96, 7, 3, double(sunflowerHalves), 1)
	rarityPatch(4, vegBush, 24, 5, 3, single([]uint32{bushState}, supportsVegetation), 1)
	rarityPatch(6, vegDryGrassBadlands, 64, 7, 3, single(dryGrassStates[:], supportsDryVegetation), 2)
	rarityPatch(3, vegDryGrassDesert, 64, 7, 3, single(dryGrassStates[:], supportsDryVegetation), 2)
	// patch_waterlily: four origins on the swamp's surface, ten tries each,
	// a lily pad over still water (or ice).
	for i := 0; i < 4; i++ {
		if x, y, z, ok := origin(vegWaterlily); ok {
			g.scatterPatch(r, reg, bg, x, y, z, 10, 7, 3, 1, func(x, y, z, _ int) {
				if below := reg.read(x, y-1, z); IsFluidSource(below, WaterBase) || below == Ice || below == frostedIce {
					reg.set(x, y, z, LilyPad)
				}
			})
		}
	}
	// wildflowers_meadow: ten origins (five in the noise's low patches),
	// eight tries each.
	meadowOrigins := 10
	if g.flowerNoiseBelow(ox, oz) {
		meadowOrigins = 5
	}
	for i := 0; i < meadowOrigins; i++ {
		if x, y, z, ok := origin(vegWildflowersMeadow); ok {
			g.scatterPatch(r, reg, bg, x, y, z, 8, 6, 2, len(wildflowerStates), single(wildflowerStates, supportsVegetation))
		}
	}
	// patch_leaf_litter: two origins, thirty-two tries on grass.
	for i := 0; i < 2; i++ {
		if x, y, z, ok := origin(vegLeafLitter); ok {
			g.scatterPatch(r, reg, bg, x, y, z, 32, 7, 3, len(leafLitterStates), func(x, y, z, pick int) {
				if s := reg.read(x, y-1, z); s >= grassBlockLo && s <= grassBlockHi {
					reg.set(x, y, z, leafLitterStates[pick])
				}
			})
		}
	}
	// pale_moss_patch: one a chunk in the pale garden.
	if x, y, z, ok := origin(vegPaleMoss); ok {
		g.paleMossGround(r, reg, bg, x, y, z)
	}
	// The firefly bushes: patch_firefly_bush_swamp, then the near-water
	// patches (patch_firefly_bush_near_water_swamp in the swamp, three
	// origins; patch_firefly_bush_near_water elsewhere, two), whose origin
	// must itself be a bush's spot with water beside the ground.
	rarityPatch(8, vegFireflySwamp, 20, 4, 3, single([]uint32{fireflyBush}, supportsVegetation), 1)
	for _, v := range [2]struct {
		biomes  map[string]bool
		origins int
	}{{vegFireflySwamp, 3}, {vegFireflyByWater, 2}} {
		for i := 0; i < v.origins; i++ {
			x, y, z, ok := origin(v.biomes)
			if !ok {
				continue
			}
			spot := reg.read(x, y, z) == Air && supportsVegetation(reg.read(x, y-1, z)) && waterBeside(reg, x, y-1, z)
			// The tries are drawn whether or not the origin holds, so the
			// stream does not hang on a read.
			g.scatterPatch(r, reg, bg, x, y, z, 20, 4, 3, 1, func(x, y, z, _ int) {
				if spot && supportsVegetation(reg.read(x, y-1, z)) {
					reg.set(x, y, z, fireflyBush)
				}
			})
		}
	}
	// vines: 127 cells between y 64 and 100 in the jungles, each a vine on
	// the first face it can hold to (VinesFeature: never from below). They
	// stay in their own chunk, so only the region's own chunk draws them.
	if ox == reg.baseX && oz == reg.baseZ && (vegVines[reg.col(ox, oz).biome.Name] || vegVines[reg.col(ox+15, oz+15).biome.Name] ||
		vegVines[reg.col(ox+15, oz).biome.Name] || vegVines[reg.col(ox, oz+15).biome.Name] || vegVines[reg.col(ox+8, oz+8).biome.Name]) {
		for i := 0; i < 127; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			y := 64 + r.Intn(37)
			if !vegVines[reg.col(x, z).biome.Name] || reg.read(x, y, z) != Air || bg.decorationBlocked(x, y, z) {
				continue
			}
			for i, f := range faceDirs6 {
				if f.d[1] < 0 {
					continue
				}
				if holdsFace(reg.read(x+f.d[0], y+f.d[1], z+f.d[2])) {
					reg.set(x, y, z, vineFaces[i])
					break
				}
			}
		}
	}
}

// vineFaces is a vine on each of faceDirs6's faces.
var vineFaces = func() (out [6]uint32) {
	for i, f := range faceDirs6 {
		if f.prop != "down" {
			out[i] = withProps("vine", f.prop, "true")
		}
	}
	return out
}()

var frostedIce = blockBase("frosted_ice")

// scatterPatch is random_patch's tail: tries cells at a triangle offset
// (xz, dy) about the origin, each with a pick among picks states drawn with
// it, and plant called on every one that lands in this chunk on an air cell
// the build guard allows.
func (g *Generator) scatterPatch(r TreeRNG, reg *owRegion, bg *buildGuard, x, y, z, tries, xz, dy, picks int, plant func(x, y, z, pick int)) {
	for i := 0; i < tries; i++ {
		px, py, pz := x+triangleOffset(r, xz), y+triangleOffset(r, dy), z+triangleOffset(r, xz)
		pick := 0
		if picks > 1 {
			pick = r.Intn(picks)
		}
		if lx, lz := px-reg.baseX, pz-reg.baseZ; lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
			continue // another chunk's cell: its own pass writes it
		}
		if reg.read(px, py, pz) != Air || bg.decorationBlocked(px, py, pz) {
			continue
		}
		plant(px, py, pz, pick)
	}
}

// plantDouble is SimpleBlockFeature for a double plant: the lower half on
// ground that holds it, the upper half in the air above — both or neither,
// and neither under a player's roof.
func (g *Generator) plantDouble(reg *owRegion, bg *buildGuard, x, y, z int, halves [2]uint32) {
	if !supportsVegetation(reg.read(x, y-1, z)) || reg.read(x, y+1, z) != Air || bg.decorationBlocked(x, y+1, z) {
		return
	}
	reg.set(x, y, z, halves[0])
	reg.set(x, y+1, z, halves[1])
}

// paleMossGround is the standalone pale_moss_patch: the pale oak's ground
// patch (treepalemoss.go), laid on the pale garden's floor by the region.
// Its draws are fixed whatever it finds, so it runs through the same
// decorator context the trees use, with the build guard on every write — a
// moss block replaces ground only where a plant could grow over it.
func (g *Generator) paleMossGround(r TreeRNG, reg *owRegion, bg *buildGuard, x, y, z int) {
	moss := blockBase("pale_moss_block")
	ctx := &decoCtx{
		rng:   r,
		isAir: func(p [3]int) bool { return reg.read(p[0], p[1], p[2]) == Air },
		read:  reg.read,
		set: func(x, y, z int, s uint32, _ bool) {
			// A moss block is ground: guard the cell a plant would take over
			// it. A plant is guarded from the cell its lowest half stands in,
			// both halves, so a tall grass or a carpet with its topper goes in
			// whole or not at all.
			base := y
			switch {
			case s == moss:
				base = y + 1
			case s == tallGrassHalves[1] || s >= paleCarpetTopper && s < paleCarpetTopper+81:
				base = y - 1
			}
			if bg.decorationBlocked(x, base, z) || s != moss && bg.decorationBlocked(x, base+1, z) {
				return
			}
			reg.set(x, y, z, s)
		},
	}
	paleMossPatch(ctx, x, y, z)
}

// paleCarpetTopper is the first pale moss carpet state with bottom=false —
// the topper layer paleCarpetAt lays over a base carpet.
var paleCarpetTopper = blockBase("pale_moss_carpet") + 81
