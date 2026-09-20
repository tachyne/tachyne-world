package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone tier 1b: repeaters, comparators, observers, pressure plates and
// daylight detectors. Repeater/comparator/observer are in the orientable
// property table (Get/SetProperty work); plates and detectors have their tiny
// layouts inlined like the torch.
//
// Direction conventions (minecraft.wiki):
//   repeater/comparator `facing` = output→input = toward the placing player,
//     so input cell = pos+facing, output cell = pos-facing.
//   observer `facing` = the watched direction = the player's look direction;
//     it pulses 15 for 2 ticks out of its back (pos-facing) when the watched
//     block changes.

const (
	observerPulseTicks = 2
)

var (
	repeaterMin   = worldgen.BlockBase("repeater")
	repeaterMax   = worldgen.BlockBase("repeater") + 63
	comparatorMin = worldgen.BlockBase("comparator")
	comparatorMax = worldgen.BlockBase("comparator") + 15
	observerMin   = worldgen.BlockBase("observer")
	observerMax   = worldgen.BlockBase("observer") + 11

	stonePlateOn  = worldgen.BlockBase("stone_pressure_plate") // powered is the low bit, true first
	stonePlateOff = worldgen.BlockBase("stone_pressure_plate") + 1
	oakPlateOn    = worldgen.BlockBase("oak_pressure_plate")
	oakPlateOff   = worldgen.BlockBase("oak_pressure_plate") + 1
	lightPlateMin = worldgen.BlockBase("light_weighted_pressure_plate") // base + power 0-15
	lightPlateMax = worldgen.BlockBase("light_weighted_pressure_plate") + 15
	heavyPlateMin = worldgen.BlockBase("heavy_weighted_pressure_plate")
	heavyPlateMax = worldgen.BlockBase("heavy_weighted_pressure_plate") + 15

	daylightMin = worldgen.BlockBase("daylight_detector") // inverted(2) × power(16); inverted=true is the LOW block
	daylightMax = worldgen.BlockBase("daylight_detector") + 31
)

// platePressedTicks is BasePressurePlateBlock.getPressedTime and the
// detector rail's: the release check runs this long after the last press.
const platePressedTicks = 20

func isRepeater(s uint32) bool   { return s >= repeaterMin && s <= repeaterMax }
func isComparator(s uint32) bool { return s >= comparatorMin && s <= comparatorMax }
func isObserver(s uint32) bool   { return s >= observerMin && s <= observerMax }
func isDaylight(s uint32) bool   { return s >= daylightMin && s <= daylightMax }

// platePower is the signal a plate emits in its current state.

// daylight detector state math: power is the fastest-varying property.
func daylightPower(s uint32) int     { return int((s - daylightMin) % 16) }
func daylightInverted(s uint32) bool { return (s-daylightMin)/16 == 0 }
func daylightWith(inverted bool, power int) uint32 {
	inv := uint32(1)
	if inverted {
		inv = 0
	}
	return daylightMin + inv*16 + uint32(power)
}

// stateFacing reads the facing property of an orient-table block.
func stateFacing(s uint32) string {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return "north"
	}
	return worldgen.GetProperty(info, s, "facing")
}

// repeaterDelay is the repeater's configured delay in game ticks.
func repeaterDelay(s uint32) int {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return 2
	}
	d := worldgen.GetProperty(info, s, "delay")
	return 2 * int(d[0]-'0')
}

// obsDelta is the observer's watch direction as a 3D delta (6-way facing).
func obsDelta(s uint32) (int, int, int) {
	switch stateFacing(s) {
	case "up":
		return 0, 1, 0
	case "down":
		return 0, -1, 0
	}
	dx, dz := facingDelta(stateFacing(s))
	return dx, 0, dz
}

// repeaterLocked reports whether a powered diode (repeater/comparator) on
// either side faces into this repeater — a locked repeater freezes its output
// (vanilla DiodeBlock.isLocked / getAlternateSignal).
func (h *hub) repeaterLocked(pos blockPos, state uint32) bool {
	// DiodeBlock.isLocked: getAlternateSignal with sideInputDiodesOnly.
	return h.diodeSideSignal(pos, state, true) > 0
}

