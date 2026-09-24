package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Flowing fluid does not stop at every block: vanilla FlowingFluid.
// canHoldAnyFluid lets water and lava into a cell whose block is in
// #washed_away_by_fluids — crops, flowers, torches, dust, rails, buttons,
// carpets, snow layers, skulls, pots — destroying it on the way in
// (WaterFluid drops the block's loot; LavaFluid only fizzes). Any block not
// in the tag holds the fluid back, whatever its shape: a pressure plate or a
// banner is a wall to water, and so is a piston's moving cell. A block that
// takes the fluid in (a LiquidBlockContainer: every waterloggable, kelp,
// seagrass) is left standing, never washed.

// fluidPassable reports a cell flowing fluid may enter: open, or holding a
// block the fluid washes away.
func fluidPassable(state uint32) bool {
	return worldgen.IsReplaceable(state) || fluidWashes(state)
}

// fluidWashes reports a non-replaceable block that flowing fluid destroys
// and replaces.
func fluidWashes(state uint32) bool {
	if state == worldgen.Air || worldgen.IsFluid(state) || worldgen.IsReplaceable(state) {
		return false
	}
	if isLiquidContainer(state) {
		return false // takes the fluid in, not washed
	}
	return inRanges2(state, washedByFluids)
}

// isLiquidContainer is `instanceof LiquidBlockContainer`: the waterloggable
// blocks, and kelp and seagrass, which live in water without a property.
func isLiquidContainer(state uint32) bool {
	if info, ok := worldgen.InfoForState(state); ok && info.HasProperty("waterlogged") {
		return true
	}
	return inRanges2(state, plainLiquidContainers)
}

var (
	washedByFluids        = worldgen.BlockTag("washed_away_by_fluids")
	plainLiquidContainers = blockRangeOK("kelp", "kelp_plant", "seagrass", "tall_seagrass")
)

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
