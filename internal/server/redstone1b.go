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

// weightedPlatePressedTicks is WeightedPressurePlateBlock.getPressedTime.
const weightedPlatePressedTicks = 10

// platePressedTime is the plate's getPressedTime.
func platePressedTime(s uint32) uint64 {
	if s >= lightPlateMin && s <= lightPlateMax || s >= heavyPlateMin && s <= heavyPlateMax {
		return weightedPlatePressedTicks
	}
	return platePressedTicks
}

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

// updateRepeater is DiodeBlock.neighborChanged for a repeater: the LOCKED
// property follows the side inputs (RepeaterBlock.updateShape), and an
// unlocked repeater whose output disagrees with its input schedules its tick
// delay×2 later (checkTickOnNeighbor) — at a priority that runs the end of a
// diode chain first, and turning off before turning on.
func (h *hub) updateRepeater(players map[int32]*tracked, pos blockPos, state uint32) {
	if locked := h.repeaterLocked(pos, state); locked != boolProp(state, "locked") {
		state = setBoolProp(state, "locked", locked)
		h.rsSet(players, pos, state)
	}
	if boolProp(state, "locked") {
		return
	}
	on := boolProp(state, "powered")
	if on != (h.diodeInputSignal(pos, state) > 0) && !h.willTickThisTick(pos) {
		prio := tickHigh
		if h.diodePrioritized(pos, state) {
			prio = tickExtremelyHigh
		} else if on {
			prio = tickVeryHigh
		}
		h.scheduleTick(pos, uint64(repeaterDelay(state)), prio)
	}
}

// repeaterTick is DiodeBlock.tick: an input that went away before the tick
// still turns the output on, and a second tick then turns it off — a pulse
// shorter than the delay comes out the delay long, never lost. A locked
// repeater's tick does nothing.
func (h *hub) repeaterTick(players map[int32]*tracked, pos blockPos, state uint32) {
	if h.repeaterLocked(pos, state) {
		return
	}
	on := boolProp(state, "powered")
	in := h.diodeInputSignal(pos, state) > 0
	switch {
	case on && !in:
		state = setBoolProp(state, "powered", false)
	case !on:
		state = setBoolProp(state, "powered", true)
		if !in {
			h.scheduleTick(pos, uint64(repeaterDelay(state)), tickVeryHigh)
		}
	default:
		return
	}
	h.rsSet(players, pos, state)
	h.updateNeighborsInFront(players, pos, state) // DiodeBlock.onPlace
}

// diodePrioritized is DiodeBlock.shouldPrioritize: the block the output
// faces is a diode that does not face the same way (it is not the next link
// of a straight chain).
func (h *hub) diodePrioritized(pos blockPos, state uint32) bool {
	f, ok := propDir(state, "facing")
	if !ok {
		return false
	}
	out := f.opposite()
	dx, dy, dz := out.delta()
	fs := h.rsWorld().At(pos.x+dx, pos.y+dy, pos.z+dz)
	if !isRepeater(fs) && !isComparator(fs) {
		return false
	}
	ff, ok := propDir(fs, "facing")
	return ok && ff != out
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
	h.scheduleSignalAround(players, pos) // relight + let an adjacent comparator re-read
}

// comparatorOutput is ComparatorBlock.calculateOutputSignal: the rear (with
// the analog readings of containers and the like, through one solid block)
// against the strongest side — compare passes the rear if no side is
// stronger, subtract takes the side off.
func (h *hub) comparatorOutput(pos blockPos, state uint32) int {
	dx, dz := facingDelta(stateFacing(state))
	rear := h.diodeInputSignal(pos, state) // DiodeBlock.getInputSignal, then the analog override below
	back := blockPos{pos.x + dx, pos.y, pos.z + dz}
	bs := h.rsWorld().At(back.x, back.y, back.z)
	// ComparatorBlock.getInputSignal: a block with an analog output behind
	// the comparator REPLACES the input (an empty chest reads 0 even when
	// powered); otherwise, through one conductor while the input is under
	// 15, the block beyond it — or an item frame hung on that conductor's far
	// face — does.
	if sig := h.comparatorAnalogAt(back, dx, dz); sig >= 0 {
		rear = sig
	} else if rear < 15 && conducts(bs) {
		beyond := blockPos{back.x + dx, back.y, back.z + dz}
		best := h.comparatorAnalogAt(beyond, dx, dz)
		if f := h.frameAnalogAt(beyond, dx, dz); f > best {
			best = f
		}
		if best >= 0 {
			rear = best
		}
	}
	if rear == 0 {
		return 0
	}
	side := h.diodeSideSignal(pos, state, false) // any source on a comparator's sides
	if side > rear {
		return 0
	}
	info, _ := worldgen.InfoForState(state)
	if worldgen.GetProperty(info, state, "mode") == "subtract" {
		return rear - side
	}
	return rear
}

