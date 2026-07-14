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
)

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
	biomes           map[string]bool // nil = any biome; else only these (emerald)
}

// Bands/counts/sizes are the vanilla 1.21 ore placed_features (above_bottom
// anchors resolved against MinY=-64; trapezoid ≈ our triangular shape 1).
var oreSpecs = []oreSpec{
	{CoalOre, DeepslateCoalOre, 14, 10, 0, 110, 0, nil},
	{CopperOre, DeepslateCopperOre, 8, 8, -16, 80, 1, nil},
	{IronOre, DeepslateIronOre, 10, 7, -56, 64, 1, nil},
	{GoldOre, DeepslateGoldOre, 4, 6, -60, 28, 0, nil},
	{DiamondOre, DeepslateDiamondOre, 5, 5, -60, 12, 2, nil},
	{RedstoneOre, DeepslateRedstoneOre, 4, 8, -64, 15, 0, nil}, // ore_redstone: uniform -64..15
	{RedstoneOre, DeepslateRedstoneOre, 8, 8, -96, 32, 1, nil}, // ore_redstone_lower: trapezoid, peak ~-32
	{LapisOre, DeepslateLapisOre, 2, 7, -32, 32, 1, nil},       // ore_lapis: trapezoid, peak 0
	{LapisOre, DeepslateLapisOre, 4, 7, -64, 64, 0, nil},       // ore_lapis_buried: uniform -64..64
	// ore_emerald: mountains only, many attempts at single blocks over a tall
	// trapezoid — most land in air above the surface and place nothing (vanilla
	// sparsity). Range trimmed to the in-world portion.
	{EmeraldOre, DeepslateEmeraldOre, 100, 1, -16, 256, 1, mountainBiomes},
}

// placeOres stamps this chunk's ore veins. Deterministic: the RNG derives from
// the world seed + chunk coords, so the same chunk always generates the same
// veins (required — Block() point reads and chunk packets must agree).
func (g *Generator) placeOres(ch *Chunk, cx, cz int32) {
	rng := rand.New(rand.NewSource(oreSeed(g.seed, cx, cz)))
	for _, spec := range oreSpecs {
		// Biome-gated ores (emerald) generate only where the chunk sits in an
		// eligible biome; sampled once at the chunk centre.
		if spec.biomes != nil && !spec.biomes[g.resolveBiome(int(cx)*16+8, int(cz)*16+8).Name] {
			continue
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
