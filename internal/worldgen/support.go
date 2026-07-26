package worldgen

// Block support: what a block needs under, over or behind it to stay put.
//
// Vanilla answers this with a canSurvive override per block class — 60-odd
// distinct rules over ~300 block ids. Rather than a hand-written list that
// drifts (the previous one covered six blocks), every id is classified here
// into the small set of SHAPES those rules take, and the game layer applies
// the shape. A block gains support by joining a list, not by growing a new
// branch in the placement code.
//
// Blocks whose rule is genuinely bespoke — chorus plants growing off each
// other, scaffolding counting its distance to a support, vines climbing,
// bamboo stacking, fire, farmland — are deliberately NOT here; they keep
// their own logic (or their own gap) rather than being forced into a shape
// that would be wrong.

// SupportKind is the shape of a block's support requirement.
type SupportKind uint8

const (
	SupportNone       SupportKind = iota
	SupportFloor                  // a solid top face below (torches, rails, carpets, doors)
	SupportSoil                   // ground a plant will root in (flowers, saplings, cactus)
	SupportFarmland               // tilled farmland below (the crops)
	SupportWall                   // the block behind it, opposite its facing (wall torches, ladders)
	SupportCeiling                // a solid face above (hanging signs, spore blossom)
	SupportFace                   // its own "face" property picks floor, wall or ceiling (buttons, levers)
	SupportAttached               // the face it grew on, from its facing (amethyst, glow lichen)
	SupportHangable               // floor or ceiling, by its own "hanging" property (lanterns)
	SupportSpeleothem             // floor or ceiling, by "vertical_direction" (pointed dripstone)
	SupportWater                  // floating on a water surface (lily pads)
)

