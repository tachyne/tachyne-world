package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone tier 1a: power sources (levers, buttons, torches, redstone
// blocks), dust that carries decaying power, and consumers (lamps, iron
// doors, TNT). The simulation is a cellular ripple over the existing
// scheduled-update system: every change re-evaluates its neighbourhood next
// tick, so signals propagate one block per tick and converge without a
// global graph. Wire loops can't self-sustain — the -1 decay kills them.

const (
	buttonPressTicks = 20 // stone (wooden 30 — close enough for v1)
)

var (
	wireStateMin = worldgen.BlockBase("redstone_wire") // redstone_wire (east×north×power×south×west)
	wireStateMax = worldgen.BlockBase("redstone_wire") + 1295

	leverStateMin = worldgen.BlockBase("lever")
	leverStateMax = worldgen.BlockBase("lever") + 23

	rsTorchMin     = worldgen.BlockBase("redstone_torch") // floor torch: lit false/true
	rsTorchMax     = worldgen.BlockBase("redstone_torch") + 1
	rsWallTorchMin = worldgen.BlockBase("redstone_wall_torch")
	rsWallTorchMax = worldgen.BlockBase("redstone_wall_torch") + 7

	stoneButtonMin = worldgen.BlockBase("stone_button")
	stoneButtonMax = worldgen.BlockBase("stone_button") + 23
	oakButtonMin   = worldgen.BlockBase("oak_button")
	oakButtonMax   = worldgen.BlockBase("oak_button") + 23

	lampOff = worldgen.BlockBase("redstone_lamp") + 1 // redstone_lamp lit=false (default)
	lampOn  = worldgen.BlockBase("redstone_lamp")

	redstoneBlock = worldgen.BlockBase("redstone_block")
)

func isWire(s uint32) bool  { return s >= wireStateMin && s <= wireStateMax }
func isLever(s uint32) bool { return s >= leverStateMin && s <= leverStateMax }
func isRSTorch(s uint32) bool {
	return (s >= rsTorchMin && s <= rsTorchMax) || (s >= rsWallTorchMin && s <= rsWallTorchMax)
}
func isButton(s uint32) bool {
	return (s >= stoneButtonMin && s <= stoneButtonMax) || (s >= oakButtonMin && s <= oakButtonMax)
}
func isLamp(s uint32) bool { return s == lampOn || s == lampOff }

// isRedstoneish reports whether a state participates in the redstone ripple.
func isRedstoneish(s uint32) bool {
	return isWire(s) || isRSTorch(s) || isLamp(s) || isButton(s) || isTNT(s) ||
		isRepeater(s) || isComparator(s) || isObserver(s) || isPlate(s) || isDaylight(s)
}

// torchLit / torchWithLit handle the redstone torch's lit bit directly (the
// torch isn't in the generated property table): lit is the fastest-varying
// property with values [true, false], so even offsets are lit.
func torchLit(s uint32) bool {
	if s >= rsWallTorchMin && s <= rsWallTorchMax {
		return (s-rsWallTorchMin)%2 == 0
	}
	return s == rsTorchMin
}

func torchWithLit(s uint32, lit bool) uint32 {
	base := s
	if s >= rsWallTorchMin && s <= rsWallTorchMax {
		base = s - (s-rsWallTorchMin)%2
	} else {
		base = rsTorchMin
	}
	if !lit {
		base++
	}
	return base
}

// boolProp reads a boolean block-state property ("powered"/"lit"/"open").
func boolProp(state uint32, name string) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && worldgen.GetProperty(info, state, name) == "true"
}

func setBoolProp(state uint32, name string, v bool) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty(name) {
		return state
	}
	val := "false"
	if v {
		val = "true"
	}
	return worldgen.SetProperty(info, state, name, val)
}

// wirePower extracts a dust cell's current 0-15 signal.
func wirePower(state uint32) int {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0
	}
	p := worldgen.GetProperty(info, state, "power")
	n := 0
	for _, c := range p {
		n = n*10 + int(c-'0')
	}
	return n
}

var rsNeighbors = [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}}

