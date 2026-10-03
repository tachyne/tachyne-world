package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Scheduled block ticks for the blocks outside the redstone family.
//
// Vanilla keeps two different things apart:
//
//   - A SCHEDULED TICK (Level.scheduleTick → LevelTicks): a ScheduledTick
//     keyed by (pos, block) with a trigger tick, a priority and a sub-tick
//     order. A second schedule for a (pos, block) already pending is
//     ignored (LevelChunkTicks.schedule), and when it comes due it runs
//     only if that block is still there (ServerLevel.tickBlock) — then it
//     is the block's tick(): frosted ice ageing, a coral bleaching, a
//     clutch of frogspawn hatching.
//   - A NEIGHBOUR REACTION (neighborChanged / updateShape), which vanilla
//     runs at once inside the change that caused it. It never runs the
//     block's tick; at most it asks for one.
//
// Every block's scheduled tick goes on the one block tick list
// (blockticks.go bticks), which runs them in vanilla's order — trigger
// tick, then priority, then the order they were scheduled — before the
// fluid ticks (liquidblock.go), as ServerLevel.tick runs blockTicks then
// fluidTicks. simBlockTick below is the tick() of the blocks here; the
// redstone family's is redstoneTick.
//
// None of these ticks is saved: vanilla keeps them with the chunk, this
// engine in memory. A block that would sit forever without its tick is
// booked again — on the first look at its chunk after a restart
// (fluidprime.go) or the first neighbour change that reaches it.

// scheduleBlockTickIn is Level.scheduleTick(pos, block, delay) for the block
// at pos in dim, at NORMAL priority: the block's tick runs delay ticks from
// now, unless a tick for the same block is already pending there, which it
// keeps. It reports whether a tick was added.
func (h *hub) scheduleBlockTickIn(dim int, pos blockPos, delay uint64) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	return h.scheduleTickFor(dim, pos, blockKind(w.At(pos.x, pos.y, pos.z)), delay, tickNormal)
}

// hasBlockTickIn is LevelTicks.hasScheduledTick(pos, block) for the block
// state's block at pos.
func (h *hub) hasBlockTickIn(dim int, pos blockPos, state uint32) bool {
	_, ok := h.bticks.pending[tickKey{simPos{dim: dim, blockPos: pos}, blockKind(state)}]
	return ok
}

// blockTickDue is the trigger tick of the pending tick for state's block at
// pos, if there is one.
func (h *hub) blockTickDue(dim int, pos blockPos, state uint32) (uint64, bool) {
	at, ok := h.bticks.pending[tickKey{simPos{dim: dim, blockPos: pos}, blockKind(state)}]
	return at, ok
}

// simBlockTick runs the scheduled tick of a block outside the redstone
// family (the block it was scheduled for is still there: runBlockTicks
// checked). It reports whether the block was one of these.
func (h *hub) simBlockTick(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	switch {
	case state == openEyeblossom || state == closedEyeblossom:
		// A flower woken by the one that turned beside it: it switches with
		// the SHORT sound, and wakes its own neighbours in turn.
		h.switchEyeblossom(players, dim, pos.x, pos.y, pos.z, state, false)
	case isFrostedIce(state):
		h.tickFrostedIce(players, dim, pos, state)
	case isLiquidBlock(state):
		h.tickBubbleWater(players, dim, pos, state) // LiquidBlock.tick: the bubble column
	case isFire(state):
		if state != soulFire { // SoulFireBlock has no tick
			h.inDim(dim, func() { h.updateFire(players, pos) })
		}
	case state == frogspawnBlock:
		h.tickFrogspawn(players, dim, pos, state)
	case isSnifferEgg(state):
		h.tickSnifferEgg(players, dim, pos.x, pos.y, pos.z, state)
	case isDriedGhast(state):
		h.driedGhastStep(players, dim, pos, state)
	case isBigDripleaf(state):
		h.tickDripleaf(players, dim, pos, state)
	case isTarget(state):
		h.inDim(dim, func() { h.tickTarget(players, pos, state) })
	case isLeaf(state):
		// LeavesBlock.tick: the distance recomputed, written with a
		// neighbour update — the next leaf's tick, a tick later: the wave.
		h.updateLeafDistance(players, dim, pos.x, pos.y, pos.z, state)
	case isChorusPlant(state):
		h.tickChorusPlant(players, dim, pos.x, pos.y, pos.z, state) // ChorusPlantBlock.tick: no footing, it pops
	case isCreakingHeartBlock(state):
		h.creakingHeartTick(players, dim, pos, state)
	case worldgen.IsBubbleColumn(state):
		h.updateBubbleColumn(players, dim, pos) // BubbleColumnBlock.tick: updateColumn
	default:
		if _, ok := coralDead[state]; ok {
			h.tickCoral(players, dim, pos, state, true)
			return true
		}
		if _, ok := composterLevel(state); ok {
			h.tickComposter(players, dim, pos, state)
			return true
		}
		return false
	}
	return true
}
