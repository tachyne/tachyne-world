package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A boat over a bubble column (AbstractBoat.onAboveBubbleColumn and
// tickBubbleColumn). Floating on a column's top cell starts a sixty-tick
// timer; the client rocks the boat from it (DATA_ID_BUBBLE_TIME). When the
// timer runs out with the boat still there, a whirlpool throws its riders
// out and pulls it under (entity event 71), and an updraft tosses it up
// (72). Leaving the column resets the timer. Under the surface a column
// pushes the boat as it does anything else (onInsideBubbleColumn).

const (
	boatMetaBubbleTime = 13 // DATA_ID_BUBBLE_TIME: INT
	boatBubbleTime     = 60 // AbstractBoat.BUBBLE_TIME
	boatEventWhirlpool = 71 // handleBubbleColumnEffect(true) on the client
	boatEventUpdraft   = 72 // handleBubbleColumnEffect(false)
)

// boatColumn finds the bubble column the boat's box is in: whether it is a
// top cell (nothing above) and whether it drags down. The first top cell
// found wins, as the last onAboveBubbleColumn call of a tick sets the
// direction.
func boatColumn(w *world.World, v *vehicle) (found, top, drag bool) {
	const e = 1e-5 // applyEffectsFromBlocks: the box deflated
	for x := floorInt(v.x - boatBoxHalfW + e); x <= floorInt(v.x+boatBoxHalfW-e); x++ {
		for y := floorInt(v.y + e); y <= floorInt(v.y+boatBoxH-e); y++ {
			for z := floorInt(v.z - boatBoxHalfW + e); z <= floorInt(v.z+boatBoxHalfW-e); z++ {
				st := w.At(x, y, z)
				if !worldgen.IsBubbleColumn(st) {
					continue
				}
				if bubbleColumnTop(w, x, y, z) {
					return true, true, st == worldgen.BubbleColumnDrag
				}
				found, drag = true, st == worldgen.BubbleColumnDrag
			}
		}
	}
	return found, false, drag
}

// tickBoatBubbles runs the column's effect on a boat and its timer.
func (h *hub) tickBoatBubbles(players map[int32]*tracked, v *vehicle) {
	w := h.worldFor(v.dim)
	if w == nil {
		return
	}
	simulates := h.boatController(v) == 0 // canSimulateMovement: nobody rowing it
	found, top, drag := boatColumn(w, v)
	above := false
	switch {
	case top:
		above, v.bubbleDown = true, drag
		if v.bubbleTime == 0 {
			h.setBoatBubbleTime(players, v, boatBubbleTime)
		}
	case found && simulates:
		if drag {
			v.boatVY = math.Max(columnDownCap, v.boatVY-columnDownStep)
		} else {
			v.boatVY = math.Min(columnUpCap, v.boatVY+columnUpStep)
		}
	}
	// tickBubbleColumn.
	if !above {
		h.setBoatBubbleTime(players, v, 0)
		return
	}
	if v.bubbleTime <= 0 {
		return
	}
	left := v.bubbleTime - 1
	h.setBoatBubbleTime(players, v, left)
	if left > 0 || boatBubbleTime-left-1 <= 0 {
		return
	}
	event := int32(boatEventUpdraft)
	if v.bubbleDown {
		h.ejectBoat(players, v)
		event = boatEventWhirlpool
	}
	h.toTracking(players, v.eid, v.dim, v.x, v.z, attachproto.EntityStatus{EID: v.eid, Status: event})
	if h.boatController(v) == 0 { // handleBubbleColumnEffect where the server moves it
		if v.bubbleDown {
			v.boatVY -= 0.7
		} else {
			v.boatVY = 0.6
		}
	}
}

// setBoatBubbleTime sets the synced timer, sending it when it changes.
func (h *hub) setBoatBubbleTime(players map[int32]*tracked, v *vehicle, n int) {
	if v.bubbleTime == n {
		return
	}
	v.bubbleTime = n
	h.toTracking(players, v.eid, v.dim, v.x, v.z, metaEv(boatBubbleMeta(v)))
}

func boatBubbleMeta(v *vehicle) []byte {
	b := protocol.AppendVarInt(nil, v.eid)
	b = protocol.AppendU8(b, boatMetaBubbleTime)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(v.bubbleTime))
	return protocol.AppendU8(b, itemMetaEnd)
}