// emitPower is the signal cell (px,py,pz) EMITS toward the receiver at
// (rx,ry,rz): omnidirectional for simple sources and dust, directional for
// repeaters/comparators (front only) and observers (back only).
func (h *hub) emitPower(px, py, pz, rx, ry, rz int) int {
	s := h.world.At(px, py, pz)
	switch {
	case isLever(s) && boolProp(s, "powered"),
		isButton(s) && boolProp(s, "powered"),
		isRSTorch(s) && torchLit(s),
		s == redstoneBlock:
		return 15
	case isWire(s):
		return wirePower(s)
	case isPlate(s):
		return platePower(s)
	case isRepeater(s) && boolProp(s, "powered"):
		dx, dz := facingDelta(stateFacing(s))
		if rx == px-dx && ry == py && rz == pz-dz {
			return 15
		}
	case isComparator(s):
		dx, dz := facingDelta(stateFacing(s))
		if rx == px-dx && ry == py && rz == pz-dz {
			return h.compOut[blockPos{px, py, pz}]
		}
	case isObserver(s) && boolProp(s, "powered"):
		dx, dy, dz := obsDelta(s)
		if rx == px-dx && ry == py-dy && rz == pz-dz {
			return 15
		}
	case isDaylight(s):
		return daylightPower(s)
	case isTarget(s):
		return targetPower(s) // energised target emits to every side
	case isTripwireHook(s) && boolProp(s, "powered"):
		return 15 // a tripped hook powers every side
	case isDetectorRail(s) && railPowered(s):
		return 15
	case isAnySensor(s):
		return sensorPower(s) // active sculk sensor emits its distance-scaled power to all sides
	}
	return 0
}

// inputPower is the strongest signal ARRIVING at a cell, on the vanilla
// model (signal.go): for dust, the evaluator's target strength; for anything
// else, SignalGetter.getBestNeighborSignal — which is how a solid block
// carrying direct power reaches a consumer on its far side.
func (h *hub) inputPower(x, y, z int, forWire bool) int {
	if forWire {
		return h.wireTargetStrength(x, y, z)
	}
	return h.bestNeighborSignal(x, y, z)
}

// supportPowered is RedstoneTorchBlock.hasNeighborSignal: is the torch's
// support block sending power toward the torch? The support is asked with d
// pointing from the torch to it (level.hasSignal(pos.below(), DOWN)). A lit
// torch cannot power its own support — its weak signal is zero toward the
// support and its direct signal goes only upward — so no self-oscillation.
func (h *hub) supportPowered(sx, sy, sz, tx, ty, tz int) bool {
	d, ok := dirFromDelta(sx-tx, sy-ty, sz-tz)
	if !ok {
		return false
	}
	return h.signal(sx, sy, sz, d) > 0
}

