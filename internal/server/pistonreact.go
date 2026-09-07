package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Piston push reactions and block-entity ownership, by vanilla's block
// registrations (1.21.5, plus the 1.21.11 copper chest, shelf and copper
// torch/lantern additions). PistonBaseBlock.isPushable consults three
// things about a block: its PushReaction (NORMAL by default; BLOCK never
// moves, DESTROY breaks when pushed and cannot be pulled, PUSH_ONLY moves
// only away from the piston), whether it can be broken at all, and whether
// it carries a block entity (chests, signs, banners… never move).

// PushReaction.BLOCK: never moved by a piston.
var pushBlockNames = []string{
	"piston_head", "moving_piston", "nether_portal", "end_portal", "anvil", "chipped_anvil",
	"damaged_anvil", "barrier", "end_gateway", "grindstone", "lodestone",
}

// PushReaction.DESTROY: pushed blocks break; nothing pulls them.
var pushDestroyNames = []string{
	"acacia_door", "acacia_pressure_plate", "acacia_sapling", "allium", "amethyst_cluster", "azalea",
	"azure_bluet", "bamboo", "bamboo_door", "bamboo_pressure_plate", "bamboo_sapling", "beetroots",
	"bell", "big_dripleaf", "big_dripleaf_stem", "birch_door", "birch_pressure_plate", "birch_sapling",
	"blue_orchid", "brain_coral", "brain_coral_fan", "brain_coral_wall_fan", "brown_mushroom", "bubble_column",
	"bubble_coral", "bubble_coral_fan", "bubble_coral_wall_fan", "budding_amethyst", "bush", "cactus",
	"cactus_flower", "cake", "carrots", "carved_pumpkin", "cave_vines", "cave_vines_plant",
	"cherry_door", "cherry_leaves", "cherry_pressure_plate", "cherry_sapling", "chorus_flower", "chorus_plant",
	"closed_eyeblossom", "cobweb", "cocoa", "comparator", "copper_door", "copper_lantern",
	"copper_torch", "copper_wall_torch", "cornflower", "creeper_head", "creeper_wall_head", "crimson_door",
	"crimson_fungus", "crimson_pressure_plate", "crimson_roots", "dandelion", "dark_oak_door", "dark_oak_pressure_plate",
	"dark_oak_sapling", "dead_bush", "decorated_pot", "dragon_egg", "dragon_head", "dragon_wall_head",
	"fern", "fire", "fire_coral", "fire_coral_fan", "fire_coral_wall_fan", "firefly_bush",
	"flowering_azalea", "frogspawn", "glow_lichen", "hanging_roots", "heavy_weighted_pressure_plate", "horn_coral",
	"horn_coral_fan", "horn_coral_wall_fan", "iron_door", "jack_o_lantern", "jungle_door", "jungle_pressure_plate",
	"jungle_sapling", "kelp", "kelp_plant", "ladder", "lantern", "large_fern",
	"lava", "leaf_litter", "lever", "light_weighted_pressure_plate", "lilac", "lily_of_the_valley",
	"lily_pad", "mangrove_door", "mangrove_pressure_plate", "mangrove_propagule", "moss_block", "moss_carpet",
	"nether_sprouts", "nether_wart", "oak_door", "oak_pressure_plate", "oak_sapling", "open_eyeblossom",
	"orange_tulip", "oxeye_daisy", "pale_hanging_moss", "pale_moss_block", "pale_moss_carpet", "pale_oak_door",
	"pale_oak_leaves", "pale_oak_pressure_plate", "pale_oak_sapling", "peony", "piglin_head", "piglin_wall_head",
	"pink_petals", "pink_tulip", "pitcher_crop", "pitcher_plant", "player_head", "player_wall_head",
	"pointed_dripstone", "polished_blackstone_pressure_plate", "poppy", "potatoes", "red_mushroom", "red_tulip",
	"redstone_torch", "redstone_wall_torch", "redstone_wire", "repeater", "resin_clump", "rose_bush",
	"scaffolding", "sculk_vein", "sea_pickle", "seagrass", "short_dry_grass", "short_grass",
	"skeleton_skull", "skeleton_wall_skull", "small_dripleaf", "snow", "soul_fire", "soul_lantern",
	"soul_torch", "soul_wall_torch", "spore_blossom", "spruce_door", "spruce_pressure_plate", "spruce_sapling",
	"stone_pressure_plate", "structure_void", "sugar_cane", "sunflower", "suspicious_gravel", "suspicious_sand",
	"sweet_berry_bush", "tall_dry_grass", "tall_grass", "tall_seagrass", "torch", "torchflower",
	"torchflower_crop", "tripwire", "tripwire_hook", "tube_coral", "tube_coral_fan", "tube_coral_wall_fan",
	"turtle_egg", "twisting_vines", "twisting_vines_plant", "vine", "wall_torch", "warped_door",
	"warped_fungus", "warped_pressure_plate", "warped_roots", "water", "weeping_vines", "weeping_vines_plant",
	"wheat", "white_tulip", "wildflowers", "wither_rose", "wither_skeleton_skull", "wither_skeleton_wall_skull",
	"zombie_head", "zombie_wall_head",
}

