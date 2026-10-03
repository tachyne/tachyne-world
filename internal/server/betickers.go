package server

import "sort"

// Block-entity tickers (BlockEntityTicker, run with the block entities after
// the block ticks): the daylight detector's twenty-tick read of the sky and
// the crafter's six-tick arm. Both used to ride the simulation queue, the
// detector re-scheduling itself every twenty ticks and the arm settling on
// a queued update six ticks out; a neighbour's update also re-read the
// detector and settled the arm, which vanilla's blocks never do (neither
// has a neighborChanged that touches them).
//
//   - DaylightDetectorBlock.tickEntity: on a game tick divisible by twenty
//     (getGameTime() % 20 == 0), updateSignalStrength. Only where the
//     dimension has sky light: getTicker hands out none elsewhere.
//   - CrafterBlockEntity.serverTick: the craftingTicksRemaining a craft set
//     counts down, and at nought CRAFTING goes back to false.

// daylightCadence is DaylightDetectorBlock.tickEntity's getGameTime() % 20.
const daylightCadence = 20

// registerDaylight adds a daylight detector to the tickers, once, in the
// order detectors appear.
func (h *hub) registerDaylight(pos simPos) {
	if h.daylightTicking == nil {
		h.daylightTicking = map[simPos]bool{}
	}
	if !h.daylightTicking[pos] {
		h.daylightTicking[pos] = true
		h.daylightOrder = append(h.daylightOrder, pos)
	}
}

// blockEntityOnPlace is a block written at pos joining the tickers its
// block entity has.
func (h *hub) blockEntityOnPlace(dim int, pos blockPos, state uint32) {
	if isDaylight(state) {
		h.registerDaylight(simPos{dim: dim, blockPos: pos})
	}
}

// startCrafterArm is CrafterBlockEntity.setCraftingTicksRemaining(6).
func (h *hub) startCrafterArm(pos simPos) {
	if h.crafterArms == nil {
		h.crafterArms = map[simPos]int{}
	}
	h.crafterArms[pos] = crafterAnimTicks
}

// registerBlockEntityTickers finds the ticking block entities in the saved
// world at boot: every daylight detector, and any crafter left with its arm
// out (its countdown was not saved, so it settles on the first tick).
func (h *hub) registerBlockEntityTickers() {
	var found []simPos
	for dim, w := range h.allDims() {
		w.ForEachEdit(func(x, y, z int, state uint32) {
			switch {
			case isDaylight(state):
				found = append(found, simPos{dim: dim, blockPos: blockPos{x, y, z}})
			case isCrafter(state) && (state-crafterMin)/24 == 0:
				if h.crafterArms == nil {
					h.crafterArms = map[simPos]int{}
				}
				h.crafterArms[simPos{dim: dim, blockPos: blockPos{x, y, z}}] = 1
			}
		})
	}
	sort.Slice(found, func(i, j int) bool { // a fixed order: the edit maps iterate at random
		a, b := found[i], found[j]
		if a.dim != b.dim {
			return a.dim < b.dim
		}
		if a.x != b.x {
			return a.x < b.x
		}
		if a.y != b.y {
			return a.y < b.y
		}
		return a.z < b.z
	})
	for _, p := range found {
		h.registerDaylight(p)
	}
}

// tickBlockEntityTickers runs the daylight detectors and crafter arms for
// one game tick.
func (h *hub) tickBlockEntityTickers(players map[int32]*tracked) {
	if len(h.daylightOrder) > 0 && h.tick.Load()%daylightCadence == 0 {
		keep := h.daylightOrder[:0]
		for _, pos := range h.daylightOrder {
			w := h.worldFor(pos.dim)
			if w == nil {
				delete(h.daylightTicking, pos)
				continue
			}
			if !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) {
				keep = append(keep, pos) // an unloaded detector waits
				continue
			}
			state := w.At(pos.x, pos.y, pos.z)
			if !isDaylight(state) {
				delete(h.daylightTicking, pos) // the block went: its ticker goes with it
				continue
			}
			keep = append(keep, pos)
			h.inDim(pos.dim, func() { h.updateDaylight(players, pos.blockPos, state) })
		}
		h.daylightOrder = keep
	}
	for pos, n := range h.crafterArms {
		w := h.worldFor(pos.dim)
		if w == nil {
			delete(h.crafterArms, pos)
			continue
		}
		if !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) {
			continue
		}
		state := w.At(pos.x, pos.y, pos.z)
		if !isCrafter(state) {
			delete(h.crafterArms, pos)
			continue
		}
		if n--; n > 0 {
			h.crafterArms[pos] = n
			continue
		}
		delete(h.crafterArms, pos)
		if (state-crafterMin)/24 == 0 { // CRAFTING
			h.setBlockAt(players, pos.dim, pos.blockPos, crafterWithCrafting(state, false))
		}
	}
}
