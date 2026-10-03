package worldgen

// The seams of the vanilla world generator (GeneratorMode vanilla): the
// types its four parts build against, so each can be written on its own
// and wired together by the terrain core.
//
//   - the terrain core (the noise router, the noise-filled chunk, the
//     surface rules) implements VanillaClimate and VanillaTerrain;
//   - the biome source implements VanillaBiomes on a VanillaClimate;
//   - the placement pass implements VanillaPlacement on VanillaBiomes and
//     VanillaTerrain;
//   - the Nether and End implement VanillaDimension.
//
// A part registers its constructor from an init() in its own file
// (RegisterVanillaBiomes, RegisterVanillaPlacement, RegisterVanillaDimension),
// so no part edits another's file. Until one is registered the terrain core
// stands in for it (the engine's own biome lookup, its own decoration).
//
// These declarations are fixed: change them only by agreement.

// GeneratorMode picks a world's generator: the engine's own (native) or
// vanilla 26.3's. It is chosen when a world is made and never changes
// after (it shapes every block). Vanilla implies vanilla caves.
type GeneratorMode uint8

const (
	GeneratorNative  GeneratorMode = iota // the engine's own terrain (every world made before the choice existed)
	GeneratorVanilla                      // 26.3's noise router, surface rules, biomes and placement
)

func (m GeneratorMode) String() string {
	if m == GeneratorVanilla {
		return "vanilla"
	}
	return "native"
}

// ParseGeneratorMode reads "native" or "vanilla".
func ParseGeneratorMode(s string) (GeneratorMode, bool) {
	switch s {
	case "native":
		return GeneratorNative, true
	case "vanilla":
		return GeneratorVanilla, true
	}
	return GeneratorNative, false
}

// WorldPreset is a vanilla world preset (worldgen/world_preset): which
// noise settings and biome source the overworld uses. Only meaningful in
// vanilla mode.
type WorldPreset uint8

const (
	PresetNormal          WorldPreset = iota // minecraft:normal
	PresetLargeBiomes                        // minecraft:large_biomes (the *_large climate noises)
	PresetAmplified                          // minecraft:amplified (amplified noise settings)
	PresetSingleBiome                        // minecraft:single_biome_surface (a fixed biome)
	PresetCaves                              // noise settings minecraft:caves (a roofed overworld)
	PresetFloatingIslands                    // noise settings minecraft:floating_islands
	PresetFlat                               // minecraft:flat (FlatLevelSource)
)

var presetNames = [...]string{"normal", "large_biomes", "amplified", "single_biome_surface", "caves", "floating_islands", "flat"}

func (p WorldPreset) String() string {
	if int(p) < len(presetNames) {
		return presetNames[p]
	}
	return "normal"
}

// ParseWorldPreset reads a preset name (with or without "minecraft:").
func ParseWorldPreset(s string) (WorldPreset, bool) {
	if len(s) > 10 && s[:10] == "minecraft:" {
		s = s[10:]
	}
	for i, n := range presetNames {
		if n == s {
			return WorldPreset(i), true
		}
	}
	return PresetNormal, false
}

// Dimension names a vanilla dimension.
type Dimension uint8

const (
	DimOverworld Dimension = iota
	DimNether
	DimEnd
)

// ClimatePoint is Climate.TargetPoint: the six climate parameters at a
// position, each quantized as Climate.quantizeCoord does —
// (long)(value * 10000.0F), the float product truncated toward zero.
type ClimatePoint struct {
	Temperature, Humidity, Continentalness, Erosion, Depth, Weirdness int64
}

// QuantizeClimate is Climate.quantizeCoord.
func QuantizeClimate(v float32) int64 { return int64(v * 10000) }

// VanillaClimate is Climate.Sampler: the noise router's temperature,
// vegetation (humidity), continents, erosion, depth and ridges
// (weirdness) density functions, sampled at QUART coordinates (block>>2,
// as BiomeSource.getNoiseBiome is called) — it evaluates at the quart's
// minimum corner, QuartPos.toBlock of each coordinate. Safe for
// concurrent use.
type VanillaClimate interface {
	Sample(qx, qy, qz int) ClimatePoint
}

// VanillaBiomes is a dimension's BiomeSource in vanilla mode: the noise
// biome at QUART coordinates (block>>2), as getNoiseBiome — the biome the
// generator itself reads (surface rules, features, structure checks).
// Names are "minecraft:…". The client-visible per-quart biome stored in a
// chunk is the same noise biome at the quart (vanilla stores noise biomes;
// BiomeManager's fuzzed zoom is applied when a block position is asked).
// Safe for concurrent use.
type VanillaBiomes interface {
	BiomeAt(qx, qy, qz int) string
}

// HeightmapType is Heightmap.Types.
type HeightmapType uint8

