package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Flowing fluid does not stop at every block: vanilla FlowingFluid.canHoldAnyFluid
// lets water and lava into any cell whose block does not block motion —
// crops, torches, dust, rails' cousins, carpets, flower pots — destroying
// it on the way in (WaterFluid drops the block's loot; LavaFluid only
// fizzes). The exceptions are doors, signs, ladders, sugar cane, bubble
// columns and the portals. Blocks that take the fluid in (waterloggable)
// are left standing: the engine keeps no waterlogged fluid, so letting
// water through them would have to destroy them instead.

// fluidPassable reports a cell flowing fluid may enter: open, or holding a
// block the fluid washes away.
func fluidPassable(state uint32) bool {
	return worldgen.IsReplaceable(state) || fluidWashes(state)
}

// fluidWashes reports a non-replaceable block that flowing fluid destroys
// and replaces. "Blocks motion" is vanilla's legacy solid test: a collision
// shape at least 0.73 on average or a full block tall — so the small
// colliders (carpets, pots, heads, eggs, lily pads, repeaters, a two-layer
// snow) wash away with the non-colliders; lanterns are waterloggable and
// so stay.
func fluidWashes(state uint32) bool {
	if state == worldgen.Air || worldgen.IsFluid(state) || worldgen.IsReplaceable(state) {
		return false
	}
	info, ok := worldgen.InfoForState(state)
	if ok && info.HasProperty("waterlogged") {
		return false // LiquidBlockContainer: would take the fluid in, not be washed
	}
	if worldgen.IsDoor(state) || inStates(state, caneStates) || isPortalBlock(state) || inRanges2(state, fluidProofStates) {
		return false
	}
	if _, isSign := signKind(state); isSign {
		return false
	}
	if !worldgen.Collides(state) {
		return true
	}
	return inRanges2(state, washedSmallStates)
}

// fluidProofStates are canHoldAnyFluid's named exclusions that are neither
// doors nor signs (ladders are waterloggable and excluded by that already).
var fluidProofStates = blockRangeOK("end_portal", "end_gateway", "structure_void")

// washedSmallStates are the colliding blocks whose shape is too small to
// count as solid, so fluid washes them.
var washedSmallStates = func() [][2]uint32 {
	names := []string{"flower_pot", "turtle_egg", "lily_pad", "repeater", "comparator",
		"moss_carpet", "pale_moss_carpet",
		"skeleton_skull", "wither_skeleton_skull", "zombie_head", "player_head", "creeper_head", "dragon_head", "piglin_head",
		"skeleton_wall_skull", "wither_skeleton_wall_skull", "zombie_wall_head", "player_wall_head", "creeper_wall_head", "dragon_wall_head", "piglin_wall_head"}
	for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
		names = append(names, c+"_carpet")
	}
	for _, p := range []string{"torchflower", "oak_sapling", "spruce_sapling", "birch_sapling", "jungle_sapling", "acacia_sapling",
		"cherry_sapling", "dark_oak_sapling", "pale_oak_sapling", "mangrove_propagule", "fern", "dandelion", "poppy", "blue_orchid",
		"allium", "azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy", "cornflower",
		"lily_of_the_valley", "wither_rose", "open_eyeblossom", "closed_eyeblossom", "red_mushroom", "brown_mushroom",
		"dead_bush", "cactus", "bamboo", "azalea_bush", "flowering_azalea_bush", "crimson_fungus", "warped_fungus",
		"crimson_roots", "warped_roots"} {
		names = append(names, "potted_"+p)
	}
	rs := blockRangeOK(names...)
	if lo, _, ok := worldgen.BlockRangeOK("snow"); ok {
		rs = append(rs, [2]uint32{lo + 1, lo + 1}) // layers=2 (layers=1 is replaceable, 3+ block motion)
	}
	return rs
}()

// blockRangeOK is blockRange for names that may be absent from the registry.
func blockRangeOK(names ...string) [][2]uint32 {
	var rs [][2]uint32
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			rs = append(rs, [2]uint32{lo, hi})
		}
	}
	return rs
}

// fluidInto lays a fluid state into a cell, washing out whatever stood
// there (FlowingFluid.spreadTo → beforeDestroyingBlock): water drops the
// block's loot, lava just fizzes.
func (h *hub) fluidInto(players map[int32]*tracked, dim int, pos blockPos, fluid uint32, water bool) {
	old := h.worldFor(dim).Block(pos.x, pos.y, pos.z)
	if old != worldgen.Air && !worldgen.IsFluid(old) {
		if water {
			h.dropLoose(players, dim, pos, old)
		} else {
			h.fizz(players, dim, pos)
		}
	}
	h.setBlockAt(players, dim, pos, fluid)
}