// comparatorAnalogAt is BlockState.getAnalogOutputSignal read by a
// comparator whose back points (dx, dz) at pos: -1 when the block has no
// analog output (hasAnalogOutputSignal false).
func (h *hub) comparatorAnalogAt(pos blockPos, dx, dz int) int {
	if sig := h.analogSignalFrom(simPos{dim: h.rsDim, blockPos: pos}, -dx, -dz); sig >= 0 {
		return sig // container fullness, cake left, composter level, …
	}
	bs := h.rsWorld().At(pos.x, pos.y, pos.z)
	switch {
	case worldgen.IsCopperBulb(bs): // CopperBulbBlock: lit 15, else 0
		if worldgen.CopperBulbLit(bs) {
			return 15
		}
		return 0
	case isAnySensor(bs): // SculkSensorBlock: the last frequency while active
		if sensorPhase(bs) == sculkPhaseActive {
			return h.sculkFreq[simPos{dim: h.rsDim, blockPos: pos}]
		}
		return 0
	}
	return -1
}

// frameAnalogAt is ComparatorBlock.getItemFrame + ItemFrame.getAnalogOutput:
// the one item frame in the cell facing along the comparator's read
// direction (hung on the conductor's far face) gives rotation % 8 + 1 when
// it holds an item, 0 when empty; -1 when there is not exactly one.
func (h *hub) frameAnalogAt(pos blockPos, dx, dz int) int {
	want, _ := dirFromDelta(dx, 0, dz)
	var found *itemFrame
	n := 0
	for _, f := range h.itemFrames {
		if f.dim != h.rsDim || f.x != pos.x || f.y != pos.y || f.z != pos.z || int(f.dir) != int(want) {
			continue
		}
		found = f
		n++
	}
	if n != 1 {
		return -1
	}
	if found.held.item == 0 || found.held.count <= 0 {
		return 0
	}
	return found.rot%8 + 1
}

// updateComparator is ComparatorBlock.checkTickOnNeighbor: when its output
// or its powered flag would change, it schedules its 2-tick refresh
// (ComparatorBlock.getDelay) — HIGH when its output faces a diode side-on.
func (h *hub) updateComparator(players map[int32]*tracked, pos blockPos, state uint32) {
	if h.willTickThisTick(pos) {
		return
	}
	out := h.comparatorOutput(pos, state)
	if out != h.compOut[h.rsKey(pos)] || boolProp(state, "powered") != (out > 0) {
		prio := tickNormal
		if h.diodePrioritized(pos, state) {
			prio = tickHigh
		}
		h.scheduleTick(pos, comparatorDelay, prio)
	}
}

// comparatorRefresh is ComparatorBlock.refreshOutputState — the tick, and
// the mode click, which runs it at once. (Output > 0 is exactly
// shouldTurnOn: compare needs rear ≥ side, subtract rear > side.)
func (h *hub) comparatorRefresh(players map[int32]*tracked, pos blockPos, state uint32) {
	key := h.rsKey(pos)
	out := h.comparatorOutput(pos, state)
	old := h.compOut[key]
	if out == 0 {
		delete(h.compOut, key)
	} else {
		h.compOut[key] = out
	}
	info, _ := worldgen.InfoForState(state)
	if old == out && worldgen.GetProperty(info, state, "mode") != "compare" {
		return
	}
	if on := out > 0; boolProp(state, "powered") != on {
		state = setBoolProp(state, "powered", on)
		h.rsSet(players, pos, state)
	}
	h.updateNeighborsInFront(players, pos, state)
}

