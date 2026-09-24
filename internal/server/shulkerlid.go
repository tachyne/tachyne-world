package server

// The shulker box's lid on the server (ShulkerBoxBlockEntity.updateAnimation).
// The client draws the lid from the opener-count block event; the server
// keeps the same animation because two things hang off it:
//
//   - the box sends shape and neighbour updates when the lid starts moving
//     and when it stops (doNeighborUpdates), so an observer watching a
//     shulker box fires when someone opens or shuts it;
//   - while opening, the lid pushes what sits in its way outward along the
//     box's facing (moveCollidedEntities): mobs and dropped items here.
//     Players move themselves — their client runs the same animation.

const (
	lidClosed = iota
	lidOpening
	lidOpened
	lidClosing
)

const lidStep = 0.1 // progress per tick, 0 → 1 in ten ticks

type shulkerLid struct {
	status         int
	progress, prev float32
}

// shulkerLidEvent is triggerEvent(1, count): a count of one starts the lid
// opening, zero starts it closing; other counts leave it as it is.
func (h *hub) shulkerLidEvent(pos simPos, count int) {
	if h.shulkerLids == nil {
		h.shulkerLids = map[simPos]*shulkerLid{}
	}
	lid := h.shulkerLids[pos]
	if lid == nil {
		lid = &shulkerLid{}
		h.shulkerLids[pos] = lid
	}
	switch count {
	case 0:
		lid.status = lidClosing
	case 1:
		lid.status = lidOpening
	}
}

// tickShulkerLids is every animating box's block-entity tick.
func (h *hub) tickShulkerLids(players map[int32]*tracked) {
	for pos, lid := range h.shulkerLids {
		w := h.worldFor(pos.dim)
		st := w.At(pos.x, pos.y, pos.z)
		if !isShulkerBox(st) {
			delete(h.shulkerLids, pos) // broken or moved
			continue
		}
		lid.prev = lid.progress
		switch lid.status {
		case lidClosed:
			lid.progress = 0
			delete(h.shulkerLids, pos)
		case lidOpening:
			lid.progress += lidStep
			if lid.prev == 0 {
				h.lidNeighborUpdates(players, pos, st)
			}
			if lid.progress >= 1 {
				lid.status, lid.progress = lidOpened, 1
				h.lidNeighborUpdates(players, pos, st)
			}
			h.lidPush(players, pos, st, lid)
		case lidOpened:
			lid.progress = 1
		case lidClosing:
			lid.progress -= lidStep
			if lid.prev == 1 {
				h.lidNeighborUpdates(players, pos, st)
			}
			if lid.progress <= 0 {
				lid.status, lid.progress = lidClosed, 0
				h.lidNeighborUpdates(players, pos, st)
			}
		}
	}
}

// lidNeighborUpdates is doNeighborUpdates: the shape update its neighbours
// (an observer watching it) get, and the ordinary neighbour update.
func (h *hub) lidNeighborUpdates(players map[int32]*tracked, pos simPos, st uint32) {
	h.observersSee(players, pos.dim, pos.blockPos, st)
	h.notifyAround(players, pos.dim, pos.blockPos)
}

// lidPush is moveCollidedEntities: the slab of space the lid swept this
// tick (Shulker.getProgressDeltaAabb on the raw 0..1 progress, a full block
// wide) pushes whatever it meets that far again, plus 0.01, outward.
func (h *hub) lidPush(players map[int32]*tracked, pos simPos, st uint32, lid *shulkerLid) {
	d, ok := propDir(st, "facing")
	if !ok {
		return
	}
	dx, dy, dz := d.delta()
	lo, hi := float64(min(lid.prev, lid.progress)), float64(max(lid.prev, lid.progress))
	// The block's own cube, slid out along the facing by the lid's travel.
	x0, y0, z0 := float64(pos.x), float64(pos.y), float64(pos.z)
	x1, y1, z1 := x0+1, y0+1, z0+1
	slide := func(a0, a1 *float64, step int) {
		switch {
		case step > 0:
			*a0, *a1 = *a1+lo, *a1+hi
		case step < 0:
			*a0, *a1 = *a0-hi, *a0-lo
		}
	}
	slide(&x0, &x1, dx)
	slide(&y0, &y1, dy)
	slide(&z0, &z1, dz)
	move := hi - lo + 0.01
	meets := func(x, y, z, hw, ht float64) bool {
		return x+hw > x0 && x-hw < x1 && y+ht > y0 && y < y1 && z+hw > z0 && z-hw < z1
	}
	for _, m := range h.mobs {
		if m.dim != pos.dim || m.dying > 0 {
			continue
		}
		b := m.box()
		if !meets(m.x, m.y, m.z, b.w/2, b.h) {
			continue
		}
		m.x += move * float64(dx)
		m.y += move * float64(dy)
		m.z += move * float64(dz)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false))
	}
	for _, it := range h.items {
		if it.dim != pos.dim || !meets(it.x, it.y, it.z, itemHalfHeight, 2*itemHalfHeight) {
			continue
		}
		it.x += move * float64(dx)
		it.y += move * float64(dy)
		it.z += move * float64(dz)
		h.toTracking(players, it.eid, it.dim, it.x, it.z, entMove(it.eid, it.x, it.y, it.z, 0, 0, false))
	}
}
