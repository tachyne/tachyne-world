package server

import (
	"log"
	"sort"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Vanilla's two kinds of block update, kept apart the way the game keeps
// them — redstone timing is nothing but the difference between the two:
//
//   - A NEIGHBOUR UPDATE (Level.updateNeighborsAt through the collecting
//     neighbour updater) is immediate: a block that changes tells the blocks
//     around it within the same tick, and they react before the tick moves
//     on. Dust re-powers, a lamp lights, a repeater or torch decides it has
//     to change and schedules itself.
//   - A SCHEDULED TICK (LevelTicks) is the delay: a torch's 2, a repeater's
//     delay×2, a lamp's 4 to go dark. Ticks due in a game tick run in
//     priority order, then in the order they were scheduled, and a block
//     that already has a tick pending cannot schedule a second one.
//
// Before this split the engine had one queue for both, and every neighbour
// notification waited a tick: a lamp at the end of dust lit two ticks late
// and a line of repeaters ran at 3 ticks a stage instead of 2.
//
// The fluid, falling-block, fire and growth simulation keeps the older
// hub.pending queue (sim.go) and its timing: a neighbour update that reaches
// a block with no redstone behaviour is handed to that queue for the next
// tick, exactly as before.
//
// Everything here runs on the hub goroutine.

// Tick priorities (TickPriority): lower runs first within a tick.
const (
	tickExtremelyHigh int8 = -3
	tickVeryHigh      int8 = -2
	tickHigh          int8 = -1
	tickNormal        int8 = 0
)

// maxBlockTicksPerTick is ServerLevel's per-tick cap on scheduled block
// ticks (blockTicks.tick(time, 65536, …)); the rest wait for the next tick.
const maxBlockTicksPerTick = 65536

// maxChainedNeighborUpdates bounds one cascade of neighbour updates. Vanilla
// allows a million (max-chained-neighbor-updates) and skips the rest; the hub
// goroutine carries the whole server, so the bound here is lower — still
// hundreds of times what any real contraption uses in one tick.
const maxChainedNeighborUpdates = 1 << 18

// maxBlockEventsPerTick bounds the block-event pass (pistons re-queue each
// other through it).
const maxBlockEventsPerTick = 65536

// schedTick is one pending scheduled tick (ScheduledTick).
type schedTick struct {
	pos   simPos
	at    uint64 // trigger tick
	kind  uint32 // the block it was scheduled for: it runs only if that block is still there
	prio  int8
	order uint64 // subTickOrder: scheduling order, the last tie-break
}

// blockTickQueue is LevelTicks for the redstone family.
type blockTickQueue struct {
	due     map[uint64][]schedTick // by trigger tick
	pending map[simPos]uint64      // hasScheduledTick: pos → trigger tick
	running map[simPos]struct{}    // willTickThisTick: due now, not yet run
	seq     uint64
	run     []schedTick // scratch for the tick being run
}

// blockKind is the block a state belongs to (its first state id): the
// "type" half of a scheduled tick's identity.
func blockKind(s uint32) uint32 {
	if name, ok := worldgen.StateName(s); ok {
		lo, _ := worldgen.BlockRange(name)
		return lo
	}
	return s
}

// scheduleTick is Level.scheduleTick for the block now at pos in the current
// simulation dimension. A block with a tick already pending keeps that one
// (LevelChunkTicks.schedule: one tick per position and block).
func (h *hub) scheduleTick(pos blockPos, delay uint64, prio int8) {
	dim := h.rsDim
	if dim == 0 && !h.ownedBlock(pos.x, pos.z) {
		return
	}
	q := &h.bticks
	if q.due == nil {
		q.due = map[uint64][]schedTick{}
		q.pending = map[simPos]uint64{}
		q.running = map[simPos]struct{}{}
	}
	key := simPos{dim: dim, blockPos: pos}
	if _, ok := q.pending[key]; ok {
		return
	}
	if delay == 0 {
		delay = 1
	}
	at := h.tick.Load() + delay
	q.seq++
	q.pending[key] = at
	q.due[at] = append(q.due[at], schedTick{pos: key, at: at, kind: blockKind(h.rsWorld().At(pos.x, pos.y, pos.z)), prio: prio, order: q.seq})
}

// hasScheduledTick is LevelTicks.hasScheduledTick (a future tick; one that
// is running this tick has already left the set).
func (h *hub) hasScheduledTick(pos blockPos) bool {
	_, ok := h.bticks.pending[h.rsKey(pos)]
	return ok
}

// willTickThisTick is LevelTicks.willTickThisTick: collected for this tick
// and not yet run.
func (h *hub) willTickThisTick(pos blockPos) bool {
	_, ok := h.bticks.running[h.rsKey(pos)]
	return ok
}

// runBlockTicks runs the scheduled ticks due at age (LevelTicks.tick): all of
// them are collected first — from then on they no longer count as pending,
// but as "will tick this tick" until each one runs — then run by priority and
// scheduling order.
func (h *hub) runBlockTicks(players map[int32]*tracked, age uint64) {
	q := &h.bticks
	if len(q.due) == 0 {
		return
	}
	// Everything due by now: normally just this tick's bucket, but a tick
	// the clock jumped past (tests set it directly) is still owed.
	run := q.run[:0]
	for at, list := range q.due {
		if at <= age {
			run = append(run, list...)
			delete(q.due, at)
		}
	}
	if len(run) == 0 {
		q.run = run
		return
	}
	sort.SliceStable(run, func(i, j int) bool {
		if run[i].at != run[j].at {
			return run[i].at < run[j].at
		}
		if run[i].prio != run[j].prio {
			return run[i].prio < run[j].prio
		}
		return run[i].order < run[j].order
	})
	if len(run) > maxBlockTicksPerTick {
		// Over the cap: the rest keep their priority and order and run first
		// next tick (vanilla leaves them in their containers, already due).
		rest := run[maxBlockTicksPerTick:]
		q.due[age+1] = append(append([]schedTick(nil), rest...), q.due[age+1]...)
		for _, t := range rest {
			q.pending[t.pos] = age + 1
		}
		run = run[:maxBlockTicksPerTick]
	}
	for _, t := range run {
		delete(q.pending, t.pos)
		q.running[t.pos] = struct{}{}
	}
	for _, t := range run {
		delete(q.running, t.pos)
		if !h.inWorldYIn(t.pos.dim, t.pos.y) || h.worldFor(t.pos.dim) == nil {
			continue
		}
		if !h.canTickBlocksAt(t.pos) {
			// Its chunk stopped ticking: the tick waits with it.
			if _, ok := q.pending[t.pos]; !ok {
				at := age + unloadedRetry
				q.pending[t.pos] = at
				q.due[at] = append(q.due[at], t)
			}
			continue
		}
		state := h.worldFor(t.pos.dim).At(t.pos.x, t.pos.y, t.pos.z)
		if blockKind(state) != t.kind {
			continue // ServerLevel.tickBlock: the block it was for is gone
		}
		h.inDim(t.pos.dim, func() { h.redstoneTick(players, t.pos.blockPos, state) })
	}
	clear(q.running)
	q.run = run[:0]
}

// ---- neighbour updates ----------------------------------------------------

// neighborUpdater is CollectingNeighborUpdater: updates added while one is
// running are collected into a layer and run, in the order they were added,
// before the rest of the cascade continues — a depth-first walk.
type neighborUpdater struct {
	stack   []simPos
	layer   []simPos
	running bool
	warned  bool
}

// nbAdd queues a neighbour update at pos in the current simulation dimension.
func (h *hub) nbAdd(pos blockPos) {
	h.nb.layer = append(h.nb.layer, simPos{dim: h.rsDim, blockPos: pos})
}

// updateOrder is NeighborUpdater.UPDATE_ORDER.
var updateOrder = [6]rsDir{dWest, dEast, dDown, dUp, dNorth, dSouth}

// nbAround queues updates for the six neighbours of pos (updateNeighborsAt),
// optionally skipping one direction (updateNeighborsAtExceptFromFacing).
func (h *hub) nbAround(pos blockPos, skip rsDir, hasSkip bool) {
	for _, d := range updateOrder {
		if hasSkip && d == skip {
			continue
		}
		dx, dy, dz := d.delta()
		h.nbAdd(blockPos{pos.x + dx, pos.y + dy, pos.z + dz})
	}
}

// nbRun runs queued neighbour updates to the end of the cascade, unless a
// cascade is already running (it picks the new ones up). Every entry point
// that queues updates finishes with it.
func (h *hub) nbRun(players map[int32]*tracked) {
	u := &h.nb
	if u.running {
		return
	}
	u.running = true
	count := 0
	for len(u.layer) > 0 || len(u.stack) > 0 {
		for i := len(u.layer) - 1; i >= 0; i-- {
			u.stack = append(u.stack, u.layer[i])
		}
		u.layer = u.layer[:0]
		sp := u.stack[len(u.stack)-1]
		u.stack = u.stack[:len(u.stack)-1]
		if count++; count > maxChainedNeighborUpdates {
			if !u.warned {
				u.warned = true
				log.Printf("redstone: too many chained neighbour updates, skipping the rest (first skipped at %v dim %d)", sp.blockPos, sp.dim)
			}
			u.stack = u.stack[:0]
			u.layer = u.layer[:0]
			break
		}
		h.neighborChanged(players, sp)
	}
	u.running = false
}

// reactsToNeighbors reports whether a block's neighborChanged does anything
// the redstone family models — those answer at once. Everything else keeps
// the simulation queue's next-tick re-check (fluids, falling blocks, fire…).
func reactsToNeighbors(s uint32) bool {
	switch {
	case isWire(s), isRSTorch(s), isLamp(s), isButton(s), isTNT(s),
		isRepeater(s), isComparator(s), isObserver(s), isPistonBase(s),
		isDispenser(s), isDropper(s), isNoteBlock(s), isCrafter(s),
		worldgen.IsCopperBulb(s), isBell(s), isAnyRail(s):
		return true
	}
	return isPowerOpenable(s)
}

// isPowerOpenable is a door, trapdoor or fence gate: open + powered.
func isPowerOpenable(s uint32) bool {
	if s == worldgen.Air {
		return false
	}
	info, ok := worldgen.InfoForState(s)
	return ok && info.HasProperty("open") && info.HasProperty("powered")
}

// neighborChanged delivers one neighbour update.
func (h *hub) neighborChanged(players map[int32]*tracked, sp simPos) {
	if !h.inWorldYIn(sp.dim, sp.y) {
		return
	}
	w := h.worldFor(sp.dim)
	if w == nil || (sp.dim == 0 && !h.ownedBlock(sp.x, sp.z)) {
		return
	}
	if !w.Loaded(int32(sp.x>>4), int32(sp.z>>4)) {
		// Never generate a chunk from a neighbour update: hand it to the
		// simulation queue, which waits for the chunk to tick.
		h.scheduleIn(sp.dim, sp.blockPos, 1)
		return
	}
	st := w.At(sp.x, sp.y, sp.z)
	if reactsToNeighbors(st) {
		h.inDim(sp.dim, func() { h.updateRedstone(players, sp.blockPos, st) })
		if worldgen.IsWaterlogged(st) {
			h.scheduleIn(sp.dim, sp.blockPos, 1) // its water's own re-check
		}
		return
	}
	if isHopper(st) {
		h.hopperPowerCheck(players, sp, st) // HopperBlock.neighborChanged: ENABLED follows power at once
	}
	if isPistonHead(st) {
		// PistonHeadBlock.neighborChanged: the head passes the update on to
		// the base it belongs to, which reads its power around the head too.
		if base, ok := pistonHeadBase(w, sp.blockPos, st); ok {
			h.inDim(sp.dim, func() { h.nbAdd(base) })
		}
	}
	h.scheduleIn(sp.dim, sp.blockPos, 1)
	// The quasi-connectivity relay (updateRedstone does the same for the
	// redstone blocks): an update at the cell above a piston reaches it.
	if below := w.At(sp.x, sp.y-1, sp.z); isPistonBase(below) && !isPistonBase(st) {
		h.inDim(sp.dim, func() { h.nbAdd(blockPos{sp.x, sp.y - 1, sp.z}) })
	}
}

// notifyAround is a block change's own notification: the cell itself and
// its six neighbours hear of it now (redstone) or next tick (the rest of
// the simulation, as before).
func (h *hub) notifyAround(players map[int32]*tracked, dim int, pos blockPos) {
	h.inDim(dim, func() {
		h.nbAdd(pos)
		h.nbAround(pos, 0, false)
	})
	h.nbRun(players)
}

// updateNeighborsInFront is DiodeBlock/ObserverBlock.updateNeighborsInFront:
// the block the output faces, then that block's other neighbours.
func (h *hub) updateNeighborsInFront(players map[int32]*tracked, pos blockPos, state uint32) {
	f, ok := propDir(state, "facing")
	if !ok {
		h.scheduleSignalAround(players, pos)
		return
	}
	dx, dy, dz := f.opposite().delta()
	front := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	h.nbAdd(front)
	h.nbAround(front, f, true)
	h.nbRun(players)
}

// ---- observers ------------------------------------------------------------

// observersSee is the shape update an observer gets when the block it
// watches changes (ObserverBlock.updateShape): any change, however it was
// written. Only observers the hub has already looked at are checked — every
// observer is looked at when it is placed, moved or loaded — so the cost on
// the hot setBlockAt path is a few map probes.
func (h *hub) observersSee(players map[int32]*tracked, dim int, pos blockPos, now uint32) {
	if len(h.obsSeen) == 0 {
		return
	}
	for d := dDown; d <= dEast; d++ {
		dx, dy, dz := d.delta()
		op := simPos{dim: dim, blockPos: blockPos{pos.x + dx, pos.y + dy, pos.z + dz}}
		if _, ok := h.obsSeen[op]; !ok {
			continue
		}
		s := h.worldFor(dim).At(op.x, op.y, op.z)
		if !isObserver(s) {
			delete(h.obsSeen, op)
			continue
		}
		wx, wy, wz := obsDelta(s)
		if op.x+wx != pos.x || op.y+wy != pos.y || op.z+wz != pos.z {
			continue // it watches another side
		}
		h.obsSeen[op] = now
		if !boolProp(s, "powered") {
			h.inDim(dim, func() { h.observerStart(op.blockPos) })
		}
	}
}

// observerStart is ObserverBlock.startSignal.
func (h *hub) observerStart(pos blockPos) {
	if !h.hasScheduledTick(pos) {
		h.scheduleTick(pos, observerPulseTicks, tickNormal)
	}
}

// ---- block events -----------------------------------------------------------

// blockEvent is a queued Level.blockEvent: a piston's move, or a powered
// note block's note.
type blockEvent struct {
	pos    simPos
	extend bool
	note   bool
}

// queueBlockEvent is Level.blockEvent: it runs at the end of the tick's
// block updates (ServerLevel.runBlockEvents) — the next tick's, for one
// queued by a player's click between ticks. Identical events collapse.
func (h *hub) queueBlockEvent(pos blockPos, extend bool) {
	ev := blockEvent{pos: h.rsKey(pos), extend: extend}
	for _, e := range h.blockEvents {
		if e == ev {
			return
		}
	}
	h.blockEvents = append(h.blockEvents, ev)
}

// runBlockEvents is ServerLevel.runBlockEvents: every queued event, including
// those queued while it runs.
func (h *hub) runBlockEvents(players map[int32]*tracked) {
	for n := 0; len(h.blockEvents) > 0 && n < maxBlockEventsPerTick; n++ {
		ev := h.blockEvents[0]
		h.blockEvents = h.blockEvents[1:]
		if !h.inWorldYIn(ev.pos.dim, ev.pos.y) || h.worldFor(ev.pos.dim) == nil || !h.canTickBlocksAt(ev.pos) {
			continue
		}
		if ev.note {
			// NoteBlock.triggerEvent: the note sounds as the event runs, from
			// whatever note block is there by then.
			if s := h.worldFor(ev.pos.dim).At(ev.pos.x, ev.pos.y, ev.pos.z); isNoteBlock(s) {
				h.playNoteBlock(players, ev.pos.dim, ev.pos.x, ev.pos.y, ev.pos.z, s, 0)
			}
			continue
		}
		h.inDim(ev.pos.dim, func() { h.pistonEvent(players, ev.pos.blockPos, ev.extend) })
	}
	if len(h.blockEvents) == 0 {
		h.blockEvents = nil
	}
}

// queueNoteEvent is NoteBlock.playNote's level.blockEvent: the note plays at
// the end of the tick's block updates.
func (h *hub) queueNoteEvent(pos blockPos) {
	ev := blockEvent{pos: h.rsKey(pos), note: true}
	for _, e := range h.blockEvents {
		if e == ev {
			return
		}
	}
	h.blockEvents = append(h.blockEvents, ev)
}