// updateRedstone is the scheduled step for any redstone-ish cell.
func (h *hub) updateRedstone(players map[int32]*tracked, pos blockPos, state uint32) {
	x, y, z := pos.x, pos.y, pos.z
	// Quasi-connectivity relay: a redstone update at this cell re-evaluates a
	// piston directly below, which reads power through this block (Java QC).
	if pb := (blockPos{x, y - 1, z}); !isPistonBase(state) && isPistonBase(h.world.At(pb.x, pb.y, pb.z)) {
		h.schedule(pb, 1)
	}
	switch {
	case isTarget(state):
		h.updateTarget(players, pos, state)
	case isTripwireHook(state):
		h.calcHook(players, pos, state) // re-evaluate its line (attached/powered)
	case isCrafter(state):
		h.updateCrafter(players, simPos{blockPos: pos}, state) // craft on a rising edge

	case isNoteBlock(state):
		// Play once on the rising edge of redstone power (NoteBlockBlock).
		powered := h.inputPower(x, y, z, false) > 0
		if powered != notePowered(state) {
			if powered {
				h.playNoteBlock(players, 0, x, y, z, state)
			}
			h.setBlock(players, pos, noteWithPowered(state, powered))
		}
	case isWire(state):
		want := h.inputPower(x, y, z, true)
		if wirePower(state) != want {
			info, _ := worldgen.InfoForState(state)
			ns := worldgen.SetProperty(info, state, "power", itoa(want))
			h.setBlock(players, pos, h.connectWire(x, y, z, ns))
			h.scheduleSignalAround(pos) // ripple on
		}
	case isRSTorch(state):
		// The torch inverts its support block (below for floor torches, the
		// block behind for wall torches): support powered → torch off.
		// (redstone_torch isn't in the orientable-block property table, so its
		// tiny state layout is inlined: lit is the low bit, even = lit.)
		sx, sy, sz := x, y-1, z
		if state >= rsWallTorchMin && state <= rsWallTorchMax {
			facing := []string{"north", "south", "west", "east"}[(state-rsWallTorchMin)/2]
			dx, dz := facingDelta(facing)
			sx, sy, sz = x-dx, y, z-dz
		}
		powered := h.supportPowered(sx, sy, sz, x, y, z)
		h.pruneTorchToggles()
		switch {
		case torchLit(state) && powered:
			h.setBlock(players, pos, torchWithLit(state, false))
			h.scheduleSignalAround(pos)
			if h.torchToggledTooOften(pos, true) { // burn out: fizz, and try again in 160 ticks
				h.toNearbyEv(players, 0, float64(x), float64(z), attachproto.WorldFX{Event: worldEventTorchBurnout, X: x, Y: y, Z: z})
				h.schedule(pos, torchRestartDelay)
			}
		case !torchLit(state) && !powered && !h.torchToggledTooOften(pos, false):
			h.setBlock(players, pos, torchWithLit(state, true))
			h.scheduleSignalAround(pos)
		}
	case worldgen.IsCopperBulb(state):
		h.updateCopperBulb(players, pos, state)
	case isLamp(state):
		want := h.inputPower(x, y, z, false) > 0
		if (state == lampOn) != want {
			ns := uint32(lampOff)
			if want {
				ns = lampOn
			}
			h.setBlock(players, pos, ns)
		}
	case isButton(state) && boolProp(state, "powered"):
		// Scheduled unpress: only past the press window (neighbor updates land
		// here too — they must not cut a press short).
		if at, ok := h.pressedAt[pos]; ok && h.tick.Load() >= at+buttonPressTicks {
			delete(h.pressedAt, pos)
			h.setBlock(players, pos, setBoolProp(state, "powered", false))
			h.playSound(players, "minecraft:block.stone_button.click_off", sndBlock,
				float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 0.5, 0.9)
			h.scheduleSignalAround(pos)
		}
	case isTNT(state):
		if h.inputPower(x, y, z, false) > 0 {
			h.primeTNT(players, x, y, z, tntFuseTicks)
		}
	case isRepeater(state):
		h.updateRepeater(players, pos, state)
	case isComparator(state):
		h.updateComparator(players, pos, state)
	case isObserver(state):
		h.updateObserver(players, pos, state)
	case isDaylight(state):
		h.updateDaylight(players, pos, state)
	case isPistonBase(state):
		h.updatePiston(players, pos, state)
	case isDispenser(state) || isDropper(state):
		h.updateBinTrigger(players, simPos{blockPos: pos}, state)
	case isHopper(state):
		h.updateHopper(players, simPos{blockPos: pos}, state)
	case isAnyRail(state):
		h.updateRail(players, pos, state)
	case isPortalBlock(state):
		h.updatePortalBlock(players, pos, state)
	}
	// Doors/trapdoors/gates respond to power beside redstone-ish updates: the
	// scheduler visits THEM directly (they're in every changed neighbourhood).
	h.updatePoweredOpenable(players, pos, h.world.At(x, y, z))
}

// updatePoweredOpenable syncs an "open" block (iron doors especially) with
// arriving power. Only flips when the redstone verdict disagrees.
func (h *hub) updatePoweredOpenable(players map[int32]*tracked, pos blockPos, state uint32) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("open") || !info.HasProperty("powered") {
		return
	}
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if boolProp(state, "powered") == powered {
		return
	}
	ns := setBoolProp(state, "powered", powered)
	ns = setBoolProp(ns, "open", powered)
	h.setBlock(players, pos, ns)
	h.playSound(players, "minecraft:block.iron_door.open", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.6, 1)
}