// updateRepeater: reads the cell behind (facing side), flips `powered` after
// the configured delay, emits 15 out the front. rsDue holds the pending flip.
// A locked repeater (a powered diode facing its side) holds its output.
func (h *hub) updateRepeater(players map[int32]*tracked, pos blockPos, state uint32) {
	if locked := h.repeaterLocked(pos, state); locked != boolProp(state, "locked") {
		state = setBoolProp(state, "locked", locked)
		h.rsSet(players, pos, state)
	}
	if boolProp(state, "locked") {
		delete(h.rsDue, pos) // frozen: a pending tick fires into nothing (DiodeBlock.tick returns)
		return
	}
	in := h.diodeInputSignal(pos, state) > 0 // DiodeBlock.shouldTurnOn
	cur := boolProp(state, "powered")
	now := h.tick.Load()
	delay := uint64(repeaterDelay(state))
	if due, pending := h.rsDue[pos]; pending {
		if now < due {
			return // a neighbour update while a tick is pending: one tick per position (LevelTicks dedup)
		}
		// The scheduled tick (DiodeBlock.tick): an input that went away
		// before the tick still turns the output on, and a second tick then
		// turns it off — a pulse shorter than the delay comes out the
		// delay long, never lost.
		delete(h.rsDue, pos)
		switch {
		case cur && !in:
			h.rsSet(players, pos, setBoolProp(state, "powered", false))
			h.scheduleSignalAround(pos)
		case !cur:
			h.rsSet(players, pos, setBoolProp(state, "powered", true))
			h.scheduleSignalAround(pos)
			if !in {
				h.rsDue[pos] = now + delay
				h.rsSchedule(pos, delay)
			}
		}
		return
	}
	if in != cur { // DiodeBlock.checkTickOnNeighbor
		h.rsDue[pos] = now + delay
		h.rsSchedule(pos, delay)
	}
}

// updateCopperBulb latches like vanilla CopperBulbBlock: on the RISING edge of
// redstone power it toggles `lit` (a T flip-flop), while `powered` always tracks
// the input. Light emission and comparator output both follow `lit`.
func (h *hub) updateCopperBulb(players map[int32]*tracked, pos blockPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if powered == worldgen.CopperBulbPowered(state) {
		return
	}
	lit := worldgen.CopperBulbLit(state)
	if powered { // rising edge (was unpowered): flip the bulb
		lit = !lit
		snd := "minecraft:block.copper_bulb.turn_off"
		if lit {
			snd = "minecraft:block.copper_bulb.turn_on"
		}
		h.rsSound(players, snd, sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.6, 1)
	}
	h.rsSet(players, pos, worldgen.CopperBulbSet(state, lit, powered))
	h.scheduleSignalAround(pos) // relight + let an adjacent comparator re-read
}