const comparatorDelay = 2 // ComparatorBlock.getDelay(): 2 game ticks

// updateObserver is the observer's side of a neighbour update. Vanilla's
// observer reacts to the SHAPE update its watched block sends
// (observersSee, from every block write); this catches the edits that do
// not come through setBlockAt, by comparing what it sees with what it saw.
func (h *hub) updateObserver(players map[int32]*tracked, pos blockPos, state uint32) {
	// ObserverBlock.onPlace: a POWERED observer with no tick coming to end
	// the pulse — moved by a piston, or saved mid-pulse over a restart (ticks
	// are not saved) — is put right, or it stays powered for good.
	if boolProp(state, "powered") && !h.hasScheduledTick(pos) {
		state = setBoolProp(state, "powered", false)
		h.rsSet(players, pos, state)
		h.updateNeighborsInFront(players, pos, state)
	}
	dx, dy, dz := obsDelta(state)
	watched := h.rsWorld().At(pos.x+dx, pos.y+dy, pos.z+dz)
	prev, seen := h.obsSeen[h.rsKey(pos)]
	h.obsSeen[h.rsKey(pos)] = watched
	if seen && watched != prev && !boolProp(state, "powered") {
		h.observerStart(pos)
	}
}

// observerTick is ObserverBlock.tick: the first tick raises the pulse and
// schedules its end 2 later; the second ends it.
func (h *hub) observerTick(players map[int32]*tracked, pos blockPos, state uint32) {
	if boolProp(state, "powered") {
		state = setBoolProp(state, "powered", false)
	} else {
		state = setBoolProp(state, "powered", true)
		h.scheduleTick(pos, observerPulseTicks, tickNormal)
	}
	h.rsSet(players, pos, state)
	h.updateNeighborsInFront(players, pos, state)
}

