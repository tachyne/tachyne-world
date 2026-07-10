package worldgen

// 1.21.5 default block-state IDs (from PrismarineJS/minecraft-data blocks.json).
// These are the surface/terrain blocks the generator places.
var (
	Air        = blockBase("air")
	Stone      = blockBase("stone")
	GrassBlock = blockBase("grass_block") + 1
	Dirt       = blockBase("dirt")
	Bedrock    = blockBase("bedrock")
	Water      = blockBase("water")
	Sand       = blockBase("sand")
	Gravel     = blockBase("gravel")
	Sandstone  = blockBase("sandstone")
	SnowBlock  = blockBase("snow_block")
	Deepslate  = blockBase("deepslate") + 1

	// Decoration (trees + ground cover).
	OakLog     = blockBase("oak_log") + 1
	OakLeaves  = blockBase("oak_leaves") + 27
	ShortGrass = blockBase("short_grass")
	Fern       = blockBase("fern")
	Dandelion  = blockBase("dandelion")
	Poppy      = blockBase("poppy")

	// Biome surface blocks (default states, minecraft-data / Mojang datagen).
	CoarseDirt          = blockBase("coarse_dirt")
	Podzol              = blockBase("podzol") + 1
	Mycelium            = blockBase("mycelium") + 1
	RedSandstone        = blockBase("red_sandstone")
	PowderSnow          = blockBase("powder_snow")
	PackedIce           = blockBase("packed_ice")
	BlueIce             = blockBase("blue_ice")
	Ice                 = blockBase("ice")
	Mud                 = blockBase("mud")
	MangroveRoots       = blockBase("mangrove_roots") + 1
	MossBlock           = blockBase("moss_block")
	Clay                = blockBase("clay")
	Terracotta          = blockBase("terracotta")
	WhiteTerracotta     = blockBase("white_terracotta")
	OrangeTerracotta    = blockBase("orange_terracotta")
	YellowTerracotta    = blockBase("yellow_terracotta")
	BrownTerracotta     = blockBase("brown_terracotta")
	LightGrayTerracotta = blockBase("light_gray_terracotta")
	RedTerracotta       = blockBase("red_terracotta")

	// Per-biome tree logs + leaves.
	SpruceLog      = blockBase("spruce_log") + 1
	SpruceLeaves   = blockBase("spruce_leaves") + 27
	BirchLog       = blockBase("birch_log") + 1
	BirchLeaves    = blockBase("birch_leaves") + 27
	JungleLog      = blockBase("jungle_log") + 1
	JungleLeaves   = blockBase("jungle_leaves") + 27
	AcaciaLog      = blockBase("acacia_log") + 1
	AcaciaLeaves   = blockBase("acacia_leaves") + 27
	DarkOakLog     = blockBase("dark_oak_log") + 1
	DarkOakLeaves  = blockBase("dark_oak_leaves") + 27
	MangroveLog    = blockBase("mangrove_log") + 1
	MangroveLeaves = blockBase("mangrove_leaves") + 27
	CherryLog      = blockBase("cherry_log") + 1
	CherryLeaves   = blockBase("cherry_leaves") + 27

	// Biome flora (single-block placements).
	TallGrass      = blockBase("tall_grass") + 1 // lower half only (2-block plant)
	LargeFern      = blockBase("large_fern") + 1
	DeadBush       = blockBase("dead_bush")
	Cornflower     = blockBase("cornflower")
	AzureBluet     = blockBase("azure_bluet")
	OxeyeDaisy     = blockBase("oxeye_daisy")
	Allium         = blockBase("allium")
	BlueOrchid     = blockBase("blue_orchid")
	SugarCane      = blockBase("sugar_cane")
	Cactus         = blockBase("cactus")
	Bamboo         = blockBase("bamboo")
	LilyPad        = blockBase("lily_pad")
	BrownMushroom  = blockBase("brown_mushroom")
	RedMushroom    = blockBase("red_mushroom")
	SweetBerryBush = blockBase("sweet_berry_bush")
)

// Opaque is the opacity of a block that stops light entirely.
const Opaque = 15

// SkyOpacity reports how much a block dims sky light passing through it:
//
//	0       fully transparent — light passes undimmed (air, grass, flowers)
//	1       translucent — dims by one extra level (leaves, water)
//	Opaque  blocks light entirely (all solid terrain)
//
// Unknown states (e.g. blocks a player places) are treated as opaque, so the
// default is "casts a shadow". This classification is what makes caves and
// building interiors dark once real sky-light propagation runs.
func SkyOpacity(state uint32) int {
	// Generated from minecraft-data filterLight, so glass/doors/fences/slabs/
	// stairs/beds/plants correctly let light through instead of blocking it.
	return LightFilter(state)
}
