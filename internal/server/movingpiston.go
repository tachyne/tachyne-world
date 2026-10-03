package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
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
	h.putMoving(key, mb)
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
				h.dropMoving(key) // the cell was broken meanwhile
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

// landingShape is Block.updateFromNeighbourShapes on the carried block, which
// both of PistonMovingBlockEntity's landings run: it arrives with the shape
// its new neighbours give it (a stair beside another turns a corner, a wall
// or a fence joins what is there), not the one it had where it left.
func landingShape(w *world.World, pos blockPos, moved uint32) uint32 {
	if s := updateFromNeighbourShapes(w, pos, moved); s != worldgen.Air {
		return s
	}
	return moved
}

// finishMoving is the block entity's last tick (PistonMovingBlockEntity.tick):
// the carried block replaces the moving cell, in the shape its neighbours
// give it and without the water it carried (its waterlogging is dropped),
// and the neighbours hear about it at once. A cell with no record — one a
// save caught before records were kept — clears to air.
func (h *hub) finishMoving(players map[int32]*tracked, pos blockPos) {
	key := simPos{dim: h.rsDim, blockPos: pos}
	mb, ok := h.movingBlocks[key]
	if ok && h.tick.Load() < mb.due {
		return
	}
	h.dropMoving(key)
	final := uint32(worldgen.Air)
	if ok {
		final = landedStateOf(landingShape(h.rsWorld(), pos, mb.moved))
		if worldgen.IsWaterlogged(final) && !worldgen.IsFluid(final) {
			final = setBoolProp(final, "waterlogged", false) // the water does not travel
		}
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
	h.dropMoving(key)
	if mb.source {
		h.rsSet(players, pos, worldgen.Air)
	} else {
		final := landedStateOf(landingShape(h.rsWorld(), pos, mb.moved))
		h.rsSet(players, pos, final)
		h.rodOnPlace(pos, final)
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
	if !h.hasScheduledTick(pos) {
		h.scheduleTick(pos, 8, tickNormal)
	}
}

// putMoving records a moving cell's block entity, and mirrors it for the
// sessions.
func (h *hub) putMoving(key simPos, mb movingBlock) {
	h.movingBlocks[key] = mb
	h.movingLive.Store(key, struct{}{})
}

// dropMoving forgets a moving cell's block entity, mirror and all.
func (h *hub) dropMoving(key simPos) {
	delete(h.movingBlocks, key)
	h.movingLive.Delete(key)
}

// movingCellLive reports, from any goroutine, whether a moving_piston cell
// has its block entity (PistonMovingBlockEntity) — the question
// MovingPistonBlock.useWithoutItem asks of a click.
func (h *hub) movingCellLive(dim int, pos blockPos) bool {
	_, ok := h.movingLive.Load(simPos{dim: dim, blockPos: pos})
	return ok
}

// evUseMovingPiston is a right-click on a moving_piston cell.
type evUseMovingPiston struct {
	eid     int32
	x, y, z int
}

func (evUseMovingPiston) isHubEvent() {}

// onUseMovingPiston is MovingPistonBlock.useWithoutItem: a moving cell with
// no block entity behind it (one left over from before a restart) is
// removed by the click. A live one carries on to its landing.
func (h *hub) onUseMovingPiston(players map[int32]*tracked, e evUseMovingPiston) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	if !isMovingPiston(h.worldFor(t.dim).At(pos.x, pos.y, pos.z)) {
		return
	}
	if _, live := h.movingBlocks[simPos{dim: t.dim, blockPos: pos}]; live {
		return
	}
	h.inDim(t.dim, func() { h.finishMoving(players, pos) })
}

// snapshotMoving saves the moving cells with the rest of the world's block
// entities (PistonMovingBlockEntity.saveAdditional), so a block caught
// mid-slide by a save still lands after a restart instead of being lost.
func (h *hub) snapshotMoving() []savedMoving {
	out := make([]savedMoving, 0, len(h.movingOrder))
	now := h.tick.Load()
	for _, key := range h.movingOrder {
		mb, ok := h.movingBlocks[key]
		if !ok {
			continue
		}
		left := 0
		if mb.due > now {
			left = int(mb.due - now)
		}
		out = append(out, savedMoving{Dim: key.dim, X: key.x, Y: key.y, Z: key.z, State: mb.moved,
			Facing: legacyDirID(mb.facing), Extending: mb.extending, Source: mb.source, Left: left})
	}
	return out
}

// restoreMoving puts the saved moving cells back at boot
// (PistonMovingBlockEntity.loadAdditional): each lands on its own clock.
func (h *hub) restoreMoving(saved []savedMoving) {
	for _, sm := range saved {
		if sm.Facing < 0 || sm.Facing > 5 {
			continue
		}
		dx, dy, dz := rsDir(sm.Facing).delta()
		key := simPos{dim: sm.Dim, blockPos: blockPos{sm.X, sm.Y, sm.Z}}
		if _, dup := h.movingBlocks[key]; dup {
			continue
		}
		h.putMoving(key, movingBlock{moved: sm.State, facing: [3]int{dx, dy, dz},
			extending: sm.Extending, source: sm.Source, due: h.tick.Load() + uint64(sm.Left)})
		h.movingOrder = append(h.movingOrder, key)
	}
}
