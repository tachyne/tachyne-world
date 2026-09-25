package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Tripwire: a line of tripwire string strung between two facing tripwire hooks.
// An entity crossing any string in the line trips it; each end hook then emits
// a redstone signal. Reimplemented from TripWireBlock / TripWireHookBlock.
//
// Model: the per-tick occupancy scan presses strings an entity stands in and
// keeps each one's scheduled tick (h.wiresOn); a changed string re-evaluates
// the hooks along its axes. A hook's
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

// updateTripwires is TripWireBlock.entityInside and tick. Any entity in a
// string's cell — a player (not a spectator), a mob, a dropped item, an
// arrow, a cart or a boat — presses an unpowered string at once, unless its
// scheduled tick is pending; a pressed string re-checks only on its own
// 10-tick tick (so a pulse lasts at least 10 ticks), and on release it
// holds a 1-tick tick, which is what stops it re-pressing that same tick.
func (h *hub) updateTripwires(players map[int32]*tracked) {
	now := h.tick.Load()
	touched := map[simPos]bool{}
	mark := func(dim int, x, y, z float64) {
		if w := h.worldFor(dim); w != nil {
			p := blockPos{floorInt(x), floorInt(y + 0.01), floorInt(z)}
			if isTripwire(w.At(p.x, p.y, p.z)) {
				touched[simPos{dim, p}] = true
			}
		}
	}
	for _, t := range players {
		if t.gamemode != gmSpectator && !t.dead {
			mark(t.dim, t.x, t.y, t.z)
		}
	}
	for _, m := range h.mobs {
		if m.dying == 0 && !ignoresBlockTriggers(m) {
			mark(m.dim, m.x, m.y, m.z)
		}
	}
	for _, st := range h.armorStands {
		mark(st.dim, st.x, st.y, st.z)
	}
	for _, it := range h.items {
		mark(it.dim, it.x, it.y, it.z)
	}
	for _, a := range h.arrows {
		mark(a.dim, a.x, a.y, a.z)
	}
	for _, v := range h.vehicles {
		mark(v.dim, v.x, v.y, v.z)
	}
	// entityInside: an unpowered string with no tick pending is pressed.
	for p := range touched {
		if _, pending := h.wiresOn[p]; pending {
			continue
		}
		h.inDim(p.dim, func() {
			if s := h.rsWorld().At(p.x, p.y, p.z); isTripwire(s) && !boolProp(s, "powered") {
				h.setWirePressed(players, p.blockPos, true)
				h.wiresOn[p] = now + tripwirePressTicks
			}
		})
	}
	// tick: a pressed string re-checks on its schedule; a released one's
	// 1-tick hold runs out.
	for p, due := range h.wiresOn {
		if due > now {
			continue
		}
		delete(h.wiresOn, p)
		h.inDim(p.dim, func() {
			s := h.rsWorld().At(p.x, p.y, p.z)
			if !isTripwire(s) || !boolProp(s, "powered") {
				return
			}
			if touched[p] {
				h.wiresOn[p] = now + tripwirePressTicks
				return
			}
			h.setWirePressed(players, p.blockPos, false)
			h.wiresOn[p] = now + 1
		})
	}
}

// tripwirePressTicks is TripWireBlock's scheduleTick(pos, this, 10).
const tripwirePressTicks = 10

// setWirePressed flips a string's powered bit and re-evaluates the hooks along
// its two axes.
func (h *hub) setWirePressed(players map[int32]*tracked, pos blockPos, pressed bool) {
	s := h.rsWorld().At(pos.x, pos.y, pos.z)
	if !isTripwire(s) {
		return
	}
	if boolProp(s, "powered") != pressed {
		h.rsSet(players, pos, setBoolProp(s, "powered", pressed))
	}
	h.tripwireUpdateSource(players, pos)
}

// tripwireUpdateSource is TripWireBlock.updateSource: along both axes, the
// first hook within reach of an unbroken string re-evaluates its line. It
// runs when a string's state changes, and when a string is laid or taken
// away (onPlace, affectNeighborsAfterRemoval), so distant hooks attach and
// detach.
func (h *hub) tripwireUpdateSource(players map[int32]*tracked, pos blockPos) {
	for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		for i := 1; i < tripwireReach; i++ {
			np := blockPos{pos.x + d[0]*i, pos.y, pos.z + d[1]*i}
			ns := h.rsWorld().At(np.x, np.y, np.z)
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
		ns := h.rsWorld().At(np.x, np.y, np.z)
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
		h.rsSet(players, pos, ns)
		h.hookEmitState(players, pos, attached, powered, boolProp(state, "attached"), boolProp(state, "powered"))
		h.scheduleSignalAround(players, pos) // a powered hook drives its neighbours (TripWireHookBlock.notifyNeighbors)
	}
}

// hookEmitState is TripWireHookBlock.emitState: the click and the game event
// for the one change that matters most — powered on, powered off, attached,
// detached.
func (h *hub) hookEmitState(players map[int32]*tracked, pos blockPos, attached, powered, wasAttached, wasPowered bool) {
	var snd string
	var pitch float32
	var freq int
	switch {
	case powered && !wasPowered:
		snd, pitch, freq = "minecraft:block.tripwire.click_on", 0.6, freqBlockActivate
	case !powered && wasPowered:
		snd, pitch, freq = "minecraft:block.tripwire.click_off", 0.5, freqBlockDeactivate
	case attached && !wasAttached:
		snd, pitch, freq = "minecraft:block.tripwire.attach", 0.7, freqBlockActivate // BLOCK_ATTACH: 10
	case !attached && wasAttached:
		snd, pitch, freq = "minecraft:block.tripwire.detach", 1.2/(h.rng.Float32()*0.2+0.9), freqBlockDeactivate // BLOCK_DETACH: 9
	default:
		return
	}
	h.rsSound(players, snd, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.4, pitch)
	h.vib(h.rsDim, freq, pos.x, pos.y, pos.z, 0)
}

// ignoresBlockTriggers is Entity.isIgnoringBlockTriggers for a mob: a bat
// flutters over pressure plates and tripwire without setting them off.
func ignoresBlockTriggers(m *mob) bool { return m.etype == entityBat }