// supportNames is the classification. Names, not ids: ids move between
// versions, names do not.
var supportNames = map[SupportKind][]string{
	SupportAttached: {
		"amethyst_cluster", "glow_lichen", "large_amethyst_bud",
		"medium_amethyst_bud", "resin_clump", "sculk_vein", "small_amethyst_bud",
	},
	SupportCeiling: {
		"acacia_hanging_sign", "bamboo_hanging_sign", "birch_hanging_sign",
		"cherry_hanging_sign", "crimson_hanging_sign", "dark_oak_hanging_sign",
		"hanging_roots", "jungle_hanging_sign", "mangrove_hanging_sign",
		"oak_hanging_sign", "pale_hanging_moss", "pale_oak_hanging_sign",
		"spore_blossom", "spruce_hanging_sign", "warped_hanging_sign",
	},
	SupportFace: {
		"acacia_button", "bamboo_button", "birch_button", "cherry_button",
		"crimson_button", "dark_oak_button", "grindstone", "jungle_button",
		"lever", "mangrove_button", "oak_button", "pale_oak_button",
		"polished_blackstone_button", "spruce_button", "stone_button",
		"warped_button",
	},
	SupportFarmland: {
		"beetroots", "carrots", "pitcher_crop", "potatoes", "torchflower_crop",
		"wheat",
	},
	SupportFloor: {
		"acacia_door", "acacia_pressure_plate", "acacia_sign", "activator_rail",
		"bamboo_door", "bamboo_pressure_plate", "bamboo_sign", "big_dripleaf",
		"birch_door", "birch_pressure_plate", "birch_sign", "black_banner",
		"black_candle", "black_candle_cake", "black_carpet", "blue_banner",
		"blue_candle", "blue_candle_cake", "blue_carpet", "brain_coral",
		"brain_coral_fan", "brown_banner", "brown_candle", "brown_candle_cake",
		"brown_carpet", "brown_mushroom", "bubble_coral", "bubble_coral_fan",
		"cake", "candle", "candle_cake", "cherry_door", "cherry_pressure_plate",
		"cherry_sign", "comparator", "copper_torch", "crimson_door",
		"crimson_pressure_plate", "crimson_sign", "cyan_banner", "cyan_candle",
		"cyan_candle_cake", "cyan_carpet", "dark_oak_door",
		"dark_oak_pressure_plate", "dark_oak_sign", "dead_brain_coral",
		"dead_brain_coral_fan", "dead_bubble_coral", "dead_bubble_coral_fan",
		"dead_fire_coral", "dead_fire_coral_fan", "dead_horn_coral",
		"dead_horn_coral_fan", "dead_tube_coral", "dead_tube_coral_fan",
		"detector_rail", "fire_coral", "fire_coral_fan", "gray_banner",
		"gray_candle", "gray_candle_cake", "gray_carpet", "green_banner",
		"green_candle", "green_candle_cake", "green_carpet",
		"heavy_weighted_pressure_plate", "horn_coral", "horn_coral_fan",
		"iron_door", "jungle_door", "jungle_pressure_plate", "jungle_sign",
		"leaf_litter", "light_blue_banner", "light_blue_candle",
		"light_blue_candle_cake", "light_blue_carpet", "light_gray_banner",
		"light_gray_candle", "light_gray_candle_cake", "light_gray_carpet",
		"light_weighted_pressure_plate", "lime_banner", "lime_candle",
		"lime_candle_cake", "lime_carpet", "magenta_banner", "magenta_candle",
		"magenta_candle_cake", "magenta_carpet", "mangrove_door",
		"mangrove_pressure_plate", "mangrove_sign", "moss_carpet", "oak_door",
		"oak_pressure_plate", "oak_sign", "orange_banner", "orange_candle",
		"orange_candle_cake", "orange_carpet", "pale_oak_door",
		"pale_oak_pressure_plate", "pale_oak_sign", "pink_banner", "pink_candle",
		"pink_candle_cake", "pink_carpet", "polished_blackstone_pressure_plate",
		"powered_rail", "purple_banner", "purple_candle", "purple_candle_cake",
		"purple_carpet", "rail", "red_banner", "red_candle", "red_candle_cake",
		"red_carpet", "red_mushroom", "redstone_torch", "redstone_wire",
		"repeater", "sea_pickle", "snow", "soul_torch", "spruce_door",
		"spruce_pressure_plate", "spruce_sign", "stone_pressure_plate",
		"tall_seagrass", "torch", "tube_coral", "tube_coral_fan", "warped_door",
		"warped_pressure_plate", "warped_sign", "white_banner", "white_candle",
		"white_candle_cake", "white_carpet", "yellow_banner", "yellow_candle",
		"yellow_candle_cake", "yellow_carpet",
	},
	SupportHangable: {
		"lantern", "soul_lantern",
	},
	SupportSoil: {
		"acacia_sapling", "allium", "attached_melon_stem",
		"attached_pumpkin_stem", "azalea", "azure_bluet", "bamboo",
		"bamboo_sapling", "birch_sapling", "blue_orchid", "bush", "cactus",
		"cactus_flower", "cherry_sapling", "closed_eyeblossom", "cornflower",
		"crimson_fungus", "crimson_roots", "dandelion", "dark_oak_sapling",
		"dead_bush", "fern", "firefly_bush", "flowering_azalea",
		"golden_dandelion", "jungle_sapling", "large_fern", "lilac",
		"lily_of_the_valley", "mangrove_propagule", "melon_stem",
		"nether_sprouts", "nether_wart", "oak_sapling", "open_eyeblossom",
		"orange_tulip", "oxeye_daisy", "pale_oak_sapling", "peony",
		"pink_petals", "pink_tulip", "pitcher_plant", "poppy", "pumpkin_stem",
		"red_tulip", "rose_bush", "seagrass", "short_dry_grass", "short_grass",
		"small_dripleaf", "spruce_sapling", "sugar_cane", "sunflower",
		"sweet_berry_bush", "tall_dry_grass", "tall_grass", "torchflower",
		"warped_fungus", "warped_roots", "white_tulip", "wildflowers",
		"wither_rose",
	},
	SupportWater: {
		"lily_pad",
	},
	SupportSpeleothem: {
		"pointed_dripstone", "sulfur_spike",
	},
	SupportWall: {
		"acacia_wall_sign", "bamboo_wall_sign", "birch_wall_sign",
		"black_wall_banner", "blue_wall_banner", "brain_coral_wall_fan",
		"brown_wall_banner", "bubble_coral_wall_fan", "cherry_wall_sign",
		"cocoa", "copper_wall_torch", "crimson_wall_sign", "cyan_wall_banner",
		"dark_oak_wall_sign", "dead_brain_coral_wall_fan",
		"dead_bubble_coral_wall_fan", "dead_fire_coral_wall_fan",
		"dead_horn_coral_wall_fan", "dead_tube_coral_wall_fan",
		"fire_coral_wall_fan", "gray_wall_banner", "green_wall_banner",
		"horn_coral_wall_fan", "jungle_wall_sign", "ladder",
		"light_blue_wall_banner", "light_gray_wall_banner", "lime_wall_banner",
		"magenta_wall_banner", "mangrove_wall_sign", "oak_wall_sign",
		"orange_wall_banner", "pale_oak_wall_sign", "pink_wall_banner",
		"purple_wall_banner", "red_wall_banner", "redstone_wall_torch",
		"soul_wall_torch", "spruce_wall_sign", "tripwire_hook",
		"tube_coral_wall_fan", "wall_torch", "warped_wall_sign",
		"white_wall_banner", "yellow_wall_banner",
	},
}

// supportKinds is supportNames flattened to every state of every listed block.
var supportKinds = func() map[uint32]SupportKind {
	out := map[uint32]SupportKind{}
	for kind, names := range supportNames {
		for _, n := range names {
			lo, hi, ok := BlockRangeOK(n)
			if !ok {
				continue // content this canonical version does not have
			}
			for s := lo; s <= hi; s++ {
				out[s] = kind
			}
		}
	}
	return out
}()

// SupportFor reports what a block needs to stay where it is.
func SupportFor(state uint32) SupportKind { return supportKinds[state] }
