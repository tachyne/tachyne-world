package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// affectNeighborsAfterRemoval. When a block of one kind is replaced by
// another, vanilla gives the old block a last word (BlockBehaviour.
// affectNeighborsAfterRemoval): a powered lever, button, torch, plate,
// wire, hook or observer tells the neighbours it powered — including the
// neighbours of the block it powered through — that the power is gone; a
// container tells the comparators reading it, even through a solid block;
// a sloped rail tells the rail above it; a piston head takes its base with
// it (PistonHeadBlock: destroyBlock), and a base gone from behind a head
// drops the head (its updateShape). The engine's writes all pass through
// here.

// sameBlockKind reports whether two states are states of one block.
func sameBlockKind(a, b uint32) bool {
	if a == b {
		return true
	}
	ia, oka := worldgen.InfoForState(a)
	ib, okb := worldgen.InfoForState(b)
	return oka && okb && ia.Min == ib.Min
}

// hasComparatorOutput: blocks whose block entity a comparator reads.
func hasComparatorOutput(s uint32) bool {
	return isChestBlock(s) || isBarrel(s) || isShulkerBox(s) || isCookerBlock(s) || isHopper(s) ||
		isDispenser(s) || isDropper(s) || isBrewStand(s) || isCrafter(s) || isJukebox(s) ||
		isLectern(s) || isDecoratedPot(s) || isBookshelf(s) || isWoodShelf(s)
}

// signalSource: blocks whose removal changes what their neighbours are
// powered by.
func signalSource(s uint32) bool {
	return isRedstoneish(s) || isLever(s) || isWire(s) || isTripwireHook(s) || isTripwire(s) ||
		isLightningRod(s) || isAnyRail(s)
}

// afterRemoval runs the old block's hook once `now` has replaced it at pos.
func (h *hub) afterRemoval(players map[int32]*tracked, dim int, pos blockPos, old, now uint32) {
	if old == worldgen.Air || sameBlockKind(old, now) {
		return
	}
	switch {
	case isPistonHead(old):
		// PistonHeadBlock.affectNeighborsAfterRemoval: the fitting base
		// behind is destroyed with its drop.
		dx, dz := facingDelta(stateFacing(old))
		dy := 0
		switch stateFacing(old) {
		case "up":
			dy = 1
		case "down":
			dy = -1
		}
		base := blockPos{pos.x - dx, pos.y - dy, pos.z - dz}
		if s := h.worldFor(dim).At(base.x, base.y, base.z); isPistonBase(s) && boolProp(s, "extended") && stateFacing(s) == stateFacing(old) {
			h.breakBlockDrop(players, dim, base, s)
		}
	case isPistonBase(old) && boolProp(old, "extended"):
		// PistonHeadBlock.updateShape: a head with no fitting base (nor a
		// moving base sliding it back) behind it does not survive.
		dx, dy, dz := pistonDelta(old)
		front := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
		if isMovingPiston(now) && stateFacing(now) == stateFacing(old) {
			break
		}
		if s := h.worldFor(dim).At(front.x, front.y, front.z); isPistonHead(s) && stateFacing(s) == stateFacing(old) {
			h.setBlockAt(players, dim, front, worldgen.Air)
		}
	}
	if dim != dimOverworld {
		return // redstone and comparators simulate the overworld only
	}
	if signalSource(old) {
		h.scheduleSignalAround(pos)
	}
	if isAnyRail(old) && railShape(old) >= 2 && railShape(old) <= 5 {
		h.scheduleAround(blockPos{pos.x, pos.y + 1, pos.z}, 1) // the rail this slope climbed to
	}
	if hasComparatorOutput(old) {
		h.updateNeighbourForOutputSignal(pos)
	}
}

// updateNeighbourForOutputSignal is Level.updateNeighbourForOutputSignal:
// each horizontal neighbour hears of the change, and a comparator one
// block further on, behind a solid neighbour, does too.
func (h *hub) updateNeighbourForOutputSignal(pos blockPos) {
	for d := dNorth; d <= dEast; d++ {
		dx, _, dz := d.delta()
		n := blockPos{pos.x + dx, pos.y, pos.z + dz}
		h.schedule(n, 1)
		if conducts(h.world.At(n.x, n.y, n.z)) {
			beyond := blockPos{n.x + dx, n.y, n.z + dz}
			if isComparator(h.world.At(beyond.x, beyond.y, beyond.z)) {
				h.schedule(beyond, 1)
			}
		}
	}
}