// updateDaylight is DaylightDetectorBlock.updateSignalStrength: the sky
// light reaching the block, less the time-and-weather darkening, bent by the
// sun's angle. A roof over it, or a storm, brings it down — and an inverted
// detector reads the raw sky value back to front, which is why it fires at
// dusk rather than tracking the night's curve.
func (h *hub) updateDaylight(players map[int32]*tracked, pos blockPos, state uint32) {
	if h.rsDim != dimOverworld {
		return // dimensionType().hasSkyLight(): the Nether and the End have none, so the power stays put
	}
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
		h.scheduleSignalAround(players, pos)
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

// plateToucher is one entity's box for the plate checks.
type plateToucher struct {
	x, y, z, half, height float64
	living                bool
}

// updatePlatesIn is one dimension's plate pass (the simulation is pointed at
// it): BasePressurePlateBlock.entityInside and tick. An entity in the cell of
// an UNPRESSED plate makes it check at once; a pressed plate checks only on
// its own scheduled tick, getPressedTime (20, weighted 10) after the last
// check. A check counts the entities whose box meets TOUCH_AABB — the plate's
// 14x14 middle, 4/16 tall — leaving spectators out; stone plates feel only
// living things. A weighted plate's power changes only on those checks.
func (h *hub) updatePlatesIn(players map[int32]*tracked, dim int) {
	var ents []plateToucher
	for _, t := range players {
		if t.dim == dim && !t.dead && t.gamemode != gmSpectator {
			ents = append(ents, plateToucher{t.x, t.y, t.z, t.halfWidth(), 1.8, true})
		}
	}
	for _, m := range h.mobs {
		if m.dim == dim && m.dying == 0 && !ignoresBlockTriggers(m) {
			b := m.box()
			ents = append(ents, plateToucher{m.x, m.y, m.z, b.w / 2, b.h, true})
		}
	}
	for _, st := range h.armorStands { // a (non-marker) armour stand is a LivingEntity on a plate
		if st.dim == dim {
			ents = append(ents, plateToucher{st.x, st.y, st.z, 0.25, 1.975, true})
		}
	}
	for _, it := range h.items {
		if it.dim == dim {
			ents = append(ents, plateToucher{it.x, it.y, it.z, 0.125, 0.25, false})
		}
	}
	for _, a := range h.arrows {
		if a.dim == dim {
			ents = append(ents, plateToucher{a.x, a.y, a.z, 0.25, 0.5, false})
		}
	}
	for _, v := range h.vehicles {
		if v.dim == dim {
			w, ht := v.box()
			ents = append(ents, plateToucher{v.x, v.y, v.z, w / 2, ht, false})
		}
	}
	now := h.tick.Load()
	// entityInside: an entity in an unpressed plate's cell.
	inside := map[blockPos]bool{}
	for _, e := range ents {
		pos := blockPos{floorInt(e.x), floorInt(e.y + 0.01), floorInt(e.z)}
		if s := h.rsWorld().At(pos.x, pos.y, pos.z); isPlate(s) && platePower(s) == 0 {
			inside[pos] = true
		}
	}
	for pos := range inside {
		h.plateCheckPressed(players, pos, ents)
	}
	// tick: each pressed plate's scheduled re-check.
	for key, due := range h.platesOn {
		if key.dim != h.rsDim || due > now {
			continue // another dimension's plates are that dimension's business
		}
		delete(h.platesOn, key)
		if s := h.rsWorld().At(key.x, key.y, key.z); isPlate(s) && platePower(s) > 0 {
			h.plateCheckPressed(players, key.blockPos, ents)
		}
	}
}

// plateCheckPressed is BasePressurePlateBlock.checkPressed: recount, write the
// new signal, click and emit on the edges, and book the next check while
// pressed.
func (h *hub) plateCheckPressed(players map[int32]*tracked, pos blockPos, ents []plateToucher) {
	s := h.rsWorld().At(pos.x, pos.y, pos.z)
	_, _, everything, on, off := plateKind(s)
	x0, x1 := float64(pos.x)+1.0/16, float64(pos.x)+15.0/16
	y0, y1 := float64(pos.y), float64(pos.y)+4.0/16
	z0, z1 := float64(pos.z)+1.0/16, float64(pos.z)+15.0/16
	n := 0
	for _, e := range ents {
		if (e.living || everything) && e.x-e.half < x1 && e.x+e.half > x0 && e.z-e.half < z1 && e.z+e.half > z0 &&
			e.y < y1 && e.y+e.height > y0 {
			n++
		}
	}
	was := platePower(s)
	ns := plateWith(s, n)
	if ns != s {
		h.rsSet(players, pos, ns)
		h.scheduleSignalAround(players, pos)
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	switch now := platePower(ns); {
	case now == 0 && was > 0:
		h.rsSound(players, off, sndBlock, cx, cy, cz, 1, 1)
		h.vib(h.rsDim, freqBlockDeactivate, pos.x, pos.y, pos.z, 0)
	case now > 0 && was == 0:
		h.rsSound(players, on, sndBlock, cx, cy, cz, 1, 1)
		h.vib(h.rsDim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
	}
	if platePower(ns) > 0 {
		h.platesOn[h.rsKey(pos)] = h.tick.Load() + platePressedTime(ns)
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
		h.rsSet(players, pos, worldgen.SetProperty(info, state, "delay", string(rune('0'+next)))) // RepeaterBlock: silent
	case isComparator(state):
		info, _ := worldgen.InfoForState(state)
		mode := "subtract"
		if worldgen.GetProperty(info, state, "mode") == "subtract" {
			mode = "compare"
		}
		ns := worldgen.SetProperty(info, state, "mode", mode)
		h.rsSet(players, pos, ns)
		pitch := float32(0.5)
		if mode == "subtract" {
			pitch = 0.55
		}
		h.rsSound(players, "minecraft:block.comparator.click", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, pitch)
		// ComparatorBlock.useWithoutItem: the output refreshes at once.
		h.comparatorRefresh(players, pos, ns)
		h.nbRun(players)
	case isDaylight(state):
		// DaylightDetectorBlock.useWithoutItem: flip, BLOCK_CHANGE, and the
		// signal re-read at once (updateSignalStrength), not a tick later.
		ns := daylightWith(!daylightInverted(state), daylightPower(state))
		h.rsSet(players, pos, ns)
		h.vib(h.rsDim, freqBlockChange, pos.x, pos.y, pos.z, 0)
		h.updateDaylight(players, pos, ns)
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