// PushReaction.PUSH_ONLY: moved only in the piston's own direction.
var pushOnlyNames = []string{
	"white_glazed_terracotta", "orange_glazed_terracotta", "magenta_glazed_terracotta", "light_blue_glazed_terracotta", "yellow_glazed_terracotta", "lime_glazed_terracotta",
	"pink_glazed_terracotta", "gray_glazed_terracotta", "light_gray_glazed_terracotta", "cyan_glazed_terracotta", "purple_glazed_terracotta", "blue_glazed_terracotta",
	"brown_glazed_terracotta", "green_glazed_terracotta", "red_glazed_terracotta", "black_glazed_terracotta",
}

// Blocks with a block entity (BlockEntityType registrations): a piston refuses them.
var blockEntityNames = []string{
	"acacia_hanging_sign", "acacia_shelf", "acacia_sign", "acacia_wall_hanging_sign", "acacia_wall_sign", "bamboo_hanging_sign",
	"bamboo_shelf", "bamboo_sign", "bamboo_wall_hanging_sign", "bamboo_wall_sign", "barrel", "beacon",
	"bee_nest", "beehive", "bell", "birch_hanging_sign", "birch_shelf", "birch_sign",
	"birch_wall_hanging_sign", "birch_wall_sign", "black_banner", "black_bed", "black_shulker_box", "black_wall_banner",
	"blast_furnace", "blue_banner", "blue_bed", "blue_shulker_box", "blue_wall_banner", "brewing_stand",
	"brown_banner", "brown_bed", "brown_shulker_box", "brown_wall_banner", "calibrated_sculk_sensor", "campfire",
	"chain_command_block", "cherry_hanging_sign", "cherry_shelf", "cherry_sign", "cherry_wall_hanging_sign", "cherry_wall_sign",
	"chest", "chiseled_bookshelf", "command_block", "comparator", "conduit", "copper_chest",
	"crafter", "creaking_heart", "creeper_head", "creeper_wall_head", "crimson_hanging_sign", "crimson_shelf",
	"crimson_sign", "crimson_wall_hanging_sign", "crimson_wall_sign", "cyan_banner", "cyan_bed", "cyan_shulker_box",
	"cyan_wall_banner", "dark_oak_hanging_sign", "dark_oak_shelf", "dark_oak_sign", "dark_oak_wall_hanging_sign", "dark_oak_wall_sign",
	"daylight_detector", "decorated_pot", "dispenser", "dragon_head", "dragon_wall_head", "dropper",
	"enchanting_table", "end_gateway", "end_portal", "ender_chest", "exposed_copper_chest", "furnace",
	"gray_banner", "gray_bed", "gray_shulker_box", "gray_wall_banner", "green_banner", "green_bed",
	"green_shulker_box", "green_wall_banner", "hopper", "jigsaw", "jukebox", "jungle_hanging_sign",
	"jungle_shelf", "jungle_sign", "jungle_wall_hanging_sign", "jungle_wall_sign", "lectern", "light_blue_banner",
	"light_blue_bed", "light_blue_shulker_box", "light_blue_wall_banner", "light_gray_banner", "light_gray_bed", "light_gray_shulker_box",
	"light_gray_wall_banner", "lime_banner", "lime_bed", "lime_shulker_box", "lime_wall_banner", "magenta_banner",
	"magenta_bed", "magenta_shulker_box", "magenta_wall_banner", "mangrove_hanging_sign", "mangrove_shelf", "mangrove_sign",
	"mangrove_wall_hanging_sign", "mangrove_wall_sign", "moving_piston", "oak_hanging_sign", "oak_shelf", "oak_sign",
	"oak_wall_hanging_sign", "oak_wall_sign", "orange_banner", "orange_bed", "orange_shulker_box", "orange_wall_banner",
	"oxidized_copper_chest", "pale_oak_hanging_sign", "pale_oak_shelf", "pale_oak_sign", "pale_oak_wall_hanging_sign", "pale_oak_wall_sign",
	"piglin_head", "piglin_wall_head", "pink_banner", "pink_bed", "pink_shulker_box", "pink_wall_banner",
	"player_head", "player_wall_head", "purple_banner", "purple_bed", "purple_shulker_box", "purple_wall_banner",
	"red_banner", "red_bed", "red_shulker_box", "red_wall_banner", "repeating_command_block", "sculk_catalyst",
	"sculk_sensor", "sculk_shrieker", "shulker_box", "skeleton_skull", "skeleton_wall_skull", "smoker",
	"soul_campfire", "spawner", "spruce_hanging_sign", "spruce_shelf", "spruce_sign", "spruce_wall_hanging_sign",
	"spruce_wall_sign", "structure_block", "suspicious_gravel", "suspicious_sand", "test_block", "test_instance_block",
	"trapped_chest", "trial_spawner", "vault", "warped_hanging_sign", "warped_shelf", "warped_sign",
	"warped_wall_hanging_sign", "warped_wall_sign", "waxed_copper_chest", "waxed_exposed_copper_chest", "waxed_oxidized_copper_chest", "waxed_weathered_copper_chest",
	"weathered_copper_chest", "white_banner", "white_bed", "white_shulker_box", "white_wall_banner", "wither_skeleton_skull",
	"wither_skeleton_wall_skull", "yellow_banner", "yellow_bed", "yellow_shulker_box", "yellow_wall_banner", "zombie_head",
	"zombie_wall_head",
}

type pushReaction uint8

const (
	pushNormal pushReaction = iota
	pushBlock
	pushDestroy
	pushOnly
)

var (
	pushBlockRanges   = rangesOf(pushBlockNames)
	pushDestroyRanges = rangesOf(pushDestroyNames)
	pushOnlyRanges    = rangesOf(pushOnlyNames)
	blockEntityRanges = rangesOf(blockEntityNames)
)

// rangesOf collects the state spans of every named block that exists in the
// engine's tables (names from a newer or older version are skipped).
func rangesOf(names []string) []stateRange {
	var out []stateRange
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, stateRange{lo, hi})
		}
	}
	return out
}

func pushReactionOf(s uint32) pushReaction {
	switch {
	case inRanges(pushBlockRanges, s):
		return pushBlock
	case inRanges(pushDestroyRanges, s):
		return pushDestroy
	case inRanges(pushOnlyRanges, s):
		return pushOnly
	}
	return pushNormal
}

func hasBlockEntity(s uint32) bool { return inRanges(blockEntityRanges, s) }
