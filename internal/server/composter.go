package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The composter. Feed it plant matter and each piece has a chance to raise the
// pile a level; the first item always takes (vanilla lets level 0 through
// regardless of the roll). At level seven it sits a second and then turns
// ready, and a ready composter hands back one bone meal and empties.
//
// The chances are vanilla's five tiers: leaves and seeds are poor compost,
// crops and flowers middling, blocks of the same good, and cake best of all.

var composterBase = worldgen.BlockBase("composter") // levels 0..8 run upward

const (
	composterFull  = 7  // the pile is full but not yet composted
	composterReady = 8  // vanilla READY: right-click for bone meal
	composterDelay = 20 // ticks between full and ready
)

// compostChance is the item→chance table, keyed by name because ids move
// between versions.
var compostChance = func() map[int32]float64 {
	tiers := []struct {
		chance float64
		items  []string
	}{
		{0.3, []string{
			"jungle_leaves", "oak_leaves", "spruce_leaves", "dark_oak_leaves",
			"pale_oak_leaves", "acacia_leaves", "cherry_leaves", "birch_leaves",
			"azalea_leaves", "mangrove_leaves", "oak_sapling", "spruce_sapling",
			"birch_sapling", "jungle_sapling", "acacia_sapling", "cherry_sapling",
			"dark_oak_sapling", "pale_oak_sapling", "mangrove_propagule", "beetroot_seeds",
			"dried_kelp", "short_grass", "kelp", "melon_seeds", "pumpkin_seeds",
			"seagrass", "sweet_berries", "glow_berries", "wheat_seeds", "moss_carpet",
			"pale_moss_carpet", "pale_hanging_moss", "pink_petals", "wildflowers",
			"leaf_litter", "small_dripleaf", "hanging_roots", "mangrove_roots",
			"torchflower_seeds", "pitcher_pod", "firefly_bush", "bush", "cactus_flower",
			"dry_short_grass", "dry_tall_grass",
		}},
		{0.5, []string{
			"dried_kelp_block", "tall_grass", "flowering_azalea_leaves", "cactus",
			"sugar_cane", "vine", "nether_sprouts", "weeping_vines", "twisting_vines",
			"melon_slice", "glow_lichen",
		}},
		{0.65, []string{
			"sea_pickle", "lily_pad", "pumpkin", "carved_pumpkin", "melon", "apple",
			"beetroot", "carrot", "cocoa_beans", "potato", "wheat", "brown_mushroom",
			"red_mushroom", "mushroom_stem", "crimson_fungus", "warped_fungus",
			"nether_wart", "crimson_roots", "warped_roots", "shroomlight", "dandelion",
			"poppy", "blue_orchid", "allium", "azure_bluet", "red_tulip", "orange_tulip",
			"white_tulip", "pink_tulip", "oxeye_daisy", "cornflower", "lily_of_the_valley",
			"wither_rose", "open_eyeblossom", "closed_eyeblossom", "fern", "sunflower",
			"lilac", "rose_bush", "peony", "large_fern", "spore_blossom", "azalea",
			"moss_block", "pale_moss_block", "big_dripleaf",
		}},
		{0.85, []string{
			"hay_block", "brown_mushroom_block", "red_mushroom_block", "nether_wart_block",
			"warped_wart_block", "flowering_azalea", "bread", "baked_potato", "cookie",
			"torchflower", "pitcher_plant",
		}},
		{1.0, []string{"cake", "pumpkin_pie"}},
	}
	out := map[int32]float64{}
	for _, tier := range tiers {
		for _, name := range tier.items {
			if id, ok := itemByName[name]; ok {
				out[int32(id)] = tier.chance
			}
		}
	}
	return out
}()

// composterLevel reports whether a state is a composter, and how full it is.
func composterLevel(st uint32) (int, bool) {
	if st < composterBase || st > composterBase+composterReady {
		return 0, false
	}
	return int(st - composterBase), true
}

type evUseComposter struct {
	eid, slot int32
	x, y, z   int
}

func (evUseComposter) isHubEvent() {}

// useComposter is the right-click: empty a ready one, or feed it.
func (h *hub) useComposter(players map[int32]*tracked, t *tracked, pos blockPos) {
	state := h.worldFor(t.dim).At(pos.x, pos.y, pos.z)
	level, ok := composterLevel(state)
	if !ok {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	if level == composterReady {
		h.setBlockAt(players, t.dim, pos, composterBase)
		h.spawnItemIn(players, t.dim, itemBoneMeal, 1, cx, cy+1, cz)
		h.playSoundDim(players, t.dim, "minecraft:block.composter.empty", sndBlock, cx, cy, cz, 1, 1)
		return
	}
	held := heldStack(t)
	chance, compostable := compostChance[held.item]
	if !compostable || level >= composterFull {
		return
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	// Vanilla's roll: an empty composter always takes the first item, and
	// after that the item's own chance decides.
	if level != 0 && h.rng.Float64() >= chance {
		h.playSoundDim(players, t.dim, "minecraft:block.composter.fill", sndBlock, cx, cy, cz, 1, 1)
		return
	}
	h.setBlockAt(players, t.dim, pos, composterBase+uint32(level)+1)
	h.playSoundDim(players, t.dim, "minecraft:block.composter.fill_success", sndBlock, cx, cy, cz, 1, 1)
	if level+1 == composterFull {
		h.scheduleIn(t.dim, pos, composterDelay) // it finishes composting a second later
	}
}

// tickComposter is the scheduled tick that turns a full pile into a ready one.
// Reports whether it handled the update, so it can sit in processUpdate's chain.
func (h *hub) tickComposter(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	level, ok := composterLevel(state)
	if !ok {
		return false
	}
	if level == composterFull {
		h.setBlockAt(players, dim, pos, composterBase+composterReady)
		h.playSoundDim(players, dim, "minecraft:block.composter.ready", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	}
	return true
}