const (
	HeightWorldSurfaceWG         HeightmapType = iota // WORLD_SURFACE_WG: not air, terrain before features
	HeightWorldSurface                                // WORLD_SURFACE: not air
	HeightOceanFloorWG                                // OCEAN_FLOOR_WG: motion-blocking material, terrain before features
	HeightOceanFloor                                  // OCEAN_FLOOR: motion-blocking material
	HeightMotionBlocking                              // MOTION_BLOCKING: motion-blocking or a fluid
	HeightMotionBlockingNoLeaves                      // MOTION_BLOCKING_NO_LEAVES
)

// VanillaTerrain is the vanilla-mode terrain before features (noise fill,
// aquifer, surface rules, carvers) at any position, in or out of the chunk
// being decorated — what placement and structure starts read of the land
// around them. Safe for concurrent use.
type VanillaTerrain interface {
	// BlockAt is the generated block state, before features.
	BlockAt(x, y, z int) uint32
	// Height is Heightmap.getFirstAvailable for the WG types: one above
	// the highest block matching kind in the column, MinY if none. Only
	// HeightWorldSurfaceWG and HeightOceanFloorWG are defined here (the
	// others depend on features; read them from the chunk itself).
	Height(kind HeightmapType, x, z int) int
	// MinY and Ceiling are the dimension's build limits (Ceiling exclusive).
	MinY() int
	Ceiling() int
	// SeaLevel is the noise settings' sea_level.
	SeaLevel() int
}

// VanillaPlacement is the placement pass in vanilla mode: the eleven
// GenerationStep.Decoration steps (and structure pieces) for one chunk,
// written into ch — a chunk whose terrain is filled and whose biomes are
// set. Features from neighbouring chunks that reach into ch are its job
// (the engine generates a chunk alone, never a 3×3 region). Safe for
// concurrent use on different chunks.
type VanillaPlacement interface {
	Decorate(ch *Chunk, cx, cz int32)
}

// TerrainAdjustment is a structure's terrain_adaptation (the Beardifier's
// kinds).
type TerrainAdjustment uint8

const (
	AdjustNone TerrainAdjustment = iota
	AdjustBury
	AdjustBeardThin
	AdjustBeardBox
	AdjustEncapsulate
)

// VanillaBeardPiece is a structure piece the terrain shapes itself to
// (Beardifier.Rigid): its bounding box (inclusive), its structure's
// adjustment, and the ground level delta of a rigid jigsaw piece (0 for
// any other piece).
type VanillaBeardPiece struct {
	MinX, MinY, MinZ, MaxX, MaxY, MaxZ int
	Adjust                             TerrainAdjustment
	GroundLevelDelta                   int
}

// VanillaJunction is a jigsaw junction the terrain is bearded to.
type VanillaJunction struct{ X, GroundY, Z int }

// VanillaBeards is what a placement pass that knows its structure starts
// gives the noise stage (optional; checked with a type assertion): for a
// chunk, the pieces and junctions Beardifier.forStructuresInChunk collects
// — the pieces of every start with an adjustment that are within 12 blocks
// of the chunk (rigid pool pieces and every non-pool piece), and the
// junctions of its pool pieces within 12 of the chunk's columns.
type VanillaBeards interface {
	BeardsFor(cx, cz int32) ([]VanillaBeardPiece, []VanillaJunction)
}

// VanillaGenContext is what a vanilla part is built from.
type VanillaGenContext struct {
	Seed    int64
	Dim     Dimension
	Preset  WorldPreset
	Climate VanillaClimate // nil outside the multi-noise dimensions
	Terrain VanillaTerrain
	Biomes  VanillaBiomes // nil when building the biome source itself
	// Gen is the owning generator (edit lookups, ceiling); parts may read it.
	Gen *Generator
}

// VanillaDimension generates a whole vanilla-mode dimension (the Nether,
// the End) chunk by chunk.
type VanillaDimension interface {
	GenerateChunk(cx, cz int32) *Chunk
	// BlockAt is the generated block before features.
	BlockAt(x, y, z int) uint32
	// SurfaceY is a safe spawn height for a column.
	SurfaceY(x, z int) float64
}

var (
	vanillaBiomesCtor    func(VanillaGenContext) VanillaBiomes
	vanillaPlacementCtor func(VanillaGenContext) VanillaPlacement
	vanillaDimensionCtor = map[Dimension]func(VanillaGenContext) VanillaDimension{}
)

// RegisterVanillaBiomes installs the biome source's constructor (call it
// from an init). It is called once per generator, for every dimension,
// with Biomes nil; a nil result means "not this dimension".
func RegisterVanillaBiomes(f func(VanillaGenContext) VanillaBiomes) { vanillaBiomesCtor = f }

// RegisterVanillaPlacement installs the placement pass's constructor
// (call it from an init). A nil result means "not this dimension".
func RegisterVanillaPlacement(f func(VanillaGenContext) VanillaPlacement) { vanillaPlacementCtor = f }

// RegisterVanillaDimension installs a whole-dimension generator for the
// Nether or the End (call it from an init).
func RegisterVanillaDimension(d Dimension, f func(VanillaGenContext) VanillaDimension) {
	vanillaDimensionCtor[d] = f
}
