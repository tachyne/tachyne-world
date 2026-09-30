package worldgen

import "math"

// Ore generation: vanilla 26.3's ore step — every ore_* placed feature of
// the overworld (the soil and stone-variant blobs too), each an OreFeature
// ellipsoid of its configured size, with its count, rarity, height
// provider, biome filter and discard_chance_on_air_exposure. Placed after
// carving, so cave walls expose ore (or, for the buried ores, mostly do
// not).

// 1.21.5 ore block-state ids (minecraft-data blocks.json). Drops, hardness and
// pickaxe-tier gating for these come from the generated blocks.json tables.
var (
	CoalOre             = blockBase("coal_ore")
	DeepslateCoalOre    = blockBase("deepslate_coal_ore")
	IronOre             = blockBase("iron_ore")
	DeepslateIronOre    = blockBase("deepslate_iron_ore")
	CopperOre           = blockBase("copper_ore")
	DeepslateCopperOre  = blockBase("deepslate_copper_ore")
	GoldOre             = blockBase("gold_ore")
	DeepslateGoldOre    = blockBase("deepslate_gold_ore")
	DiamondOre          = blockBase("diamond_ore")
	DeepslateDiamondOre = blockBase("deepslate_diamond_ore")
	// Redstone ore carries a `lit` property; generate the UNLIT/default state
	// (lit=false is the base+1 id — Minecraft orders booleans true-then-false).
	RedstoneOre          = blockBase("redstone_ore") + 1
	DeepslateRedstoneOre = blockBase("deepslate_redstone_ore") + 1
	LapisOre             = blockBase("lapis_ore")
	DeepslateLapisOre    = blockBase("deepslate_lapis_ore")
	EmeraldOre           = blockBase("emerald_ore")
	DeepslateEmeraldOre  = blockBase("deepslate_emerald_ore")
	InfestedStone        = blockBase("infested_stone")
	InfestedDeepslate    = blockBase("infested_deepslate")
)

// infestedBiomes and mountainBiomes are where ore_infested and ore_emerald
// generate: the same ten mountain biomes in 26.3's biome lists.
var (
	infestedBiomes = biomeSet("cherry_grove", "frozen_peaks", "grove", "jagged_peaks", "meadow",
		"snowy_slopes", "stony_peaks", "windswept_forest", "windswept_gravelly_hills", "windswept_hills")
	mountainBiomes = infestedBiomes
	badlandsBiomes = biomeSet("badlands", "eroded_badlands", "wooded_badlands")
)

// oreTarget is an ore's replacement rule.
type oreTarget int8

const (
	// oreTargetOres: #stone_ore_replaceables (stone, granite, diorite,
	// andesite) takes the stone ore and #deepslate_ore_replaceables the
	// deepslate ore; tuff (#height_specific_ore_replaceables) takes the
	// stone ore from y=0 up and the deepslate ore below.
	oreTargetOres oreTarget = iota
	// oreTargetBaseStone: #base_stone_overworld takes the one block.
	oreTargetBaseStone
)

// oreCfg is one configured ore feature.
type oreCfg struct {
	stone, deepslate uint32 // oreTargetBaseStone places stone
	target           oreTarget
	size             int
	discard          float64 // discard_chance_on_air_exposure
}

// replace is the configured targets, in order: what cell s at height y
// becomes, if the ore takes it.
func (c oreCfg) replace(s uint32, y int) (uint32, bool) {
	if c.target == oreTargetBaseStone {
		if baseStoneOverworld(s) {
			return c.stone, true
		}
		return 0, false
	}
	switch s {
	case stoneTuff:
		if y >= 0 {
			return c.stone, true
		}
		return c.deepslate, true
	case Stone, stoneGranite, stoneDiorite, stoneAndesite:
		return c.stone, true
	case Deepslate:
		return c.deepslate, true
	}
	return 0, false
}

func oreOf(stone, deepslate uint32, size int, discard float64) oreCfg {
	return oreCfg{stone: stone, deepslate: deepslate, target: oreTargetOres, size: size, discard: discard}
}

