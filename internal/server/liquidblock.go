package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// LiquidBlock's own hooks, and the fluid tick they schedule.
//
// A water or lava block does not flow the moment something changes beside
// it. LiquidBlock.onPlace and neighborChanged first let lava meet water
// (shouldSpreadLiquid: obsidian, cobblestone or basalt, at once), then ask
// for the fluid's tick getTickDelay away — 5 for water, 30 for lava, 10 for
// lava in an ultra-warm dimension. FlowingFluid.tick runs when that tick
// comes due, and a cell holds one pending fluid tick at most: asking again
// while one waits changes nothing (LevelChunkTicks.schedule).
//
// The ticks ride the simulation queue (sim.go pending); fluidTicks marks
// which of a cell's queued updates is its fluid tick. Any other update that
// reaches a fluid is a neighbour's change and only schedules.

// isLiquidBlock is a water or lava block itself (LiquidBlock) — not a
// bubble column and not a waterlogged block, whose water answers its own
// block's updateShape.
func isLiquidBlock(s uint32) bool {
	return (s >= worldgen.WaterBase && s <= worldgen.WaterBase+15) || worldgen.IsLava(s)
}

// fluidTickDelay is Fluid.getTickDelay for the fluid a state holds.
func fluidTickDelay(dim int, state uint32) uint64 {
	if worldgen.IsLava(state) {
		if dimType(dim).FastLava {
			return lavaDelayNether
		}
		return lavaDelay
	}
	return waterDelay
}

// scheduleFluidTick is Level.scheduleTick(pos, fluid, delay): the cell's
// fluid runs delay ticks from now, unless a fluid tick is already waiting
// there, which it keeps.
func (h *hub) scheduleFluidTick(dim int, pos blockPos, delay uint64) {
	if dim == 0 && !h.ownedBlock(pos.x, pos.z) {
		return
	}
	key := simPos{dim: dim, blockPos: pos}
	now := h.tick.Load()
	if due, ok := h.fluidTicks[key]; ok && due >= now {
		return // one is pending (an older mark whose bucket passed is stale)
	}
	if delay == 0 {
		delay = 1
	}
	if h.fluidTicks == nil {
		h.fluidTicks = map[simPos]uint64{}
	}
	h.fluidTicks[key] = now + delay
	h.scheduleIn(dim, pos, delay)
}

// takeFluidTick reports whether the update being run at pos is its fluid
// tick, and clears the mark.
func (h *hub) takeFluidTick(dim int, pos blockPos) bool {
	key := simPos{dim: dim, blockPos: pos}
	due, ok := h.fluidTicks[key]
	if !ok || due > h.tick.Load() {
		return false
	}
	delete(h.fluidTicks, key)
	return true
}

// fluidTickMoved follows a queued update the simulation put off (its chunk
// is not ticking, or the tick's cap was reached): if it was the cell's fluid
// tick, the mark moves with it, so the tick stays pending until it runs.
func (h *hub) fluidTickMoved(sp simPos, age, to uint64) {
	if due, ok := h.fluidTicks[sp]; ok && due <= age {
		h.fluidTicks[sp] = to
	}
}

// liquidOnPlace is LiquidBlock.onPlace and LiquidBlock.neighborChanged,
// which do the same: lava that meets water sets solid, and otherwise the
// fluid asks for its tick.
func (h *hub) liquidOnPlace(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	if !isLiquidBlock(state) {
		return
	}
	if worldgen.IsLava(state) && h.lavaMeetsWater(players, dim, pos, state) {
		return
	}
	h.scheduleFluidTick(dim, pos, fluidTickDelay(dim, state))
}

// lavaFlowOpposites are LiquidBlock.POSSIBLE_FLOW_DIRECTIONS (down, south,
// north, east, west) turned round, in that order: the cells shouldSpreadLiquid
// looks at — above, then north, south, west and east.
var lavaFlowOpposites = [5]blockPos{{0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// lavaMeetsWater is the lava half of LiquidBlock.shouldSpreadLiquid: water
// (any cell whose fluid is water — a waterlogged block, a column, seagrass)
// above or beside the lava turns a source to obsidian and a flow to
// cobblestone; lava over soul soil beside blue ice turns to basalt. It
// reports whether the lava set solid.
func (h *hub) lavaMeetsWater(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	w := h.worldFor(dim)
	overSoulSoil := w.Block(pos.x, pos.y-1, pos.z) == worldgen.SoulSoil
	for _, d := range lavaFlowOpposites {
		n := w.Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)
		to := uint32(0)
		switch {
		case worldgen.HoldsWater(n):
			to = worldgen.Cobblestone
			if worldgen.FluidLevel(state, worldgen.LavaBase) == 0 {
				to = worldgen.Obsidian
			}
		case overSoulSoil && n == worldgen.BlueIce:
			to = worldgen.Basalt
		default:
			continue
		}
		h.setBlockAt(players, dim, pos, to)
		h.fizz(players, dim, pos)
		h.scheduleAroundIn(dim, pos, 1) // setBlockAndUpdate: the neighbours hear it
		return true
	}
	return false
}

// liquidNotifyNeighbors is the neighbour half of a fluid tick's own
// setBlockAndUpdate: a water or lava neighbour hears it at once (and asks
// for its tick); any other neighbour gets the simulation's next-tick look,
// as a neighbour update to a block with no redstone behaviour does.
func (h *hub) liquidNotifyNeighbors(players map[int32]*tracked, dim int, pos blockPos) {
	w := h.worldFor(dim)
	for _, d := range allNeighbors {
		n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
		if !h.inWorldYIn(dim, n.y) {
			continue
		}
		if s := w.Block(n.x, n.y, n.z); isLiquidBlock(s) {
			h.liquidOnPlace(players, dim, n, s)
			continue
		}
		h.scheduleIn(dim, n, 1)
	}
}