// updateComparator: output = rear if rear >= strongest side (compare mode),
// or rear - strongest side (subtract). The level lives in h.compOut (vanilla
// keeps it in a block entity).
func (h *hub) updateComparator(players map[int32]*tracked, pos blockPos, state uint32) {
	dx, dz := facingDelta(stateFacing(state))
	rear := h.diodeInputSignal(pos, state) // DiodeBlock.getInputSignal, then the analog override below
	back := blockPos{pos.x + dx, pos.y, pos.z + dz}
	bs := h.rsWorld().At(back.x, back.y, back.z)
	if sig := h.analogSignal(simPos{dim: h.rsDim, blockPos: back}); sig >= 0 {
		if sig > rear {
			rear = sig // container fullness, cake left, composter level, …
		}
	} else if worldgen.IsCopperBulb(bs) {
		if worldgen.CopperBulbLit(bs) && rear < 15 {
			rear = 15 // lit copper bulb: full analog signal (unlit = 0)
		}
	} else if isAnySensor(bs) {
		if sensorPhase(bs) == sculkPhaseActive { // active sensor: comparator reads the frequency
			if f := h.sculkFreq[back]; f > rear {
				rear = f
			}
		}
	} else if worldgen.IsSolidFull(bs) {
		// A solid block behind is transparent to the read: measure the container
		// one cell further (vanilla comparator-through-block).
		if sig := h.analogSignal(simPos{blockPos: blockPos{back.x + dx, back.y, back.z + dz}}); sig > rear {
			rear = sig
		}
	}
	side := h.diodeSideSignal(pos, state, false) // any source on a comparator's sides
	out := 0
	info, _ := worldgen.InfoForState(state)
	if worldgen.GetProperty(info, state, "mode") == "subtract" {
		if out = rear - side; out < 0 {
			out = 0
		}
	} else if rear >= side {
		out = rear
	}
	// Vanilla ComparatorBlock.getDelay() = 2: the output change lands two game
	// ticks after the input settles, via the same delayed-flip (rsDue) mechanism
	// repeaters use. (Was applied immediately.)
	key := simPos{dim: h.rsDim, blockPos: pos}
	if h.compOut[key] == out && boolProp(state, "powered") == (out > 0) {
		delete(h.rsDue, pos) // settled — cancel any pending flip
		return
	}
	now := h.tick.Load()
	due, pending := h.rsDue[pos]
	if !pending {
		h.rsDue[pos] = now + comparatorDelay
		h.rsSchedule(pos, comparatorDelay)
		return
	}
	if now >= due {
		delete(h.rsDue, pos)
		h.compOut[key] = out
		h.rsSet(players, pos, setBoolProp(state, "powered", out > 0))
		h.scheduleSignalAround(pos)
	}
}

const comparatorDelay = 2 // ComparatorBlock.getDelay(): 2 game ticks

// updateObserver: pulse 15 out the back for 2 ticks when the watched block
// changes state (obsSeen remembers the last look).
func (h *hub) updateObserver(players map[int32]*tracked, pos blockPos, state uint32) {
	now := h.tick.Load()
	if boolProp(state, "powered") {
		if at, ok := h.obsPulse[pos]; !ok || now >= at+observerPulseTicks {
			delete(h.obsPulse, pos)
			h.rsSet(players, pos, setBoolProp(state, "powered", false))
			h.scheduleSignalAround(pos)
		}
		return
	}
	dx, dy, dz := obsDelta(state)
	watched := h.rsWorld().At(pos.x+dx, pos.y+dy, pos.z+dz)
	prev, seen := h.obsSeen[pos]
	h.obsSeen[pos] = watched
	if seen && watched != prev {
		h.obsPulse[pos] = now
		h.rsSet(players, pos, setBoolProp(state, "powered", true))
		h.rsSchedule(pos, observerPulseTicks)
		h.scheduleSignalAround(pos)
	}
}

// updateDaylight is DaylightDetectorBlock.updateSignalStrength: the sky
// light reaching the block, less the time-and-weather darkening, bent by the
// sun's angle. A roof over it, or a storm, brings it down — and an inverted
// detector reads the raw sky value back to front, which is why it fires at
// dusk rather than tracking the night's curve.
func (h *hub) updateDaylight(players map[int32]*tracked, pos blockPos, state uint32) {
	sky, _ := h.rsWorld().LightAt(pos.x, pos.y, pos.z)
	n := int(sky) - h.skyDarken()
	if daylightInverted(state) {
		n = 15 - n
	} else if n > 0 {
		f := sunAngle(int64(h.dayTime.Load()))
		target := 0.0
		if f >= math.Pi {
			target = 2 * math.Pi
		}
		f += (target - f) * 0.2
		n = int(math.Round(float64(n) * math.Cos(f)))
	}
	switch {
	case n < 0:
		n = 0
	case n > 15:
		n = 15
	}
	if daylightPower(state) != n {
		h.rsSet(players, pos, daylightWith(daylightInverted(state), n))
		h.scheduleSignalAround(pos)
	}
	h.rsSchedule(pos, 20) // vanilla re-checks every 20 ticks
}

