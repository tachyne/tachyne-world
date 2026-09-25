package worldgen

import "math/rand"

// Ore generation: deterministic per-chunk veins so mining has a progression
// ladder (stone → iron → diamond). Veins are small random walks that replace
// only stone/deepslate, placed after carving so cave walls expose ore. Bands
// and vein sizes approximate vanilla 1.18+ distribution (uniform within the
// band; iron peaks mid-band, diamond ramps toward bedrock).

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

// infestedBiomes are where ore_infested generates (BiomeDefaultFeatures
// .addInfestedStone: the windswept hills, meadow and cherry grove, stony
// peaks, snowy slopes and grove).
var infestedBiomes = map[string]bool{
	"minecraft:windswept_hills": true, "minecraft:windswept_forest": true,
	"minecraft:windswept_gravelly_hills": true, "minecraft:meadow": true,
	"minecraft:cherry_grove": true, "minecraft:stony_peaks": true,
	"minecraft:snowy_slopes": true, "minecraft:grove": true,
}

// mountainBiomes are where emerald ore generates (vanilla adds ore_emerald only
// to the mountain-family biomes).
var mountainBiomes = map[string]bool{
	"minecraft:windswept_hills": true, "minecraft:windswept_forest": true,
	"minecraft:windswept_gravelly_hills": true, "minecraft:meadow": true,
	"minecraft:grove": true, "minecraft:snowy_slopes": true,
	"minecraft:frozen_peaks": true, "minecraft:jagged_peaks": true,
	"minecraft:stony_peaks": true, "minecraft:cherry_grove": true,
}

// oreSpec is one ore type's distribution: veins per chunk, blocks per vein, and
// the world-y band it spawns in. shape biases the y roll.
type oreSpec struct {
	stone, deepslate uint32
	attempts, size   int
	minY, maxY       int
	shape            int             // 0 uniform, 1 triangular (peak mid-band), 2 ramp-to-bottom
	rarity           int             // 0 = every chunk; else one blob in `rarity` chunks
	biomes           map[string]bool // nil = any biome; else only these (emerald)
}

// Bands/counts/sizes are the vanilla 1.21 ore placed_features (above_bottom
// anchors resolved against MinY=-64; trapezoid ≈ our triangular shape 1).
var oreSpecs = []oreSpec{
	{CoalOre, DeepslateCoalOre, 14, 10, 0, 110, 0, 0, nil},
	{CopperOre, DeepslateCopperOre, 8, 8, -16, 80, 1, 0, nil},
	{IronOre, DeepslateIronOre, 10, 7, -56, 64, 1, 0, nil},
	{GoldOre, DeepslateGoldOre, 4, 6, -60, 28, 0, 0, nil},
	{DiamondOre, DeepslateDiamondOre, 5, 5, -60, 12, 2, 0, nil},
	{RedstoneOre, DeepslateRedstoneOre, 4, 8, -64, 15, 0, 0, nil}, // ore_redstone: uniform -64..15
	{RedstoneOre, DeepslateRedstoneOre, 8, 8, -96, 32, 1, 0, nil}, // ore_redstone_lower: trapezoid, peak ~-32
	{LapisOre, DeepslateLapisOre, 2, 7, -32, 32, 1, 0, nil},       // ore_lapis: trapezoid, peak 0
	{LapisOre, DeepslateLapisOre, 4, 7, -64, 64, 0, 0, nil},       // ore_lapis_buried: uniform -64..64
	// ore_emerald: mountains only, many attempts at single blocks over a tall
	// trapezoid — most land in air above the surface and place nothing (vanilla
	// sparsity). Range trimmed to the in-world portion.
	{EmeraldOre, DeepslateEmeraldOre, 100, 1, -16, 256, 1, 0, mountainBiomes},
	// ore_infested: silverfish stone, 14 veins of 9 from the bottom to y=63.
	{InfestedStone, InfestedDeepslate, 14, 9, -64, 63, 0, 0, infestedBiomes},
	// The stone-variant blobs, which vanilla places in every overworld biome
	// and which give a cave wall its granite, diorite, andesite and tuff:
	// the upper three one chunk in six between 64 and 128, the lower three
	// twice a chunk from 0 to 60, tuff twice a chunk below 0. Size 64 in
	// vanilla — the engine's vein walk is shorter-legged than its ellipsoid,
	// so these read as pockets rather than the wide bands vanilla draws.
	{stoneGranite, stoneGranite, 1, 64, 64, 128, 0, 6, nil},
	{stoneDiorite, stoneDiorite, 1, 64, 64, 128, 0, 6, nil},
	{stoneAndesite, stoneAndesite, 1, 64, 64, 128, 0, 6, nil},
	{stoneGranite, stoneGranite, 2, 64, 0, 60, 0, 0, nil},
	{stoneDiorite, stoneDiorite, 2, 64, 0, 60, 0, 0, nil},
	{stoneAndesite, stoneAndesite, 2, 64, 0, 60, 0, 0, nil},
	{stoneTuff, stoneTuff, 2, 64, -64, 0, 0, 0, nil},
}

