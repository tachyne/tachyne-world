package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Farmland moisture + trampling, reimplemented from the vanilla FarmBlock:
// tilled soil hydrates to moisture 7 when water is within a 9×9×2 box (or it's
// raining on the block above), dries a stage at a time otherwise, and finally
// reverts to dirt once bone-dry with nothing growing on it. Jumping onto it
// from height tramples it back to dirt. A crop sitting on farmland that turns
// to dirt pops off with its drops (vanilla setBlockAndUpdate → canSurvive).

// maintainsFarmlandRanges are the MAINTAINS_FARMLAND block-tag members: a plant
// from this set sitting directly above keeps the soil tilled, and it is the
// plant that pops when the soil reverts.
var maintainsFarmlandRanges = blockRange(
	"wheat", "carrots", "potatoes", "beetroots",
	"melon_stem", "pumpkin_stem", "attached_melon_stem", "attached_pumpkin_stem",
	"torchflower_crop", "torchflower", "pitcher_crop",
)

func maintainsFarmland(state uint32) bool {
	for _, r := range maintainsFarmlandRanges {
		if inRange(state, r) {
			return true
		}
	}
	return false
}

// farmlandRandomTick runs one FarmBlock.randomTick: hydrate, dehydrate, or dry
// out to dirt. Returns whether it handled the block.
func (h *hub) farmlandRandomTick(players map[int32]*tracked, x, y, z int, state uint32) bool {
	if state < farmlandMin || state > farmlandMin+7 {
		return false
	}
	n := int(state - farmlandMin) // moisture 0..7
	switch {
	case h.farmlandNearWater(x, y, z) || h.rainingAbove(x, y, z):
		if n < 7 {
			h.setBlock(players, blockPos{x, y, z}, farmlandMin+7)
		}
	case n > 0:
		h.setBlock(players, blockPos{x, y, z}, farmlandMin+uint32(n-1))
	case !maintainsFarmland(h.world.At(x, y+1, z)):
		h.turnFarmlandToDirt(players, x, y, z)
	}
	return true
}

// farmlandNearWater reports water within the vanilla scan box: horizontally
// ±4, vertically the soil's own level and the one above (FarmBlock.isNearWater).
func (h *hub) farmlandNearWater(x, y, z int) bool {
	for dy := 0; dy <= 1; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				if worldgen.IsWater(h.world.At(x+dx, y+dy, z+dz)) {
					return true
				}
			}
		}
	}
	return false
}

// rainingAbove reports whether rain is falling on the block above the soil —
// raining, the column is open to the sky, and this biome/height gets rain (not
// snow). Mirrors ServerLevel.isRainingAt(pos.above()).
func (h *hub) rainingAbove(x, y, z int) bool {
	return h.raining && h.skyExposedColumn(x, z) &&
		worldgen.PrecipitationAt(h.world.BiomeAt(x, z), y+1) == worldgen.PrecipRain
}

// turnFarmlandToDirt reverts tilled soil to dirt, popping any crop resting on
// it (FarmBlock.turnToDirt → the crop above loses support and drops).
func (h *hub) turnFarmlandToDirt(players map[int32]*tracked, x, y, z int) {
	if above := h.world.At(x, y+1, z); maintainsFarmland(above) {
		if h.rules.DoTileDrops {
			for _, d := range h.evalBlockLoot(lootCtx{state: above,
				rng: h.rng.Intn, randf: h.rng.Float64}) {
				h.spawnBlockDrop(players, d.item, d.count, x, y+1, z)
			}
		}
		h.setBlock(players, blockPos{x, y + 1, z}, worldgen.Air)
	}
	h.setBlock(players, blockPos{x, y, z}, worldgen.Dirt)
}

// tramplePlayer tramples farmland the player just landed on from `dist` blocks
// up (FarmBlock.fallOn): probability dist-0.5, and a player's hitbox always
// clears vanilla's 0.512 size gate. Called on landing, off the fall path.
func (h *hub) tramplePlayer(players map[int32]*tracked, t *tracked, x, y, z int, dist float64) {
	if h.rng.Float64() >= dist-0.5 {
		return
	}
	state := h.world.At(x, y, z)
	if state >= farmlandMin && state <= farmlandMin+7 {
		h.turnFarmlandToDirt(players, x, y, z)
	}
}
