package server

import (
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Pistons. A powered piston pushes the structure in front of it one cell
// along its facing and grows a piston_head; unpowering retracts, and a
// sticky piston pulls the structure back. The structure is vanilla's
// (PistonStructureResolver, pistonmove.go): the straight line ahead plus
// whatever slime and honey blocks in it stick to, up to 12 blocks, with the
// push reactions of pistonreact.go. Moved blocks travel as moving_piston
// cells for two ticks (movingpiston.go), and anything standing where a
// block arrives is shoved along with it.

const (
	pistonMaxPush = 12
)

var (
	stickyPistonMin = worldgen.BlockBase("sticky_piston")
	stickyPistonMax = worldgen.BlockBase("sticky_piston") + 11
	pistonMin       = worldgen.BlockBase("piston")
	pistonMax       = worldgen.BlockBase("piston") + 11
	pistonHeadMin   = worldgen.BlockBase("piston_head")
	pistonHeadMax   = worldgen.BlockBase("piston_head") + 23

	obsidianState = worldgen.BlockBase("obsidian")

	dispenserMin = worldgen.BlockBase("dispenser") // facing(6) × triggered(2)
	dispenserMax = worldgen.BlockBase("dispenser") + 11
	dropperMin   = worldgen.BlockBase("dropper")
	dropperMax   = worldgen.BlockBase("dropper") + 11
)

func isDispenser(s uint32) bool { return s >= dispenserMin && s <= dispenserMax }
func isDropper(s uint32) bool   { return s >= dropperMin && s <= dropperMax }

// sixWayFacing: blocks whose placement uses the full up/down facing set.
func sixWayFacing(s uint32) bool {
	return isPistonBase(s) || isObserver(s) || isDispenser(s) || isDropper(s) || isBarrel(s)
}

func isPistonBase(s uint32) bool {
	return (s >= pistonMin && s <= pistonMax) || (s >= stickyPistonMin && s <= stickyPistonMax)
}
func isSticky(s uint32) bool     { return s >= stickyPistonMin && s <= stickyPistonMax }
func isPistonHead(s uint32) bool { return s >= pistonHeadMin && s <= pistonHeadMax }

// pistonDelta is the base's facing as a 3D delta (6-way, like observers).
func pistonDelta(s uint32) (int, int, int) {
	switch stateFacing(s) {
	case "up":
		return 0, 1, 0
	case "down":
		return 0, -1, 0
	}
	dx, dz := facingDelta(stateFacing(s))
	return dx, 0, dz
}

// pistonHeadBase is where a head's base stands, when the head is still on it
// (PistonHeadBlock.canSurvive): an extended piston of the head's own type
// and facing, or the moving_piston cell of one mid-retraction.
func pistonHeadBase(w *world.World, pos blockPos, head uint32) (blockPos, bool) {
	dx, dy, dz := pistonDelta(head)
	at := blockPos{pos.x - dx, pos.y - dy, pos.z - dz}
	base := w.At(at.x, at.y, at.z)
	switch {
	case isPistonBase(base):
		ok := isSticky(base) == headIsSticky(head) && boolProp(base, "extended") && stateFacing(base) == stateFacing(head)
		return at, ok
	case isMovingPiston(base):
		return at, stateFacing(base) == stateFacing(head)
	}
	return at, false
}

// headIsSticky reads a piston head's type: sticky or normal.
func headIsSticky(head uint32) bool {
	info, ok := worldgen.InfoForState(head)
	return ok && worldgen.GetProperty(info, head, "type") == "sticky"
}

// headFor builds the piston_head state matching a base: [facing(6), short(2),
// type(2)], short=false; facing order north,east,south,west,up,down.
func headFor(base uint32) uint32 {
	fIdx := map[string]uint32{"north": 0, "east": 1, "south": 2, "west": 3, "up": 4, "down": 5}[stateFacing(base)]
	head := pistonHeadMin + fIdx*4 + 2 // short=false
	if isSticky(base) {
		head++
	}
	return head
}

// pistonPowered is PistonBaseBlock.getNeighborSignal (signal.go): any side
// but the push face, then quasi-connectivity through the block above.
func (h *hub) pistonPowered(pos blockPos, state uint32) bool {
	dx, dy, dz := pistonDelta(state)
	if push, ok := dirFromDelta(dx, dy, dz); ok {
		return h.pistonHasSignal(pos, push)
	}
	return false
}

// updatePiston is PistonBaseBlock.checkIfExtend: a piston whose power and
// extension disagree queues a block event — extend or retract — and moves
// when the block events run, at the end of the tick's block updates
// (queueBlockEvent). An extension that cannot move its blocks is not queued.
func (h *hub) updatePiston(players map[int32]*tracked, pos blockPos, state uint32) {
	powered := h.pistonPowered(pos, state)
	extended := boolProp(state, "extended")
	switch {
	case powered && !extended:
		dx, dy, dz := pistonDelta(state)
		if h.pistonCanMove(pos, [3]int{dx, dy, dz}) {
			h.queueBlockEvent(pos, true)
		}
	case !powered && extended:
		h.queueBlockEvent(pos, false)
	}
}

// pistonCanMove is checkIfExtend's PistonStructureResolver.resolve: would
// an extension move (or break) what is in front, without doing it.
func (h *hub) pistonCanMove(pos blockPos, dir [3]int) bool {
	r := &pistonResolver{h: h, pistonPos: pos, startPos: stepPos(pos, dir, 1), push: dir, extending: true}
	return r.resolve()
}

// pistonEvent is PistonBaseBlock.triggerEvent: the power is read again, so
// an extension whose power is already gone does nothing, and a retraction
// whose power came back leaves the piston out.
func (h *hub) pistonEvent(players map[int32]*tracked, pos blockPos, extend bool) {
	state := h.rsWorld().At(pos.x, pos.y, pos.z)
	if !isPistonBase(state) {
		return
	}
	powered := h.pistonPowered(pos, state)
	extended := boolProp(state, "extended")
	dx, dy, dz := pistonDelta(state)
	// The move writes many cells; like vanilla's moveBlocks (which sets them
	// without neighbour updates and updates afterwards), nothing reacts to a
	// half-moved structure: the updates it causes are held until it is done.
	held := h.nb.running
	h.nb.running = true
	defer func() {
		h.nb.running = held
		h.nbRun(players)
	}()
	switch {
	case extend && powered && !extended:
		h.extendPiston(players, pos, state, [3]int{dx, dy, dz})
	case !extend && !powered && extended:
		h.retractPiston(players, pos, state, [3]int{dx, dy, dz})
	}
}

// extendPiston moves the structure ahead one cell and grows the head.
func (h *hub) extendPiston(players map[int32]*tracked, pos blockPos, state uint32, dir [3]int) {
	if !h.movePistonBlocks(players, pos, dir, true) {
		return // blocked: stay retracted
	}
	h.rsSet(players, pos, setBoolProp(state, "extended", true))
	h.rsSound(players, "minecraft:block.piston.extend", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleSignalAround(players, pos)
}

// retractPiston pulls the head back into the base (the base itself is a
// moving cell for two ticks, carrying the retracted piston); a sticky
// piston first tries to pull the structure beyond it back, or catches a
// block still sliding toward it (PistonBaseBlock.triggerEvent, retract
// branch).
func (h *hub) retractPiston(players map[int32]*tracked, pos blockPos, state uint32, dir [3]int) {
	head := blockPos{pos.x + dir[0], pos.y + dir[1], pos.z + dir[2]}
	h.finalTickMoving(players, head) // a head still extending lands first
	h.placeMoving(players, pos, movingPistonState(dir, isSticky(state)),
		movingBlock{moved: setBoolProp(state, "extended", false), facing: dir, source: true})
	pulled := false
	if isSticky(state) {
		beyond := blockPos{head.x + dir[0], head.y + dir[1], head.z + dir[2]}
		if mb, ok := h.movingBlocks[simPos{dim: h.rsDim, blockPos: beyond}]; ok && mb.facing == dir && mb.extending {
			h.finalTickMoving(players, beyond) // caught mid-push: it lands where it is
			pulled = true
		}
		s := h.rsWorld().At(beyond.x, beyond.y, beyond.z)
		back := [3]int{-dir[0], -dir[1], -dir[2]}
		if !pulled && s != worldgen.Air && h.pistonPushable(s, beyond.y, back, false, dir) &&
			(pushReactionOf(s) == pushNormal || isPistonBase(s)) {
			pulled = h.movePistonBlocks(players, pos, dir, false)
		}
	}
	if !pulled && isPistonHead(h.rsWorld().At(head.x, head.y, head.z)) {
		h.rsSet(players, head, worldgen.Air)
	}
	h.rsSound(players, "minecraft:block.piston.contract", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleSignalAround(players, pos)
}