// connectWire stores a dust cell's arms as wireArms computes them, plus the
// "up" flavour where the arm climbs onto a neighbour (the client draws the
// dust running up the block face).
func (h *hub) connectWire(x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	arms := h.wireArms(x, y, z)
	aboveConducts := conducts(h.world.At(x, y+1, z))
	for _, d := range horizontalDirs {
		dx, _, dz := d.delta()
		v := "none"
		if arms[d] {
			v = "side"
			if !aboveConducts && canHoldDust(h.world.At(x+dx, y, z+dz)) && isWire(h.world.At(x+dx, y+1, z+dz)) {
				v = "up"
			}
		}
		state = worldgen.SetProperty(info, state, rsDirName[d], v)
	}
	return state
}

// wireConnectsTo is shouldConnectTo(state, direction) with direction the way
// from the dust to the neighbour.
func (h *hub) wireConnectsTo(ns uint32, d rsDir) bool {
	switch {
	case isWire(ns):
		return true
	case isRepeater(ns):
		f, ok := propDir(ns, "facing")
		return ok && (f == d || f.opposite() == d)
	case isObserver(ns):
		f, ok := propDir(ns, "facing")
		return ok && f == d
	}
	return h.isSignalSource(ns)
}

// pressButton / toggleLever are the right-click interactions.
func (h *hub) pressButton(players map[int32]*tracked, pos blockPos, state uint32) {
	if boolProp(state, "powered") {
		return
	}
	h.pressedAt[pos] = h.tick.Load()
	h.setBlock(players, pos, setBoolProp(state, "powered", true))
	h.playSound(players, "minecraft:block.stone_button.click_on", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 1)
	h.scheduleSignalAround(pos)
	h.schedule(pos, buttonPressTicks) // the unpress timer
}

func (h *hub) toggleLever(players map[int32]*tracked, pos blockPos, state uint32) {
	on := !boolProp(state, "powered")
	h.setBlock(players, pos, setBoolProp(state, "powered", on))
	pitch := float32(0.9)
	if on {
		pitch = 1.1
	}
	h.playSound(players, "minecraft:block.lever.click", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, pitch)
	h.scheduleSignalAround(pos)
}

type evUseRedstone struct {
	eid     int32
	x, y, z int
}

func (evUseRedstone) isHubEvent() {}

func itoa(n int) string {
	if n <= 9 {
		return string(rune('0' + n))
	}
	return "1" + string(rune('0'+n-10))
}

// Redstone torch burnout (RedstoneTorchBlock.isToggledTooFrequently): every
// lit→unlit flip is logged; toggles older than 60 ticks are forgotten; a
// torch with eight logged flips stays dark (fizzing, level event 1502) and
// is re-tried 160 ticks later — which is what stops a torch clock or an
// inverter loop from ticking forever.
const (
	torchToggleWindow      = 60
	torchMaxRecentToggles  = 8
	torchRestartDelay      = 160
	worldEventTorchBurnout = 1502
)

type torchToggle struct {
	pos  blockPos
	when uint64
}

func (h *hub) pruneTorchToggles() {
	now := h.tick.Load()
	i := 0
	for i < len(h.torchToggles) && now-h.torchToggles[i].when > torchToggleWindow {
		i++
	}
	h.torchToggles = h.torchToggles[i:]
}

// torchToggledTooOften is isToggledTooFrequently: optionally log this flip,
// then report whether the position has eight or more recent ones.
func (h *hub) torchToggledTooOften(pos blockPos, add bool) bool {
	if add {
		h.torchToggles = append(h.torchToggles, torchToggle{pos: pos, when: h.tick.Load()})
	}
	n := 0
	for _, tg := range h.torchToggles {
		if tg.pos == pos {
			if n++; n >= torchMaxRecentToggles {
				return true
			}
		}
	}
	return false
}
