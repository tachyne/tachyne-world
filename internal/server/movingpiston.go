package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Moving pistons. A piston does not swap its blocks into place at once:
// each cell a block is heading for holds a moving_piston block for two
// ticks with a PistonMovingBlockEntity naming the block it carries, the
// piston's facing and whether it is extending, and clients draw the block
// sliding in (PistonMovingBlockEntity.tick: progress 0 → 0.5 → 1, then
// the real block is laid down). The extending head and the retracting base
// are moving cells too, marked as the source. The hub keeps the record per
// cell and finishes it from the block-update schedule.

const movingPistonTicks = 2 // progress 0.5 per tick

var (
	movingPistonMin = worldgen.BlockBase("moving_piston")
	movingPistonMax = worldgen.BlockBase("moving_piston") + 11 // facing(6) × type(2)
)

func isMovingPiston(s uint32) bool { return s >= movingPistonMin && s <= movingPistonMax }

// movingBlock is what one moving_piston cell carries.
type movingBlock struct {
	moved     uint32 // the block laid down when the move completes
	facing    [3]int // the piston's facing
	extending bool
	source    bool   // the piston's own head (extending) or base (retracting)
	due       uint64 // the tick the block lands on
}

// movingPistonState is the moving_piston block for a piston facing and type
// (MovingPistonBlock.FACING + TYPE).
func movingPistonState(facing [3]int, sticky bool) uint32 {
	info, _ := worldgen.InfoForState(movingPistonMin)
	d, _ := dirFromDelta(facing[0], facing[1], facing[2])
	st := worldgen.SetProperty(info, movingPistonMin, "facing", rsDirName[d])
	typ := "normal"
	if sticky {
		typ = "sticky"
	}
	return worldgen.SetProperty(info, st, "type", typ)
}

// legacyDirID is Direction.LEGACY_ID_CODEC: down 0, up 1, north 2, south 3,
// west 4, east 5 — vanilla's Direction order, which rsDir already is.
func legacyDirID(d [3]int) int32 {
	r, _ := dirFromDelta(d[0], d[1], d[2])
	return int32(r)
}

// placeMoving sets a moving_piston cell carrying `moved`, tells viewers what
// it carries, and schedules its completion.
func (h *hub) placeMoving(players map[int32]*tracked, pos blockPos, moving uint32, mb movingBlock) {
	h.setBlock(players, pos, moving)
	mb.due = h.tick.Load() + movingPistonTicks
	h.movingBlocks[pos] = mb
	h.toNearbyEv(players, 0, float64(pos.x), float64(pos.z), h.movingFrame(pos, mb))
	h.schedule(pos, movingPistonTicks)
}

// movingFrame is the cell's block-entity update for viewers.
func (h *hub) movingFrame(pos blockPos, mb movingBlock) attachproto.MovingPiston {
	name, _ := worldgen.StateName(mb.moved)
	return attachproto.MovingPiston{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z),
		State: mb.moved, Block: "minecraft:" + name, Props: worldgen.StateProps(mb.moved),
		Facing: legacyDirID(mb.facing), Extending: mb.extending, Source: mb.source,
	}
}

// finishMoving is the block entity's last tick: the carried block replaces
// the moving cell (air when the record is gone — a cell left over from
// before a restart) and the neighbours hear about it. A neighbour's block
// update reaching the cell early changes nothing; the cell lands on its
// own schedule.
func (h *hub) finishMoving(players map[int32]*tracked, pos blockPos) {
	mb, ok := h.movingBlocks[pos]
	if ok && h.tick.Load() < mb.due {
		return
	}
	delete(h.movingBlocks, pos)
	final := uint32(worldgen.Air)
	if ok {
		final = mb.moved
	}
	h.setBlock(players, pos, final)
	h.scheduleAround(pos, 1)
	if ok && isPistonBase(final) {
		h.scheduleSignalAround(pos)
	}
}

// finalTickMoving completes a moving cell at once (PistonMovingBlockEntity.
// finalTick, as a retracting piston does to the head it is pulling back):
// the source cell becomes air, any other its carried block.
func (h *hub) finalTickMoving(players map[int32]*tracked, pos blockPos) bool {
	mb, ok := h.movingBlocks[pos]
	if !ok || !isMovingPiston(h.world.At(pos.x, pos.y, pos.z)) {
		return false
	}
	delete(h.movingBlocks, pos)
	if mb.source {
		h.setBlock(players, pos, worldgen.Air)
	} else {
		h.setBlock(players, pos, mb.moved)
	}
	h.scheduleAround(pos, 1)
	return true
}