func blobOf(block uint32, size int) oreCfg {
	return oreCfg{stone: block, deepslate: block, target: oreTargetBaseStone, size: size}
}

// oreAnchor is a VerticalAnchor: absolute, above_bottom or below_top.
type oreAnchor struct {
	kind int8 // 0 absolute, 1 above_bottom, 2 below_top
	v    int
}

func oreAbs(v int) oreAnchor         { return oreAnchor{0, v} }
func oreAboveBottom(v int) oreAnchor { return oreAnchor{1, v} }
func oreBelowTop(v int) oreAnchor    { return oreAnchor{2, v} }

// resolve places the anchor in a world whose cells run MinY..top-1.
func (a oreAnchor) resolve(top int) int {
	switch a.kind {
	case 1:
		return MinY + a.v
	case 2:
		return top - 1 - a.v
	}
	return a.v
}

// oreHeight is height_range's provider: uniform or a plateau-less
// trapezoid between two anchors.
type oreHeight struct {
	trapezoid bool
	min, max  oreAnchor
}

func oreUniform(min, max oreAnchor) oreHeight   { return oreHeight{false, min, max} }
func oreTrapezoid(min, max oreAnchor) oreHeight { return oreHeight{true, min, max} }

// sample is UniformHeight / TrapezoidHeight (plateau 0): an empty range
// gives its minimum.
func (h oreHeight) sample(r TreeRNG, top int) int {
	lo, hi := h.min.resolve(top), h.max.resolve(top)
	if lo > hi {
		return lo
	}
	if !h.trapezoid {
		return lo + r.Intn(hi-lo+1)
	}
	span := hi - lo
	start := span / 2
	end := span - start
	return lo + r.Intn(end+1) + r.Intn(start+1)
}

// orePlacement is one ore_* placed feature: count (to countMax, when the
// count is a uniform range) or a rarity_filter chance, in_square, the
// height range, and the biome filter at the origin cell.
type orePlacement struct {
	name            string
	cfg             oreCfg
	count, countMax int
	rarity          int
	height          oreHeight
	biomes          map[string]bool // nil: every overworld biome
	notBiomes       map[string]bool // biomes that list another placement instead
	surface         bool            // biomes are surface biomes: asked of the origin's column
	salt            int64
}

