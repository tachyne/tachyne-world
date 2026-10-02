package server

// Typed scheduled block ticks on the simulation queue.
//
// Vanilla keeps two different things apart that the simulation queue
// (sim.go pending) used to run the same way:
//
//   - A SCHEDULED TICK (Level.scheduleTick → LevelTicks): a ScheduledTick
//     keyed by (pos, block) with a trigger tick, a priority and a sub-tick
//     order. A second schedule for a (pos, block) already pending is
//     ignored (LevelChunkTicks.schedule), and when it comes due it runs
//     only if that block is still there (ServerLevel.tickBlock) — then it
//     is the block's tick(): frosted ice ageing, a coral bleaching, an
//     eyeblossom switching.
//   - A NEIGHBOUR REACTION (neighborChanged / updateShape), which vanilla
//     runs at once inside the change that caused it. It never runs the
//     block's tick; at most it asks for one.
//
// The redstone family has its own LevelTicks (blockticks.go bticks) and the
// fluids theirs (liquidblock.go fluidTicks). This is the rest of the block
// list: a scheduled tick is marked here, with the block it is for, and the
// queue entry that reaches the cell on its trigger tick runs as the tick;
// every other entry reaching the cell is a neighbour reaction. The
// reactions keep the queue's one-tick look (scheduleIn/scheduleAroundIn at
// fallDelay/1) — the queue's stand-in for vanilla's immediate update.
//
// The call sites, sorted:
//
//   - scheduled ticks (this API): frosted ice (onPlace 60-120, retry
//     20-40), a dry coral's die tick (60-99), an eyeblossom's ripple.
//     Ticks that keep their own due map (frogspawn, sniffer egg,
//     composter, dried ghast, dripleaf, bubble source, fire, target) are
//     scheduled ticks already — a neighbour reaction reaching them before
//     their due tick does not run them — and are not yet moved onto this
//     API (dripleaf and target overwrite a pending tick's due rather than
//     keeping it).
//   - fluid ticks: scheduleFluidTick (liquidblock.go).
//   - neighbour reactions (scheduleIn/scheduleAroundIn at 1): a fluid
//     tick's or a removal's setBlockAndUpdate (liquidNotifyNeighbors,
//     lavaMeetsWater, hub.go block edits, enderman take/place, fire burning
//     a block away, a portal popping, a rail slope's top, an explosion's
//     cell, leaf distance and decay, cactus/sugar cane growth), the water
//     a melt leaves (ice, frosted ice: waterDelay, the fluid's own delay),
//     neighbour updates with no redstone behaviour (blockticks.go), and the
//     restart re-checks (fluidprime.go, redstonerepair.go) that stand in
//     for the ticks vanilla saves with the chunk.
//
// Priority: every tick here is NORMAL, run in scheduling order (the
// queue's bucket order) — the redstone list sorts its own priorities, and
// runs before this one in a tick.

// simTickMark is a pending scheduled tick's identity: the block (its first
// state, blockKind) and the trigger tick.
type simTickMark struct {
	kind uint32
	due  uint64
}

// scheduleBlockTickIn is Level.scheduleTick(pos, block, delay) for the block
// at pos in dim: the block's tick runs delay ticks from now, unless a tick
// for the same block is already pending there, which it keeps. It reports
// whether a tick was added.
func (h *hub) scheduleBlockTickIn(dim int, pos blockPos, delay uint64) bool {
	if dim == 0 && !h.ownedBlock(pos.x, pos.z) {
		return false
	}
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	kind := blockKind(w.At(pos.x, pos.y, pos.z))
	key := simPos{dim: dim, blockPos: pos}
	now := h.tick.Load()
	if m, ok := h.simTicks[key]; ok && m.kind == kind && m.due >= now {
		return false // one is pending (a mark whose tick passed is stale)
	}
	if delay == 0 {
		delay = 1
	}
	if h.simTicks == nil {
		h.simTicks = map[simPos]simTickMark{}
	}
	// A tick for another block that was here is replaced: vanilla would
	// keep it and drop it when it came due, its block gone.
	h.simTicks[key] = simTickMark{kind: kind, due: now + delay}
	h.scheduleIn(dim, pos, delay)
	return true
}

// hasBlockTickIn is LevelTicks.hasScheduledTick(pos, block) for the block
// now at pos.
func (h *hub) hasBlockTickIn(dim int, pos blockPos, state uint32) bool {
	m, ok := h.simTicks[simPos{dim: dim, blockPos: pos}]
	return ok && m.due >= h.tick.Load() && m.kind == blockKind(state)
}

// takeBlockTick reports whether the update being run at pos is the cell's
// scheduled tick, for the block (state) still there, and clears the mark.
// A tick whose block has gone is cleared and reports false: the update
// runs as a neighbour reaction at most.
func (h *hub) takeBlockTick(dim int, pos blockPos, state uint32) bool {
	key := simPos{dim: dim, blockPos: pos}
	m, ok := h.simTicks[key]
	if !ok || m.due > h.tick.Load() {
		return false
	}
	delete(h.simTicks, key)
	return m.kind == blockKind(state)
}

// blockTickMoved follows a queued update the simulation put off (its chunk
// is not ticking, or the list's cap was reached): if it was the cell's
// scheduled tick, the mark moves with it.
func (h *hub) blockTickMoved(sp simPos, age, to uint64) {
	if m, ok := h.simTicks[sp]; ok && m.due <= age {
		m.due = to
		h.simTicks[sp] = m
	}
}