// updatePlates is the per-tick occupancy scan: entities standing on plates
// press them; empty pressed plates release. platesOn tracks what's pressed.
func (h *hub) updatePlates(players map[int32]*tracked) {
	for dim := 0; dim <= 2; dim++ {
		if dim != 0 && h.worldFor(dim) == h.world {
			continue
		}
		h.inDim(dim, func() { h.updatePlatesIn(players, dim) })
	}
}

// updatePlatesIn is one dimension's plate pass (the simulation is pointed at it).
func (h *hub) updatePlatesIn(players map[int32]*tracked, dim int) {
	occupied := map[blockPos]int{}
	feet := func(x, y, z float64, living bool) {
		pos := blockPos{floorInt(x), floorInt(y + 0.01), floorInt(z)}
		s := h.rsWorld().At(pos.x, pos.y, pos.z)
		if !isPlate(s) {
			return
		}
		// Sensitivity: stone-like plates feel living things; wooden and
		// weighted plates feel everything — dropped items and arrows too.
		if _, _, everything, _, _ := plateKind(s); living || everything {
			occupied[pos]++
		}
	}
	for _, t := range players {
		if t.dim == dim {
			feet(t.x, t.y, t.z, true)
		}
	}
	for _, m := range h.mobs {
		if m.dim == dim {
			feet(m.x, m.y, m.z, true)
		}
	}
	for _, it := range h.items {
		if it.dim == dim {
			feet(it.x, it.y, it.z, false)
		}
	}
	for _, a := range h.arrows {
		if a.dim == dim {
			feet(a.x, a.y, a.z, false)
		}
	}
	for pos, n := range occupied {
		s := h.rsWorld().At(pos.x, pos.y, pos.z)
		if ns := plateWith(s, n); ns != s {
			if platePower(s) == 0 {
				_, _, _, on, _ := plateKind(s)
				h.rsSound(players, on, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
				h.vib(h.rsDim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
			}
			h.rsSet(players, pos, ns)
			h.scheduleSignalAround(pos)
			h.platesOn[pos] = h.tick.Load()
		} else if platePower(s) > 0 {
			h.platesOn[pos] = h.tick.Load()
		}
	}
	for pos, last := range h.platesOn {
		// BasePressurePlateBlock.checkPressed schedules the release check
		// getPressedTime (20) ticks after the last thing stood here.
		if occupied[pos] > 0 || h.tick.Load() < last+platePressedTicks {
			continue
		}
		delete(h.platesOn, pos)
		s := h.rsWorld().At(pos.x, pos.y, pos.z)
		if isPlate(s) && platePower(s) > 0 {
			h.rsSet(players, pos, plateWith(s, 0))
			_, _, _, _, off := plateKind(s)
			h.rsSound(players, off, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
			h.vib(h.rsDim, freqBlockDeactivate, pos.x, pos.y, pos.z, 0)
			h.scheduleSignalAround(pos)
		}
	}
}

// useRedstone1b handles right-clicks: cycle repeater delay, toggle comparator
// mode, flip a daylight detector.
func (h *hub) useRedstone1b(players map[int32]*tracked, pos blockPos, state uint32) bool {
	switch {
	case isRepeater(state):
		info, _ := worldgen.InfoForState(state)
		d := worldgen.GetProperty(info, state, "delay")[0] - '0'
		next := d%4 + 1
		h.rsSet(players, pos, worldgen.SetProperty(info, state, "delay", string(rune('0'+next))))
		h.rsSound(players, "minecraft:block.lever.click", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, 1.2)
	case isComparator(state):
		info, _ := worldgen.InfoForState(state)
		mode := "subtract"
		if worldgen.GetProperty(info, state, "mode") == "subtract" {
			mode = "compare"
		}
		h.rsSet(players, pos, worldgen.SetProperty(info, state, "mode", mode))
		h.rsSound(players, "minecraft:block.comparator.click", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, 1.1)
		h.rsSchedule(pos, 1)
	case isDaylight(state):
		h.rsSet(players, pos, daylightWith(!daylightInverted(state), daylightPower(state)))
		h.rsSchedule(pos, 1)
	default:
		return false
	}
	return true
}

func floorInt(v float64) int {
	i := int(v)
	if v < 0 && float64(i) != v {
		i--
	}
	return i
}