// orePlacements is the ore step in 26.3's biome order: the soil and stone
// blobs, the ores, then the biome extras (ore_copper_large in the dripstone
// caves, ore_gold_extra in the badlands, ore_emerald in the mountains,
// ore_clay in the lush caves) and ore_infested, which is the next step's.
var orePlacements = []orePlacement{
	{name: "ore_dirt", cfg: blobOf(Dirt, 33), count: 7, height: oreUniform(oreAbs(0), oreAbs(160)), salt: 0x0E_01},
	{name: "ore_gravel", cfg: blobOf(Gravel, 33), count: 14, height: oreUniform(oreAboveBottom(0), oreBelowTop(0)), salt: 0x0E_02},
	{name: "ore_granite_upper", cfg: blobOf(stoneGranite, 64), rarity: 6, height: oreUniform(oreAbs(64), oreAbs(128)), salt: 0x0E_03},
	{name: "ore_granite_lower", cfg: blobOf(stoneGranite, 64), count: 2, height: oreUniform(oreAbs(0), oreAbs(60)), salt: 0x0E_04},
	{name: "ore_diorite_upper", cfg: blobOf(stoneDiorite, 64), rarity: 6, height: oreUniform(oreAbs(64), oreAbs(128)), salt: 0x0E_05},
	{name: "ore_diorite_lower", cfg: blobOf(stoneDiorite, 64), count: 2, height: oreUniform(oreAbs(0), oreAbs(60)), salt: 0x0E_06},
	{name: "ore_andesite_upper", cfg: blobOf(stoneAndesite, 64), rarity: 6, height: oreUniform(oreAbs(64), oreAbs(128)), salt: 0x0E_07},
	{name: "ore_andesite_lower", cfg: blobOf(stoneAndesite, 64), count: 2, height: oreUniform(oreAbs(0), oreAbs(60)), salt: 0x0E_08},
	{name: "ore_tuff", cfg: blobOf(stoneTuff, 64), count: 2, height: oreUniform(oreAboveBottom(0), oreAbs(0)), salt: 0x0E_09},
	{name: "ore_coal_upper", cfg: oreOf(CoalOre, DeepslateCoalOre, 17, 0), count: 30, height: oreUniform(oreAbs(136), oreBelowTop(0)), salt: 0x0E_0A},
	{name: "ore_coal_lower", cfg: oreOf(CoalOre, DeepslateCoalOre, 17, 0.5), count: 20, height: oreTrapezoid(oreAbs(0), oreAbs(192)), salt: 0x0E_0B},
	{name: "ore_iron_upper", cfg: oreOf(IronOre, DeepslateIronOre, 9, 0), count: 90, height: oreTrapezoid(oreAbs(80), oreAbs(384)), salt: 0x0E_0C},
	{name: "ore_iron_middle", cfg: oreOf(IronOre, DeepslateIronOre, 9, 0), count: 10, height: oreTrapezoid(oreAbs(-24), oreAbs(56)), salt: 0x0E_0D},
	{name: "ore_iron_small", cfg: oreOf(IronOre, DeepslateIronOre, 4, 0), count: 10, height: oreUniform(oreAboveBottom(0), oreAbs(72)), salt: 0x0E_0E},
	{name: "ore_gold", cfg: oreOf(GoldOre, DeepslateGoldOre, 9, 0.5), count: 4, height: oreTrapezoid(oreAbs(-64), oreAbs(32)), salt: 0x0E_0F},
	{name: "ore_gold_lower", cfg: oreOf(GoldOre, DeepslateGoldOre, 9, 0.5), count: 0, countMax: 1, height: oreUniform(oreAbs(-64), oreAbs(-48)), salt: 0x0E_10},
	{name: "ore_redstone", cfg: oreOf(RedstoneOre, DeepslateRedstoneOre, 8, 0), count: 4, height: oreUniform(oreAboveBottom(0), oreAbs(15)), salt: 0x0E_11},
	{name: "ore_redstone_lower", cfg: oreOf(RedstoneOre, DeepslateRedstoneOre, 8, 0), count: 8, height: oreTrapezoid(oreAboveBottom(-32), oreAboveBottom(32)), salt: 0x0E_12},
	{name: "ore_diamond", cfg: oreOf(DiamondOre, DeepslateDiamondOre, 4, 0.5), count: 7, height: oreTrapezoid(oreAboveBottom(-80), oreAboveBottom(80)), salt: 0x0E_13},
	{name: "ore_diamond_medium", cfg: oreOf(DiamondOre, DeepslateDiamondOre, 8, 0.5), count: 2, height: oreUniform(oreAbs(-64), oreAbs(-4)), salt: 0x0E_14},
	{name: "ore_diamond_large", cfg: oreOf(DiamondOre, DeepslateDiamondOre, 12, 0.7), rarity: 9, height: oreTrapezoid(oreAboveBottom(-80), oreAboveBottom(80)), salt: 0x0E_15},
	{name: "ore_diamond_buried", cfg: oreOf(DiamondOre, DeepslateDiamondOre, 8, 1.0), count: 4, height: oreTrapezoid(oreAboveBottom(-80), oreAboveBottom(80)), salt: 0x0E_16},
	{name: "ore_lapis", cfg: oreOf(LapisOre, DeepslateLapisOre, 7, 0), count: 2, height: oreTrapezoid(oreAbs(-32), oreAbs(32)), salt: 0x0E_17},
	{name: "ore_lapis_buried", cfg: oreOf(LapisOre, DeepslateLapisOre, 7, 1.0), count: 4, height: oreUniform(oreAboveBottom(0), oreAbs(64)), salt: 0x0E_18},
	{name: "ore_copper", cfg: oreOf(CopperOre, DeepslateCopperOre, 10, 0), count: 16, height: oreTrapezoid(oreAbs(-16), oreAbs(112)),
		notBiomes: biomeSet("dripstone_caves"), salt: 0x0E_19},
	{name: "ore_copper_large", cfg: oreOf(CopperOre, DeepslateCopperOre, 20, 0), count: 16, height: oreTrapezoid(oreAbs(-16), oreAbs(112)),
		biomes: biomeSet("dripstone_caves"), salt: 0x0E_1A},
	{name: "ore_gold_extra", cfg: oreOf(GoldOre, DeepslateGoldOre, 9, 0), count: 50, height: oreUniform(oreAbs(32), oreAbs(256)),
		biomes: badlandsBiomes, surface: true, salt: 0x0E_1B},
	{name: "ore_emerald", cfg: oreOf(EmeraldOre, DeepslateEmeraldOre, 3, 0), count: 100, height: oreTrapezoid(oreAbs(-16), oreAbs(480)),
		biomes: mountainBiomes, surface: true, salt: 0x0E_1C},
	{name: "ore_clay", cfg: blobOf(Clay, 33), count: 46, height: oreUniform(oreAboveBottom(0), oreAbs(256)),
		biomes: biomeSet("lush_caves"), salt: 0x0E_1D},
	{name: "ore_infested", cfg: oreOf(InfestedStone, InfestedDeepslate, 9, 0), count: 14, height: oreUniform(oreAboveBottom(0), oreAbs(63)),
		biomes: infestedBiomes, surface: true, salt: 0x0E_1E},
}

