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
	h.rsSet(players, pos, moving)
	mb.due = h.tick.Load() + movingPistonTicks
	key := simPos{dim: h.rsDim, blockPos: pos}
	h.movingBlocks[key] = mb
	h.movingOrder = append(h.movingOrder, key)
	h.toNearbyEv(players, h.rsDim, float64(pos.x), float64(pos.z), h.movingFrame(pos, mb))
}

// landMovingBlocks is the moving cells' block-entity tick, which vanilla
// runs after the tick's block events: each cell whose two ticks are up lays
// its block down, in the order the cells were made.
func (h *hub) landMovingBlocks(players map[int32]*tracked, age uint64) {
	if len(h.movingOrder) == 0 {
		return
	}
	order := h.movingOrder
	h.movingOrder = nil
	var keep []simPos
	for i, key := range order {
		mb, ok := h.movingBlocks[key]
		if !ok {
			continue // finished early (finalTickMoving) or replaced
		}
		if mb.due > age {
			keep = append(keep, key)
			continue
		}
		if h.worldFor(key.dim) == nil || !h.canTickBlocksAt(key) {
			keep = append(keep, order[i])
			continue
		}
		h.inDim(key.dim, func() {
			if isMovingPiston(h.rsWorld().At(key.x, key.y, key.z)) {
				h.finishMoving(players, key.blockPos)
			} else {
				delete(h.movingBlocks, key) // the cell was broken meanwhile
			}
		})
	}
	h.movingOrder = append(keep, h.movingOrder...)
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

// landedStateOf is SculkSensorBlock.onPlace, TargetBlock.onPlace and
// ObserverBlock.onPlace: a block
// that carries a redstone power but has no scheduled tick to clear it again
// is put down with that power zeroed. Vanilla's guard exists for exactly this
// case — a piston carrying a live sensor or a freshly shot target — because
// the schedule that would have reset it does not travel with the block.
func landedStateOf(state uint32) uint32 {
	switch {
	case isAnySensor(state) && sensorPower(state) > 0:
		return sensorWith(state, 0, sensorPhase(state))
	case isTarget(state) && targetPower(state) > 0:
		return targetMin
	case isObserver(state) && boolProp(state, "powered"): // ObserverBlock.onPlace
		return setBoolProp(state, "powered", false)
	}
	return state
}

// finishMoving is the block entity's last tick: the carried block replaces
// the moving cell (air when the record is gone — a cell left over from
// before a restart) and the neighbours hear about it at once.
func (h *hub) finishMoving(players map[int32]*tracked, pos blockPos) {
	key := simPos{dim: h.rsDim, blockPos: pos}
	mb, ok := h.movingBlocks[key]
	if ok && h.tick.Load() < mb.due {
		return
	}
	delete(h.movingBlocks, key)
	final := uint32(worldgen.Air)
	if ok {
		final = landedStateOf(mb.moved)
	}
	h.rsSet(players, pos, final)
	h.rodOnPlace(pos, final)
	h.composterOnPlace(h.rsDim, pos, final)
	h.notifyAround(players, h.rsDim, pos)
	if ok && isPistonBase(final) {
		h.scheduleSignalAround(players, pos)
	}
}

// finalTickMoving completes a moving cell at once (PistonMovingBlockEntity.
// finalTick, as a retracting piston does to the head it is pulling back):
// the source cell becomes air, any other its carried block.
func (h *hub) finalTickMoving(players map[int32]*tracked, pos blockPos) bool {
	key := simPos{dim: h.rsDim, blockPos: pos}
	mb, ok := h.movingBlocks[key]
	if !ok || !isMovingPiston(h.rsWorld().At(pos.x, pos.y, pos.z)) {
		return false
	}
	delete(h.movingBlocks, key)
	if mb.source {
		h.rsSet(players, pos, worldgen.Air)
	} else {
		h.rsSet(players, pos, landedStateOf(mb.moved))
		h.rodOnPlace(pos, landedStateOf(mb.moved))
	}
	h.notifyAround(players, h.rsDim, pos)
	return true
}

// rodOnPlace is LightningRodBlock.onPlace: a rod put down still POWERED (a
// piston carried it mid-pulse) with no tick of its own due schedules one
// eight ticks out, or it would stay powered: its pending tick stayed with
// the cell it left.
func (h *hub) rodOnPlace(pos blockPos, state uint32) {
	if !isLightningRod(state) || !boolProp(state, "powered") {
		return
	}
	if _, ok := h.rsDue[h.rsKey(pos)]; ok {
		return
	}
	h.rsDue[h.rsKey(pos)] = h.tick.Load() + 8
	h.rsSchedule(pos, 8)
}
