package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

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

// keepsFarmland is the whole 26.3 #maintains_farmland tag, which also holds
// the fence gates and moving_piston: dry soil under any of them stays tilled.
// (maintainsFarmland is the plant half, the part that pops on a revert.)
func keepsFarmland(state uint32) bool {
	return maintainsFarmland(state) || isFenceGate(state) || isMovingPiston(state)
}

// farmlandRandomTick runs one FarmBlock.randomTick: hydrate, dehydrate, or dry
// out to dirt. Returns whether it handled the block.
func (h *hub) farmlandRandomTick(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state < farmlandMin || state > farmlandMin+7 {
		return false
	}
	n := int(state - farmlandMin) // moisture 0..7
	switch {
	case h.farmlandNearWater(dim, x, y, z) || h.rainingAbove(dim, x, y, z):
		if n < 7 {
			h.setBlockAt(players, dim, blockPos{x, y, z}, farmlandMin+7)
		}
	case n > 0:
		h.setBlockAt(players, dim, blockPos{x, y, z}, farmlandMin+uint32(n-1))
	case !keepsFarmland(h.worldFor(dim).At(x, y+1, z)):
		h.turnFarmlandToDirt(players, dim, x, y, z)
	}
	return true
}

// farmlandNearWater reports water within the vanilla scan box: horizontally
// ±4, vertically the soil's own level and the one above (FarmBlock.isNearWater).
func (h *hub) farmlandNearWater(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	for dy := 0; dy <= 1; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				if worldgen.HoldsWater(w.At(x+dx, y+dy, z+dz)) { // getFluidState().is(WATER): waterlogged blocks too
					return true
				}
			}
		}
	}
	return false
}

// rainingAbove reports whether rain is falling on the block above the soil:
// ServerLevel.isRainingAt(pos.above()) — raining, nothing that blocks motion
// or holds a fluid above that cell (the MOTION_BLOCKING heightmap, so glass
// and leaves shelter it; a crop does not), and the biome rains there.
func (h *hub) rainingAbove(dim, x, y, z int) bool {
	// Only the overworld has weather: a Nether or End farm is never rained on.
	return h.rainAt(dim, x, y+1, z)
}

// turnFarmlandToDirt reverts tilled soil to dirt, popping any crop resting on
// it (FarmBlock.turnToDirt → the crop above loses support and drops).
func (h *hub) turnFarmlandToDirt(players map[int32]*tracked, dim, x, y, z int) {
	if above := h.worldFor(dim).At(x, y+1, z); maintainsFarmland(above) {
		if h.rules.DoTileDrops {
			for _, d := range h.evalBlockLoot(lootCtx{state: above,
				rng: h.rng.Intn, randf: h.rng.Float64}) {
				h.spawnBlockDrop(players, dim, d.item, d.count, x, y+1, z)
			}
		}
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, worldgen.Air)
	}
	h.turnToBaseBlock(players, dim, blockPos{x, y, z}, worldgen.Dirt)
}

// turnToBaseBlock is FarmlandBlock/PathBlock.turnToBaseBlock: the 15/16-tall
// block becomes dirt, whoever stands on it is lifted onto the full block
// (Block.pushEntitiesUp), and it is a BLOCK_CHANGE.
func (h *hub) turnToBaseBlock(players map[int32]*tracked, dim int, pos blockPos, base uint32) {
	h.pushEntitiesUp(players, dim, pos)
	h.setBlockAt(players, dim, pos, base)
	h.vib(dim, freqBlockChange, pos.x, pos.y, pos.z, 0)
}

// pushEntitiesUp lifts anything whose box meets the top sixteenth of the
// cell (the part the new full block adds) to stand on the full block.
func (h *hub) pushEntitiesUp(players map[int32]*tracked, dim int, pos blockPos) {
	lo, top := float64(pos.y)+15.0/16, float64(pos.y)+1
	x0, x1 := float64(pos.x), float64(pos.x)+1
	z0, z1 := float64(pos.z), float64(pos.z)+1
	meets := func(x, y, z, half float64) bool {
		return x-half < x1 && x+half > x0 && z-half < z1 && z+half > z0 && y < top && y >= lo-1e-7
	}
	for _, m := range h.mobs {
		if m.dim == dim && m.dying == 0 && meets(m.x, m.y, m.z, m.box().w/2) {
			m.y = top
		}
	}
	for _, t := range players {
		if t.dim == dim && !t.dead && meets(t.x, t.y, t.z, 0.3) {
			h.teleportPlayer(players, t, t.x, top, t.z)
		}
	}
}

// tramplePlayer tramples farmland the player just landed on from `dist` blocks
// up (FarmBlock.fallOn): probability dist-0.5, and a player's hitbox always
// clears vanilla's 0.512 size gate. Called on landing, off the fall path.
func (h *hub) tramplePlayer(players map[int32]*tracked, t *tracked, x, y, z int, dist float64) {
	if h.rng.Float64() >= dist-0.5 {
		return
	}
	state := h.worldFor(t.dim).At(x, y, z)
	if state >= farmlandMin && state <= farmlandMin+7 {
		h.turnFarmlandToDirt(players, t.dim, x, y, z)
	}
}

// mobTrample is FarmlandBlock.fallOn for a mob: a landing from past half a
// block may turn the farmland under it to dirt (the chance is the fall less
// half a block), if mob griefing allows and the mob is big enough —
// width² × height over 0.512, so a cow tramples and a chicken never does.
func (h *hub) mobTrample(players map[int32]*tracked, m *mob, fell float64) {
	if !h.rules.MobGriefing {
		return
	}
	if b := m.box(); b.w*b.w*b.h <= 0.512 {
		return
	}
	if h.rng.Float64() >= fell-0.5 {
		return
	}
	x, y, z := int(math.Floor(m.x)), int(math.Floor(m.y))-1, int(math.Floor(m.z))
	if st := h.worldFor(m.dim).At(x, y, z); st >= farmlandMin && st <= farmlandMin+7 {
		h.turnFarmlandToDirt(players, m.dim, x, y, z)
	}
}

// mobFallOnEgg is TurtleEggBlock.fallOn for a mob: landing on a clutch
// breaks an egg one time in three — never for the zombie kind (Zombie and
// what extends it: husks, drowned, zombie villagers, zombified piglins),
// and like every egg-breaking mob (canDestroyEgg) never a turtle or a bat,
// and only with mob griefing.
func (h *hub) mobFallOnEgg(players map[int32]*tracked, m *mob) {
	switch m.etype {
	case entityZombie, entityHusk, entityDrowned, entityZombieVillager, entityZombifiedPiglin,
		entityTurtle, entityBat:
		return
	}
	if !h.rules.MobGriefing {
		return
	}
	x, y, z := int(math.Floor(m.x)), int(math.Floor(m.y))-1, int(math.Floor(m.z))
	st := h.worldFor(m.dim).At(x, y, z)
	if !isTurtleEgg(st) {
		// A clutch is a thin block: the mob stands in its cell, not above it.
		y++
		if st = h.worldFor(m.dim).At(x, y, z); !isTurtleEgg(st) {
			return
		}
	}
	if h.rng.Intn(3) == 0 {
		h.crushTurtleEgg(players, m.dim, x, y, z, st)
	}
}
