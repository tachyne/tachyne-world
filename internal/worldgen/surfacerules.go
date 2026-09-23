package worldgen

// Overworld surface rules — SurfaceRuleData.overworld, the part that picks
// each column's floor block and what lies under it. The peaks get packed
// ice, ice and snow blocks with stone on steep faces, the slopes and grove
// pockets of powder snow, the stony peaks calcite bands, the stony shore
// gravel, the windswept hills bare stone where the surface noise runs high
// and the gravelly hills a gravel/stone/grass mix, the windswept savanna
// coarse dirt, the old-growth taigas podzol with coarse dirt, the badlands
// their terracotta bands above y=74 and red sand below, the wooded
// badlands coarse dirt and grass on their tops, and the swamps water
// puddles at y=62. Snowy plains and taiga are grass now, as vanilla's:
// their snow is the freeze pass's layer.

type surfaceNoises struct {
	surface, powder, packedIce, ice, calcite, gravel, swamp *Perlin
	frozenA, frozenB                                        *Perlin // the frozen oceans' open-water patches
	smallPatch                                              *Perlin // SMALL_PATCH (26.3): the dappled forest's coarse dirt
}

func newSurfaceNoises(seed int64) *surfaceNoises {
	return &surfaceNoises{
		surface:    NewPerlin(seed ^ 0x5F01),
		powder:     NewPerlin(seed ^ 0x5F02),
		packedIce:  NewPerlin(seed ^ 0x5F03),
		ice:        NewPerlin(seed ^ 0x5F04),
		calcite:    NewPerlin(seed ^ 0x5F05),
		gravel:     NewPerlin(seed ^ 0x5F06),
		swamp:      NewPerlin(seed ^ 0x5F07),
		frozenA:    NewPerlin(seed ^ 0x5F08),
		frozenB:    NewPerlin(seed ^ 0x5F09),
		smallPatch: NewPerlin(seed ^ 0x5F0A),
	}
}

// The noises at vanilla's octaves (NoiseData), the engine's Perlin FBm
// normalised to about ±1. surfaceNoiseAbove(t) is SURFACE ≥ t/8.25.
func (n *surfaceNoises) surfaceAbove(x, z int, t float64) bool {
	return n.surface.FBm(float64(x)/64, float64(z)/64, 3, 2, 1)/2.1 >= t/8.25
}
func (n *surfaceNoises) surfaceIn(x, z int, lo, hi float64) bool {
	v := n.surface.FBm(float64(x)/64, float64(z)/64, 3, 2, 1) / 2.1
	return v >= lo && v < hi
}
func (n *surfaceNoises) powderIn(x, z int, lo, hi float64) bool { // POWDER_SNOW -6, four octaves
	v := n.powder.FBm(float64(x)/64, float64(z)/64, 4, 2, 1) / 2.6
	return v >= lo && v < hi
}
func (n *surfaceNoises) packedIceIn(x, z int, lo, hi float64) bool { // PACKED_ICE -7
	v := n.packedIce.FBm(float64(x)/128, float64(z)/128, 4, 2, 1) / 2.6
	return v >= lo && v < hi
}
func (n *surfaceNoises) iceIn(x, z int, lo, hi float64) bool { // ICE -4
	v := n.ice.FBm(float64(x)/16, float64(z)/16, 4, 2, 1) / 2.6
	return v >= lo && v < hi
}
func (n *surfaceNoises) calciteIn(x, z int, lo, hi float64) bool { // CALCITE -9
	v := n.calcite.FBm(float64(x)/512, float64(z)/512, 4, 2, 1) / 2.6
	return v >= lo && v < hi
}
func (n *surfaceNoises) gravelIn(x, z int, lo, hi float64) bool { // GRAVEL -8
	v := n.gravel.FBm(float64(x)/256, float64(z)/256, 4, 2, 1) / 2.6
	return v >= lo && v < hi
}

// smallPatchAbove is SMALL_PATCH ≥ t: one octave at -3 with amplitude 3, so
// vanilla's normal noise spans about ±6.7 where this one spans ±1.
func (n *surfaceNoises) smallPatchAbove(x, z int, t float64) bool {
	return n.smallPatch.Noise2(float64(x)/8, float64(z)/8) >= t/6.67
}
func (n *surfaceNoises) swampAbove(x, z int, t float64) bool { // SWAMP -2
	return n.swamp.Noise2(float64(x)/4, float64(z)/4) > t
}