// oreGuardMargin is how far round an ore blob's box a build keeps it out.
const oreGuardMargin = 2

// placeOres runs the ore step into this chunk: every placement of the 3×3
// origin chunks (a blob reaches up to thirteen blocks from its origin), each
// origin chunk's placement on its own stream, so every chunk pass draws the
// same blobs and writes its own part of each.
//
// Build guard: a blob with a player's build in its box (two blocks out) is
// not placed — an ore pocket would otherwise replace the stone of a wall or
// floor a player dug into.
func (g *Generator) placeOres(ch *Chunk, cx, cz int32) {
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	guard := g.newBuildIndex(cx, cz, 2)
	top := MinY + len(ch.Sections)*16
	for i := range orePlacements {
		p := &orePlacements[i]
		for dcx := int32(-1); dcx <= 1; dcx++ {
			for dcz := int32(-1); dcz <= 1; dcz++ {
				g.runOrePlacement(reg, guard, p, int(cx+dcx)*16, int(cz+dcz)*16, top)
			}
		}
	}
}

// runOrePlacement draws origin chunk (ox, oz)'s blobs of one placement.
func (g *Generator) runOrePlacement(reg *owRegion, guard *buildIndex, p *orePlacement, ox, oz, top int) {
	if p.surface && !reg.chunkHasSurfaceBiome(ox, oz, p.biomes) {
		return
	}
	r := newTreeRNG(g.seed^p.salt, ox, oz)
	n := p.count
	if p.countMax > p.count {
		n += r.Intn(p.countMax - p.count + 1)
	}
	if p.rarity > 0 {
		if r.Float64() >= 1/float64(p.rarity) {
			return
		}
		n = 1
	}
	for a := 0; a < n; a++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := p.height.sample(r, top)
		if p.biomes != nil || p.notBiomes != nil {
			// A surface biome's ore asks the column's surface biome: the
			// engine calls every deep cell a cave biome, where vanilla's
			// mountains run on down under their caves.
			b := reg.col(x, z).biome.Name
			if !p.surface {
				b = reg.caveBiomeAt(x, y, z)
			}
			if (p.biomes != nil && !p.biomes[b]) || p.notBiomes[b] {
				continue
			}
		}
		key := uint64(int64(ox))*0x9E3779B97F4A7C15 ^ uint64(int64(oz))*0xC2B2AE3D27D4EB4F ^ uint64(a)*0x165667B19E3779F9
		oreBlob(reg, guard, p.cfg, r, x, y, z, top, g.seed^p.salt, key)
	}
}