// placeOres stamps this chunk's ore veins. Deterministic: the RNG derives from
// the world seed + chunk coords, so the same chunk always generates the same
// veins (required — Block() point reads and chunk packets must agree).
func (g *Generator) placeOres(ch *Chunk, cx, cz int32) {
	blobs := &blobGuard{g: g, cx: cx, cz: cz}
	g.placeBlobs(ch, cx, cz, earthBlobs, blobs)      // ore_dirt, ore_gravel: first in the ore step
	defer g.placeBlobs(ch, cx, cz, clayBlobs, blobs) // ore_clay: after the ores, as in the lush caves' list
	rng := rand.New(rand.NewSource(oreSeed(g.seed, cx, cz)))
	for _, spec := range oreSpecs {
		// Biome-gated ores (emerald) generate only where the chunk sits in an
		// eligible biome; sampled once at the chunk centre.
		if spec.biomes != nil && !spec.biomes[g.resolveBiome(int(cx)*16+8, int(cz)*16+8).Name] {
			continue
		}
		if spec.rarity > 0 && rng.Intn(spec.rarity) != 0 {
			continue // rarity_filter: most chunks skip this blob entirely
		}
		for a := 0; a < spec.attempts; a++ {
			lx, lz := rng.Intn(16), rng.Intn(16)
			// Clamp the origin: bands that dip below the world floor (redstone's
			// trapezoid reaches -96) just concentrate near bedrock, as vanilla's
			// below-floor samples effectively do.
			y := clampInt(rollY(rng, spec), MinY+1, MinY+len(ch.Sections)*16-1)
			for i := 0; i < spec.size; i++ {
				setOre(ch, lx, y, lz, spec)
				// Random-walk one step, staying inside the chunk column bounds.
				switch rng.Intn(3) {
				case 0:
					lx = clampInt(lx+rng.Intn(3)-1, 0, 15)
				case 1:
					lz = clampInt(lz+rng.Intn(3)-1, 0, 15)
				default:
					y = clampInt(y+rng.Intn(3)-1, MinY+1, MinY+len(ch.Sections)*16-1)
				}
			}
		}
	}
}

// rollY picks a vein origin height within the spec's band, per its shape.
func rollY(rng *rand.Rand, spec oreSpec) int {
	span := spec.maxY - spec.minY + 1
	switch spec.shape {
	case 1: // triangular: two rolls averaged peak mid-band (iron/copper)
		return spec.minY + (rng.Intn(span)+rng.Intn(span))/2
	case 2: // ramp to bottom: the lower roll wins (diamond richer near bedrock)
		a, b := rng.Intn(span), rng.Intn(span)
		if b < a {
			a = b
		}
		return spec.minY + a
	default:
		return spec.minY + rng.Intn(span)
	}
}