var Calcite = blockBase("calcite")

// surface is a column's picked floor block and the block under it; a
// badlands column bands by height instead.
type surface struct {
	top, under uint32
	badlands   bool
}

// steep is SurfaceRules.steep: the ground climbs four or more from north to
// south, or from east to west, across the column.
func (g *Generator) steep(x, z int) bool {
	if g.Height(x, z+1) >= g.Height(x, z-1)+4 {
		return true
	}
	return g.Height(x-1, z) >= g.Height(x+1, z)+4
}

// surfaceFor picks the floor and under-floor blocks of a column of height h
// (its top block sits at h-1) in biome b.
func (g *Generator) surfaceFor(b *Biome, x, z, h int) surface {
	n := g.surfN
	if n == nil {
		return surface{top: b.Top, under: b.Sub}
	}
	top := h - 1
	aboveWater := h >= SeaLevel      // waterBlockCheck(0, 0): no water over the floor
	notUnderwater := h >= SeaLevel-1 // waterBlockCheck(-1, 0)
	notUnderDeep := h >= SeaLevel-6  // waterStartCheck(-6, -1)
	grassOrDirt := Dirt
	if aboveWater {
		grassOrDirt = GrassBlock
	}
	s := surface{top: grassOrDirt, under: Dirt}
	switch b.Name {
	case "minecraft:frozen_peaks":
		switch {
		case g.steep(x, z):
			s.top, s.under = PackedIce, PackedIce
		case n.packedIceIn(x, z, 0, 0.2):
			s.top = PackedIce
		case n.iceIn(x, z, 0, 0.025):
			s.top = Ice
		case aboveWater:
			s.top = SnowBlock
		default:
			s.top = Stone
		}
		switch {
		case s.under == PackedIce:
		case n.packedIceIn(x, z, -0.5, 0.2):
			s.under = PackedIce
		case n.iceIn(x, z, -0.0625, 0.025):
			s.under = Ice
		case aboveWater:
			s.under = SnowBlock
		default:
			s.under = Stone
		}
	case "minecraft:snowy_slopes":
		switch {
		case g.steep(x, z):
			s.top, s.under = Stone, Stone
		case n.powderIn(x, z, 0.35, 0.6) && aboveWater:
			s.top, s.under = PowderSnow, SnowBlock
		case aboveWater:
			s.top, s.under = SnowBlock, SnowBlock
		default:
			s.top, s.under = Stone, Dirt
		}
		if s.top != Stone && n.powderIn(x, z, 0.45, 0.58) && aboveWater {
			s.under = PowderSnow
		}
	case "minecraft:jagged_peaks":
		s.under = Stone
		if g.steep(x, z) || !aboveWater {
			s.top = Stone
		} else {
			s.top = SnowBlock
		}
	case "minecraft:grove":
		switch {
		case n.powderIn(x, z, 0.35, 0.6) && aboveWater:
			s.top = PowderSnow
		case aboveWater:
			s.top = SnowBlock
		}
		if n.powderIn(x, z, 0.45, 0.58) && aboveWater {
			s.under = PowderSnow
		}
	case "minecraft:stony_peaks":
		s.top, s.under = Stone, Stone
		if n.calciteIn(x, z, -0.0125, 0.0125) {
			s.top, s.under = Calcite, Calcite
		}
	case "minecraft:stony_shore":
		s.top, s.under = Stone, Stone
		if n.gravelIn(x, z, -0.05, 0.05) {
			s.top, s.under = Gravel, Gravel
		}
	case "minecraft:windswept_hills":
		if n.surfaceAbove(x, z, 1.0) {
			s.top, s.under = Stone, Stone
		}
	case "minecraft:windswept_savanna":
		switch {
		case n.surfaceAbove(x, z, 1.75):
			s.top, s.under = Stone, Stone
		case n.surfaceAbove(x, z, -0.5):
			s.top = CoarseDirt
		}
	case "minecraft:windswept_gravelly_hills":
		switch {
		case n.surfaceAbove(x, z, 2.0):
			s.top, s.under = Gravel, Gravel
		case n.surfaceAbove(x, z, 1.0):
			s.top, s.under = Stone, Stone
		case n.surfaceAbove(x, z, -1.0):
			s.top, s.under = grassOrDirt, Dirt
		default:
			s.top, s.under = Gravel, Gravel
		}
	case "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga":
		switch {
		case n.surfaceAbove(x, z, 1.75):
			s.top = CoarseDirt
		case n.surfaceAbove(x, z, -0.95):
			s.top = Podzol
		}
	case "minecraft:dappled_forest": // 26.3: coarse dirt in small patches
		if n.smallPatchAbove(x, z, 1.2) {
			s.top = CoarseDirt
		}
	case "minecraft:ice_spikes":
		if aboveWater {
			s.top = SnowBlock
		}
	case "minecraft:mangrove_swamp":
		s.top, s.under = Mud, Mud
		if top == 60 && !notUnderwater && n.swampAbove(x, z, 0) { // the puddles
			s.top = Water
		}
	case "minecraft:swamp":
		if top == 62 && n.swampAbove(x, z, 0) {
			s.top = Water
		}
	case "minecraft:mushroom_fields":
		s.top = Mycelium
	case "minecraft:desert":
		s.top, s.under = Sand, Sand
	case "minecraft:beach", "minecraft:snowy_beach", "minecraft:warm_ocean":
		s.top, s.under = Sand, Sand
	case "minecraft:lukewarm_ocean", "minecraft:deep_lukewarm_ocean":
		s.top, s.under = Sand, Sand
	case "minecraft:dripstone_caves":
		s.top, s.under = Stone, Stone
	case "minecraft:badlands", "minecraft:eroded_badlands", "minecraft:wooded_badlands":
		s.badlands = true
		switch {
		case b.Name == "minecraft:wooded_badlands" && top >= 97:
			if n.surfaceIn(x, z, -0.909, -0.5454) || n.surfaceIn(x, z, -0.1818, 0.1818) || n.surfaceIn(x, z, 0.5454, 0.909) {
				s.top = CoarseDirt
			} else {
				s.top = grassOrDirt
			}
		case top >= 256:
			s.top = OrangeTerracotta
		case top >= 74: // badlandsMid: the bands reach the surface
			if n.surfaceIn(x, z, -0.909, -0.5454) || n.surfaceIn(x, z, -0.1818, 0.1818) || n.surfaceIn(x, z, 0.5454, 0.909) {
				s.top = Terracotta
			} else {
				s.top = badlandsBand(top)
			}
		case notUnderwater:
			s.top = RedSand
		case notUnderDeep:
			s.top = WhiteTerracotta
		default:
			s.top = Gravel
		}
	default:
		// Under water the floor keeps the biome's own material — but gravel is
		// vanilla's DEEP fallback, not its shallow one. SurfaceRules asks
		// waterStartCheck(-6,-1) first: a floor within six blocks of the
		// surface — a river bed, a lake bottom, the shelf off a beach — is
		// grass-or-dirt, and only below that does it turn to gravel. Sand
		// biomes (the warm oceans, the beaches) name their floor outright and
		// never reach the check.
		//
		// Measured against a real 1.21.11 world rather than argued from the
		// rule tree: of 92,062 open-water columns at sea level, 61,011 sat on
		// gravel, 9,481 on DIRT and 359 on clay. Dirt under water is real, and
		// it is what the clay disks need to land on.
		if !aboveWater && b.Top != GrassBlock {
			if b.Top == Gravel && notUnderDeep {
				break // shallow: s.top is already Dirt
			}
			s.top, s.under = b.Top, b.Sub
		}
	}
	return s
}

// badlandsUnder is the badlands' under-floor: above y=62 the bands (or
// orange terracotta below the mid line), lower white terracotta, deeper
// still gravel.
func badlandsUnder(y, h int) uint32 {
	top := h - 1
	switch {
	case y >= 62 && top >= 74:
		return badlandsBand(y)
	case y >= 63:
		return OrangeTerracotta
	case y >= 62:
		return badlandsBand(y)
	case h >= SeaLevel-6:
		return WhiteTerracotta
	}
	return Gravel
}