// oreBlob is OreFeature.place for one origin: size spheres strung along a
// line of length size/4 through the origin at a random angle, each of a
// random radius swelling toward the middle, spheres swallowed by a bigger
// neighbour dropped, and every cell inside the rest tried once against the
// targets. A cell of an ore with discard_chance_on_air_exposure touching
// air is kept only when the discard roll fails (always at 0, never at 1).
// Only the cells inside this chunk are written; the draws are the same in
// every pass. seed and key make the per-cell discard roll.
func oreBlob(reg *owRegion, guard *buildIndex, cfg oreCfg, r TreeRNG, x, y, z, top int, seed int64, key uint64) {
	size := cfg.size
	dir := r.Float64() * math.Pi
	spread := float64(size) / 8
	maxR := int(math.Ceil((float64(size)/16*2 + 1) / 2))
	x0, x1 := float64(x)+math.Sin(dir)*spread, float64(x)-math.Sin(dir)*spread
	z0, z1 := float64(z)+math.Cos(dir)*spread, float64(z)-math.Cos(dir)*spread
	y0 := float64(y + r.Intn(3) - 2)
	y1 := float64(y + r.Intn(3) - 2)
	cs := int(math.Ceil(spread))
	xs, ys, zs := x-cs-maxR, y-2-maxR, z-cs-maxR
	sxz, sy := 2*(cs+maxR), 2*(2+maxR)
	data := make([]float64, size*4)
	for i := 0; i < size; i++ {
		step := float64(i) / float64(size)
		ss := r.Float64() * float64(size) / 16
		data[i*4] = x0 + (x1-x0)*step
		data[i*4+1] = y0 + (y1-y0)*step
		data[i*4+2] = z0 + (z1-z0)*step
		data[i*4+3] = ((math.Sin(math.Pi*step)+1)*ss + 1) / 2
	}
	if xs+sxz < reg.baseX || xs > reg.baseX+15 || zs+sxz < reg.baseZ || zs > reg.baseZ+15 ||
		ys+sy < MinY || ys >= top {
		return // nothing of it in this chunk or the world
	}
	if guard.builtIn(xs-oreGuardMargin, ys-oreGuardMargin, zs-oreGuardMargin,
		xs+sxz+oreGuardMargin, ys+sy+oreGuardMargin, zs+sxz+oreGuardMargin) {
		return
	}
	for i := 0; i < size-1; i++ {
		if data[i*4+3] <= 0 {
			continue
		}
		for j := i + 1; j < size; j++ {
			if data[j*4+3] <= 0 {
				continue
			}
			dx, dy, dz := data[i*4]-data[j*4], data[i*4+1]-data[j*4+1], data[i*4+2]-data[j*4+2]
			dr := data[i*4+3] - data[j*4+3]
			if dr*dr > dx*dx+dy*dy+dz*dz {
				if dr > 0 {
					data[j*4+3] = -1
				} else {
					data[i*4+3] = -1
				}
			}
		}
	}
	// tested is vanilla's bit set, with its index (which lets the box's far
	// faces alias cells a row on — a cell is tried at most once either way).
	tested := make([]bool, sxz+sy*sxz+sxz*sxz*sy+sxz*sy+sxz+1)
	for i := 0; i < size; i++ {
		rad := data[i*4+3]
		if rad < 0 {
			continue
		}
		xx, yy, zz := data[i*4], data[i*4+1], data[i*4+2]
		xMin := max(int(math.Floor(xx-rad)), xs)
		yMin := max(int(math.Floor(yy-rad)), ys)
		zMin := max(int(math.Floor(zz-rad)), zs)
		xMax := max(int(math.Floor(xx+rad)), xMin)
		yMax := max(int(math.Floor(yy+rad)), yMin)
		zMax := max(int(math.Floor(zz+rad)), zMin)
		for cx := xMin; cx <= xMax; cx++ {
			xd := (float64(cx) + 0.5 - xx) / rad
			if xd*xd >= 1 {
				continue
			}
			for cy := yMin; cy <= yMax; cy++ {
				yd := (float64(cy) + 0.5 - yy) / rad
				if xd*xd+yd*yd >= 1 {
					continue
				}
				for cz := zMin; cz <= zMax; cz++ {
					zd := (float64(cz) + 0.5 - zz) / rad
					if xd*xd+yd*yd+zd*zd >= 1 || cy < MinY || cy >= top {
						continue
					}
					bi := cx - xs + (cy-ys)*sxz + (cz-zs)*sxz*sy
					if bi < 0 || bi >= len(tested) || tested[bi] {
						continue
					}
					tested[bi] = true
					lx, lz := cx-reg.baseX, cz-reg.baseZ
					if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
						continue
					}
					to, ok := cfg.replace(sectionBlockAt(reg.ch, lx, cy, lz), cy)
					if !ok {
						continue
					}
					if cfg.discard > 0 && (cfg.discard >= 1 || hash01(seed, cx, cz, key^uint64(int64(cy))*0xD6E8FEB86659FD93) < cfg.discard) &&
						oreNextToAir(reg, cx, cy, cz) {
						continue // exposed, and the discard roll says so
					}
					setSectionBlock(reg.ch, lx, cy, lz, to, true)
				}
			}
		}
	}
}

