package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Tripwire: a line of tripwire string strung between two facing tripwire hooks.
// An entity crossing any string in the line trips it; each end hook then emits
// a redstone signal. Reimplemented from TripWireBlock / TripWireHookBlock.
//
// Model: the per-tick occupancy scan flags strings an entity stands in
// (h.wiresOn); a changed string re-evaluates the hooks along its axes. A hook's
// calcHook walks up to 42 blocks along its facing for the opposite hook,
// marking itself attached (a valid line exists) and powered (some string in the
// line is tripped and not disarmed).

var (
	tripwireMin, tripwireMax         = worldgen.BlockRange("tripwire")
	tripwireHookMin, tripwireHookMax = worldgen.BlockRange("tripwire_hook")
)

func isTripwire(s uint32) bool     { return s >= tripwireMin && s <= tripwireMax }
func isTripwireHook(s uint32) bool { return s >= tripwireHookMin && s <= tripwireHookMax }

// tripwireDefaultState is a freshly laid tripwire string: all booleans clear
// (the block's min state has them all set, so it can't be used directly).
func tripwireDefaultState() uint32 {
	s := tripwireMin
	for _, p := range []string{"attached", "disarmed", "powered", "north", "south", "east", "west"} {
		s = setBoolProp(s, p, false)
	}
	return s
}

const tripwireReach = 42

// updateTripwires is the per-tick occupancy scan for tripwire strings.
func (h *hub) updateTripwires(players map[int32]*tracked) {
	now := map[blockPos]bool{}
	mark := func(x, y, z float64) {
		p := blockPos{floorInt(x), floorInt(y + 0.01), floorInt(z)}
		if isTripwire(h.world.At(p.x, p.y, p.z)) {
			now[p] = true
		}
	}
	for _, t := range players {
		if t.dim == 0 {
			mark(t.x, t.y, t.z)
		}
	}
	for _, m := range h.mobs {
		if m.dim == 0 {
			mark(m.x, m.y, m.z)
		}
	}
	for p := range h.wiresOn { // released strings
		if !now[p] {
			h.setWirePressed(players, p, false)
		}
	}
	for p := range now { // newly pressed strings
		if !h.wiresOn[p] {
			h.setWirePressed(players, p, true)
		}
	}
	h.wiresOn = now
}

// setWirePressed flips a string's powered bit and re-evaluates the hooks along
// its two axes.
func (h *hub) setWirePressed(players map[int32]*tracked, pos blockPos, pressed bool) {
	s := h.world.At(pos.x, pos.y, pos.z)
	if !isTripwire(s) {
		return
	}
	if boolProp(s, "powered") != pressed {
		h.setBlock(players, pos, setBoolProp(s, "powered", pressed))
	}
	for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		for i := 1; i < tripwireReach; i++ {
			np := blockPos{pos.x + d[0]*i, pos.y, pos.z + d[1]*i}
			ns := h.world.At(np.x, np.y, np.z)
			if isTripwireHook(ns) {
				h.calcHook(players, np, ns)
				break
			}
			if !isTripwire(ns) {
				break // line broken
			}
		}
	}
}

// calcHook re-evaluates a hook: scan its facing line for the opposite hook and
// any tripped string, then write attached + powered.
func (h *hub) calcHook(players map[int32]*tracked, pos blockPos, state uint32) {
	facing := stateFacing(state)
	dx, dz := facingDelta(facing)
	dist, powered := 0, false
	for i := 1; i < tripwireReach; i++ {
		np := blockPos{pos.x + dx*i, pos.y, pos.z + dz*i}
		ns := h.world.At(np.x, np.y, np.z)
		if isTripwireHook(ns) {
			if stateFacing(ns) == oppositeFacing(facing) {
				dist = i // a matching hook closes the line
			}
			break
		}
		if isTripwire(ns) {
			if boolProp(ns, "powered") && !boolProp(ns, "disarmed") {
				powered = true
			}
			continue
		}
		break // anything else breaks the line
	}
	attached := dist > 1
	if !attached {
		powered = false
	}
	ns := setBoolProp(setBoolProp(state, "attached", attached), "powered", powered)
	if ns != state {
		h.setBlock(players, pos, ns)
		h.scheduleAround(pos, 1) // a powered hook drives its neighbours
	}
}
