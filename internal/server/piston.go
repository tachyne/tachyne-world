package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Pistons. A powered piston pushes the structure in front of it one cell
// along its facing and grows a piston_head; unpowering retracts, and a
// sticky piston pulls the structure back. The structure is vanilla's
// (PistonStructureResolver, pistonmove.go): the straight line ahead plus
// whatever slime and honey blocks in it stick to, up to 12 blocks, with the
// push reactions of pistonreact.go. Block movement is still instant — there
// is no moving_piston animation block — but anything standing where a block
// arrives is shoved along with it.

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

// updatePiston reacts to power: extend when powered, retract when not.
func (h *hub) updatePiston(players map[int32]*tracked, pos blockPos, state uint32) {
	dx, dy, dz := pistonDelta(state)
	// PistonBaseBlock.getNeighborSignal (signal.go): any side but the push
	// face, then quasi-connectivity through the block above.
	powered := false
	if push, ok := dirFromDelta(dx, dy, dz); ok {
		powered = h.pistonHasSignal(pos, push)
	}
	extended := boolProp(state, "extended")
	if powered && !extended {
		h.extendPiston(players, pos, state, [3]int{dx, dy, dz})
	} else if !powered && extended {
		h.retractPiston(players, pos, state, [3]int{dx, dy, dz})
	}
}

// extendPiston moves the structure ahead one cell and grows the head.
func (h *hub) extendPiston(players map[int32]*tracked, pos blockPos, state uint32, dir [3]int) {
	if !h.movePistonBlocks(players, pos, dir, true) {
		return // blocked: stay retracted
	}
	h.setBlock(players, pos, setBoolProp(state, "extended", true))
	h.playSound(players, "minecraft:block.piston.extend", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleSignalAround(pos)
}

// retractPiston removes the head; a sticky piston first tries to pull the
// structure beyond it back (PistonBaseBlock.triggerEvent, retract branch).
func (h *hub) retractPiston(players map[int32]*tracked, pos blockPos, state uint32, dir [3]int) {
	head := blockPos{pos.x + dir[0], pos.y + dir[1], pos.z + dir[2]}
	pulled := false
	if isSticky(state) {
		beyond := blockPos{head.x + dir[0], head.y + dir[1], head.z + dir[2]}
		s := h.world.At(beyond.x, beyond.y, beyond.z)
		back := [3]int{-dir[0], -dir[1], -dir[2]}
		if s != worldgen.Air && h.pistonPushable(s, beyond.y, back, false, dir) &&
			(pushReactionOf(s) == pushNormal || isPistonBase(s)) {
			pulled = h.movePistonBlocks(players, pos, dir, false)
		}
	}
	if !pulled && isPistonHead(h.world.At(head.x, head.y, head.z)) {
		h.setBlock(players, head, worldgen.Air)
	}
	h.setBlock(players, pos, setBoolProp(state, "extended", false))
	h.playSound(players, "minecraft:block.piston.contract", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 0.7)
	h.scheduleSignalAround(pos)
}