// setOre replaces a stone/deepslate cell with the matching ore variant. Air,
// water, dirt and everything else is left alone, so veins never float in caves
// or stick out of the surface.
func setOre(ch *Chunk, lx, y, lz int, spec oreSpec) {
	yi := y - MinY
	sec, idx := yi/16, ((yi%16)*16+lz)*16+lx
	switch ch.Sections[sec][idx] {
	case Stone:
		ch.Sections[sec][idx] = spec.stone
	case Deepslate:
		ch.Sections[sec][idx] = spec.deepslate
	}
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

// Soil blobs: ore_dirt (7 a chunk, 0–160) and ore_gravel (14 a chunk, the
// whole height) in every overworld biome, and ore_clay (46 a chunk from the
// bottom to 256) where the cave biome is the lush caves. Size 33, into
// the base stone. They walk as the ore veins do, but on their own seeds,
// so the ores' layout is unchanged; dirt and gravel go in before the ores,
// clay after, as in vanilla's ore step.
//
// Build guard: a blob with a player's build in the box around it (two
// blocks out) is not placed — a dirt pocket would otherwise replace the
// stone of a wall or floor a player dug into.

// blobSpec is one soil blob feature.
type blobSpec struct {
	block       uint32
	count, size int
	minY, maxY  int    // maxY 0 = the top of the world
	caveBiome   string // "" = any biome; else only where the cave biome is this
	salt        int64
}

var (
	earthBlobs = []blobSpec{
		{block: Dirt, count: 7, size: 33, minY: 0, maxY: 160, salt: 0xB10B_D1},
		{block: Gravel, count: 14, size: 33, minY: MinY, salt: 0xB10B_67},
	}
	clayBlobs = []blobSpec{
		{block: Clay, count: 46, size: 33, minY: MinY, maxY: 256, caveBiome: "minecraft:lush_caves", salt: 0xB10B_C1},
	}
)

const blobGuardMargin = 2

// blobGuard holds the build blocks around a chunk, read once for all its
// blobs.
type blobGuard struct {
	g      *Generator
	cx, cz int32
	read   bool
	builds [][3]int
}

// builtIn reports whether a build block lies in the box (inclusive).
func (bg *blobGuard) builtIn(x0, y0, z0, x1, y1, z1 int) bool {
	if bg.g.editsIn == nil {
		return false
	}
	if !bg.read {
		bg.read = true
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				bg.g.editsIn(bg.cx+dx, bg.cz+dz, func(x, y, z int, s uint32) {
					if isBuildBlock(s) {
						bg.builds = append(bg.builds, [3]int{x, y, z})
					}
				})
			}
		}
	}
	for _, p := range bg.builds {
		if p[0] >= x0 && p[0] <= x1 && p[1] >= y0 && p[1] <= y1 && p[2] >= z0 && p[2] <= z1 {
			return true
		}
	}
	return false
}

// placeBlobs walks this chunk's blobs of each spec.
func (g *Generator) placeBlobs(ch *Chunk, cx, cz int32, specs []blobSpec, guard *blobGuard) {
	top := MinY + len(ch.Sections)*16 - 1
	cells := make([][3]int, 0, 64)
	for _, spec := range specs {
		rng := rand.New(rand.NewSource(oreSeed(g.seed^spec.salt, cx, cz)))
		maxY := spec.maxY
		if maxY == 0 || maxY > top {
			maxY = top
		}
		for a := 0; a < spec.count; a++ {
			lx, lz := rng.Intn(16), rng.Intn(16)
			y := spec.minY + rng.Intn(maxY-spec.minY+1)
			if spec.caveBiome != "" && g.caveBiomeAt(int(cx)*16+lx, y, int(cz)*16+lz) != spec.caveBiome {
				continue
			}
			y = clampInt(y, MinY+1, top)
			cells = cells[:0]
			x0, y0, z0, x1, y1, z1 := lx, y, lz, lx, y, lz
			for i := 0; i < spec.size; i++ {
				cells = append(cells, [3]int{lx, y, lz})
				x0, y0, z0 = min(x0, lx), min(y0, y), min(z0, lz)
				x1, y1, z1 = max(x1, lx), max(y1, y), max(z1, lz)
				switch rng.Intn(3) {
				case 0:
					lx = clampInt(lx+rng.Intn(3)-1, 0, 15)
				case 1:
					lz = clampInt(lz+rng.Intn(3)-1, 0, 15)
				default:
					y = clampInt(y+rng.Intn(3)-1, MinY+1, top)
				}
			}
			bx, bz := int(cx)*16, int(cz)*16
			if guard.builtIn(bx+x0-blobGuardMargin, y0-blobGuardMargin, bz+z0-blobGuardMargin,
				bx+x1+blobGuardMargin, y1+blobGuardMargin, bz+z1+blobGuardMargin) {
				continue
			}
			for _, c := range cells {
				yi := c[1] - MinY
				sec, idx := yi/16, ((yi%16)*16+c[2])*16+c[0]
				if baseStoneOverworld(ch.Sections[sec][idx]) {
					ch.Sections[sec][idx] = spec.block
				}
			}
		}
	}
}

// baseStoneOverworld is the #base_stone_overworld tag: what a soil blob
// replaces.
func baseStoneOverworld(s uint32) bool {
	switch s {
	case Stone, Deepslate, stoneGranite, stoneDiorite, stoneAndesite, stoneTuff:
		return true
	}
	return false
}