// oreNextToAir is isAdjacentToAir: any of the six neighbours air (a
// neighbour in another chunk is read from its terrain).
func oreNextToAir(reg *owRegion, x, y, z int) bool {
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if reg.read(x+d[0], y+d[1], z+d[2]) == Air {
			return true
		}
	}
	return false
}

// oreSeed mixes the world seed and chunk coords (splitmix64-style) into a
// deterministic per-chunk RNG seed.
func oreSeed(seed int64, cx, cz int32) int64 {
	h := uint64(seed) ^ 0x9e3779b97f4a7c15
	for _, v := range [2]uint64{uint64(uint32(cx)), uint64(uint32(cz))} {
		h ^= v + 0x9e3779b97f4a7c15 + (h << 6) + (h >> 2)
		h *= 0xbf58476d1ce4e5b9
		h ^= h >> 27
	}
	return int64(h)
}

// buildIndex is the build blocks around a chunk, bucketed by 4×4×4 cell
// so a feature's box is a handful of map lookups however much is built.
type buildIndex struct {
	buckets map[[3]int][][3]int
}

// newBuildIndex reads the edits of the chunks within reach of (cx, cz).
// With no edit overlay it blocks nothing.
func (g *Generator) newBuildIndex(cx, cz, reach int32) *buildIndex {
	bi := &buildIndex{}
	if g.editsIn == nil {
		return bi
	}
	bi.buckets = map[[3]int][][3]int{}
	for dx := -reach; dx <= reach; dx++ {
		for dz := -reach; dz <= reach; dz++ {
			g.editsIn(cx+dx, cz+dz, func(x, y, z int, s uint32) {
				if isBuildBlock(s) {
					k := [3]int{x >> 2, y >> 2, z >> 2}
					bi.buckets[k] = append(bi.buckets[k], [3]int{x, y, z})
				}
			})
		}
	}
	return bi
}

// builtIn reports whether a build block lies in the box (inclusive).
func (bi *buildIndex) builtIn(x0, y0, z0, x1, y1, z1 int) bool {
	if bi == nil || len(bi.buckets) == 0 {
		return false
	}
	for bx := x0 >> 2; bx <= x1>>2; bx++ {
		for by := y0 >> 2; by <= y1>>2; by++ {
			for bz := z0 >> 2; bz <= z1>>2; bz++ {
				for _, p := range bi.buckets[[3]int{bx, by, bz}] {
					if p[0] >= x0 && p[0] <= x1 && p[1] >= y0 && p[1] <= y1 && p[2] >= z0 && p[2] <= z1 {
						return true
					}
				}
			}
		}
	}
	return false
}

// baseStoneOverworld is the #base_stone_overworld tag: what a soil or
// stone blob replaces.
func baseStoneOverworld(s uint32) bool {
	switch s {
	case Stone, Deepslate, stoneGranite, stoneDiorite, stoneAndesite, stoneTuff:
		return true
	}
	return false
}
