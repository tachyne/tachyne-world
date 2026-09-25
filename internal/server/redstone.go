package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone tier 1a: power sources (levers, buttons, torches, redstone
// blocks), dust that carries decaying power, and consumers (lamps, iron
// doors, TNT).
//
// The simulation follows vanilla's split (blockticks.go): a change notifies
// its neighbours at once — updateRedstone is their neighborChanged — and a
// component that waits schedules a tick, which redstoneTick runs. Dust
// re-evaluates inside the cascade, so a line of dust carries its signal end
// to end in the tick the source changes — fifteen blocks, all at once — and
// a lamp at its end lights in that same tick.

const (
	torchDelay   = 2 // RedstoneTorchBlock.neighborChanged: scheduleTick(pos, this, 2)
	lampOffDelay = 4 // RedstoneLampBlock.neighborChanged: scheduleTick(pos, this, 4)
)

// redstoneTick is a scheduled tick (Block.tick) for the redstone family.
// Members without a tick of their own — the older timers that still ride
// the simulation queue — never get here.
func (h *hub) redstoneTick(players map[int32]*tracked, pos blockPos, state uint32) {
	switch {
	case fallBlockState(state):
		h.fallingBlockTick(players, h.rsDim, pos, state) // FallingBlock.tick
	case isStalactite(state):
		h.spawnFallingStalactite(players, h.rsDim, pos, state) // SpeleothemBlock.tick
	case isRSTorch(state):
		h.torchTick(players, pos, state)
	case isLamp(state):
		// RedstoneLampBlock.tick: dark only if the power is still gone.
		if state == lampOn && h.inputPower(pos.x, pos.y, pos.z, false) == 0 {
			h.rsSet(players, pos, lampOff)
		}
	case isRepeater(state):
		h.repeaterTick(players, pos, state)
	case isComparator(state):
		h.comparatorRefresh(players, pos, state)
	case isObserver(state):
		h.observerTick(players, pos, state)
	case isCrafter(state):
		h.crafterTick(players, simPos{dim: h.rsDim, blockPos: pos}, state)
	}
	h.nbRun(players)
}

// torchSupport is the block a torch hangs on or stands on.
func torchSupport(pos blockPos, state uint32) blockPos {
	if state >= rsWallTorchMin && state <= rsWallTorchMax {
		dx, dy, dz := rsWallTorchDir(state).delta()
		return blockPos{pos.x - dx, pos.y - dy, pos.z - dz}
	}
	return blockPos{pos.x, pos.y - 1, pos.z}
}

// torchSupportPowered is RedstoneTorchBlock.hasNeighborSignal (and the wall
// torch's): the torch inverts its support.
func (h *hub) torchSupportPowered(pos blockPos, state uint32) bool {
	s := torchSupport(pos, state)
	return h.supportPowered(s.x, s.y, s.z, pos.x, pos.y, pos.z)
}

// torchTick is RedstoneTorchBlock.tick: flip to match the support, logging
// each lit→unlit flip; the eighth inside 60 ticks burns it out (fizz, and
// look again in 160 ticks).
func (h *hub) torchTick(players map[int32]*tracked, pos blockPos, state uint32) {
	powered := h.torchSupportPowered(pos, state)
	h.pruneTorchToggles()
	switch {
	case torchLit(state) && powered:
		h.rsSet(players, pos, torchWithLit(state, false))
		if h.torchToggledTooOften(pos, true) {
			h.toNearbyEv(players, h.rsDim, float64(pos.x), float64(pos.z), attachproto.WorldFX{Event: worldEventTorchBurnout, X: pos.x, Y: pos.y, Z: pos.z})
			h.scheduleTick(pos, torchRestartDelay, tickNormal)
		}
		h.notifyTwoDeep(players, pos)
	case !torchLit(state) && !powered && !h.torchToggledTooOften(pos, false):
		h.rsSet(players, pos, torchWithLit(state, true))
		h.notifyTwoDeep(players, pos)
	}
}

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
	s := h.rsWorld().At(px, py, pz)
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
			return h.compOut[simPos{dim: h.rsDim, blockPos: blockPos{px, py, pz}}]
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
	case isJukebox(s):
		// JukeboxBlock.getSignal: a full 15 to every side while a disc is
		// actually playing, which is separate from the comparator's reading
		// of WHICH disc it is.
		if jb := h.jukeboxes[simPos{dim: h.rsDim, blockPos: blockPos{px, py, pz}}]; jb != nil && jb.playing(h.tick.Load()) {
			return 15
		}
		return 0
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

// updateRedstone is a redstone-ish cell's neighborChanged: what it does when
// a neighbour tells it something changed. It runs inside the neighbour
// cascade (blockticks.go) — immediately, in the tick of the change — and,
// for the family members vanilla keeps on a timer, only decides whether to
// schedule a tick; redstoneTick is what then happens.
func (h *hub) updateRedstone(players map[int32]*tracked, pos blockPos, state uint32) {
	x, y, z := pos.x, pos.y, pos.z
	// Quasi-connectivity relay: a redstone update at this cell re-evaluates a
	// piston directly below, which reads power through this block (Java QC).
	if pb := (blockPos{x, y - 1, z}); !isPistonBase(state) && isPistonBase(h.rsWorld().At(pb.x, pb.y, pb.z)) {
		h.nbAdd(pb)
	}
	defer h.nbRun(players)
	switch {
	case isTarget(state):
		h.updateTarget(players, pos, state)
	case isTripwireHook(state):
		h.calcHook(players, pos, state) // re-evaluate its line (attached/powered)
	case isCrafter(state):
		h.updateCrafter(players, simPos{dim: h.rsDim, blockPos: pos}, state) // craft on a rising edge

	case isNoteBlock(state):
		// Play once on the rising edge of redstone power (NoteBlockBlock).
		powered := h.inputPower(x, y, z, false) > 0
		if powered != notePowered(state) {
			if powered {
				h.queueNoteEvent(pos) // playNote → level.blockEvent: sounds at the end of the tick
			}
			h.rsSet(players, pos, noteWithPowered(state, powered))
		}
	case isWire(state):
		// DefaultRedstoneWireEvaluator.updatePowerStrength: a changed cell
		// tells everything within two of it, and the dust among them re-
		// evaluates in the same cascade — a line of dust carries a signal end
		// to end in the tick it changes, fifteen blocks at once.
		want := h.inputPower(x, y, z, true)
		if wirePower(state) != want {
			info, _ := worldgen.InfoForState(state)
			ns := worldgen.SetProperty(info, state, "power", itoa(want))
			h.rsSet(players, pos, h.connectWire(x, y, z, ns))
			h.notifyTwoDeep(players, pos)
			break
		}
		// updateShape: the CONNECTIONS are recomputed on any neighbour change,
		// not only when the power moves — dust laid next to unpowered dust
		// has to join up straight away rather than on the first pulse.
		if ns := h.connectWire(x, y, z, state); ns != state {
			h.rsSet(players, pos, ns)
		}
	case isRSTorch(state):
		// RedstoneTorchBlock.neighborChanged: a torch whose state disagrees
		// with its support schedules its tick, 2 later.
		if torchLit(state) == h.torchSupportPowered(pos, state) && !h.willTickThisTick(pos) {
			h.scheduleTick(pos, torchDelay, tickNormal)
		}
	case worldgen.IsCopperBulb(state):
		h.updateCopperBulb(players, pos, state)
	case isSkullBlock(state):
		// AbstractSkullBlock.neighborChanged: POWERED follows the signal.
		// Clients read it — a powered dragon head works its jaw, a piglin
		// head flaps its ears.
		if want := h.inputPower(x, y, z, false) > 0; boolProp(state, "powered") != want {
			h.rsSet(players, pos, setBoolProp(state, "powered", want))
		}
	case isBell(state): // BellBlock.neighborChanged: rings on the rising edge of its input
		want := h.inputPower(x, y, z, false) > 0
		if boolProp(state, "powered") != want {
			if want {
				h.ringBell(players, h.rsDim, pos, -1)
			}
			h.rsSet(players, pos, setBoolProp(state, "powered", want))
		}
	case isLightningRod(state) && boolProp(state, "powered"): // LightningRodBlock.tick: 8 ticks after the strike
		// …and LightningRodBlock.onPlace: a rod that arrives powered with no
		// tick pending (pushed by a piston mid-pulse) switches off.
		if due, ok := h.rsDue[h.rsKey(pos)]; !ok || h.tick.Load() >= due {
			delete(h.rsDue, h.rsKey(pos))
			h.rsSet(players, pos, setBoolProp(state, "powered", false))
			h.scheduleSignalAround(players, pos)
		}
	case isLectern(state) && boolProp(state, "powered"): // LecternBlock.tick: the page-turn pulse ends after 2
		if due, ok := h.rsDue[h.rsKey(pos)]; ok && h.tick.Load() >= due {
			delete(h.rsDue, h.rsKey(pos))
			h.rsSet(players, pos, setBoolProp(state, "powered", false))
			h.scheduleSignalAround(players, pos)
		}
	case isLamp(state):
		// RedstoneLampBlock.neighborChanged: lights at once, and schedules
		// going dark 4 ticks after the power leaves (the tick checks again,
		// so power back in time keeps it lit).
		want := h.inputPower(x, y, z, false) > 0
		switch {
		case want && state == lampOff:
			h.rsSet(players, pos, lampOn)
		case !want && state == lampOn:
			h.scheduleTick(pos, lampOffDelay, tickNormal)
		}
	case isButton(state) && boolProp(state, "powered"):
		// Scheduled unpress: only past the press window (neighbor updates land
		// here too — they must not cut a press short).
		ticks, _, off, wooden := buttonKind(state)
		if at, ok := h.pressedAt[simPos{dim: h.rsDim, blockPos: pos}]; ok && h.tick.Load() >= at+uint64(ticks) {
			if wooden && h.arrowInCell(h.rsDim, pos) { // ButtonBlock.checkPressed: an arrow keeps it down
				h.pressedAt[simPos{dim: h.rsDim, blockPos: pos}] = h.tick.Load()
				h.rsSchedule(pos, uint64(ticks))
				break
			}
			delete(h.pressedAt, simPos{dim: h.rsDim, blockPos: pos})
			h.rsSet(players, pos, setBoolProp(state, "powered", false))
			h.vib(h.rsDim, freqBlockDeactivate, pos.x, pos.y, pos.z, 0)
			h.rsSound(players, off, sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
			h.scheduleSignalAround(players, pos)
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
	case isMovingPiston(state):
		// A live moving cell lands on its own clock (landMovingBlocks); one
		// with no record — left over from before a restart — clears now.
		if _, live := h.movingBlocks[h.rsKey(pos)]; !live {
			h.finishMoving(players, pos)
		}
	case isDispenser(state) || isDropper(state):
		h.updateBinTrigger(players, simPos{dim: h.rsDim, blockPos: pos}, state)
	case isHopper(state):
		h.updateHopper(players, simPos{dim: h.rsDim, blockPos: pos}, state)
	case isWoodShelf(state):
		h.updateShelfPower(players, h.rsDim, pos, state)
	case isAnyRail(state):
		h.updateRail(players, pos, state)
	case isPortalBlock(state):
		h.updatePortalBlock(players, pos, state)
	}
	// Doors/trapdoors/gates respond to power beside redstone-ish updates: the
	// scheduler visits THEM directly (they're in every changed neighbourhood).
	h.updatePoweredOpenable(players, pos, h.rsWorld().At(x, y, z))
}

// updatePoweredOpenable syncs an "open" block (doors, trapdoors, fence gates)
// with arriving power. Only flips when the redstone verdict disagrees.
//
// A door is two blocks and vanilla treats them as one: DoorBlock.neighborChanged
// reads the signal at BOTH halves, so a lever beside the top half opens the
// bottom one too, and each half writes its own state. The engine visits one
// cell, so it has to carry the other half itself — without that, power reached
// whichever half the redstone touched and left the other standing shut.
func (h *hub) updatePoweredOpenable(players map[int32]*tracked, pos blockPos, state uint32) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("open") || !info.HasProperty("powered") {
		return
	}
	other, otherPos, isDoor := h.doorOtherHalf(pos, state, info)
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if isDoor {
		powered = powered || h.inputPower(otherPos.x, otherPos.y, otherPos.z, false) > 0
	}
	if boolProp(state, "powered") == powered {
		return
	}
	wasOpen := boolProp(state, "open")
	h.rsSet(players, pos, setBoolProp(setBoolProp(state, "powered", powered), "open", powered))
	if isDoor {
		if oi, ok := worldgen.InfoForState(other); ok && oi.HasProperty("open") && oi.HasProperty("powered") {
			h.rsSet(players, otherPos, setBoolProp(setBoolProp(other, "powered", powered), "open", powered))
		}
	}
	if wasOpen == powered {
		return // the state moved but the door did not: vanilla is silent
	}
	// DoorBlock / TrapDoorBlock / FenceGateBlock.neighborChanged: the sound
	// at full volume, pitch 0.9–1.0, and BLOCK_OPEN or BLOCK_CLOSE for sculk.
	name, _ := worldgen.StateName(state)
	h.rsSound(players, openCloseSound(name, powered), sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 0.9+h.rng.Float32()*0.1)
	freq := freqBlockClose
	if powered {
		freq = freqBlockOpen
	}
	h.vib(h.rsDim, freq, pos.x, pos.y, pos.z, 0)
}

// placedOpenable is the power half of DoorBlock / TrapDoorBlock /
// FenceGateBlock.getStateForPlacement: a door, trapdoor or gate set down
// where a signal already reaches it is placed open and powered — as its
// placed state, silently, not swung open with a sound by the update that
// follows. Runs in the current simulation dimension.
func (h *hub) placedOpenable(players map[int32]*tracked, pos blockPos) {
	state := h.rsWorld().At(pos.x, pos.y, pos.z)
	if !isPowerOpenable(state) || boolProp(state, "powered") {
		return
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return
	}
	other, otherPos, isDoor := h.doorOtherHalf(pos, state, info)
	if isDoor && !sameBlockFamily(other, state) {
		return // the other half is not down yet; its own placement asks again
	}
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if isDoor {
		powered = powered || h.inputPower(otherPos.x, otherPos.y, otherPos.z, false) > 0
	}
	if !powered {
		return
	}
	h.rsSet(players, pos, setBoolProp(setBoolProp(state, "powered", true), "open", true))
	if isDoor {
		h.rsSet(players, otherPos, setBoolProp(setBoolProp(other, "powered", true), "open", true))
	}
}

// doorOtherHalf is the cell holding a door's other half, if this is a door.
func (h *hub) doorOtherHalf(pos blockPos, state uint32, info worldgen.BlockInfo) (uint32, blockPos, bool) {
	if !info.HasProperty("half") || !info.HasProperty("hinge") {
		return 0, pos, false // a trapdoor has "half" too, but no hinge — it is one block
	}
	op := pos
	if worldgen.GetProperty(info, state, "half") == "upper" {
		op.y--
	} else {
		op.y++
	}
	return h.rsWorld().At(op.x, op.y, op.z), op, true
}

// connectWire stores a dust cell's arms as wireArms computes them, plus the
// "up" flavour where the arm climbs onto a neighbour (the client draws the
// dust running up the block face).
func (h *hub) connectWire(x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	if wireIsDot(state) && !h.wireHasRealArms(x, y, z) {
		return state // getConnectionState: a dot with nothing to connect to stays a dot
	}
	arms := h.wireArms(x, y, z)
	aboveConducts := conducts(h.rsWorld().At(x, y+1, z))
	for _, d := range horizontalDirs {
		dx, _, dz := d.delta()
		v := "none"
		if arms[d] {
			v = "side"
			if !aboveConducts && canHoldDust(h.rsWorld().At(x+dx, y, z+dz)) && isWire(h.rsWorld().At(x+dx, y+1, z+dz)) {
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
	ticks, on, _, _ := buttonKind(state)
	h.pressedAt[simPos{dim: h.rsDim, blockPos: pos}] = h.tick.Load()
	h.rsSet(players, pos, setBoolProp(state, "powered", true))
	h.vib(h.rsDim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
	h.rsSound(players, on, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	h.scheduleSignalAround(players, pos)
	h.rsSchedule(pos, uint64(ticks)) // the unpress timer
}

func (h *hub) toggleLever(players map[int32]*tracked, pos blockPos, state uint32) {
	on := !boolProp(state, "powered")
	h.rsSet(players, pos, setBoolProp(state, "powered", on))
	if on {
		h.vib(h.rsDim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
	} else {
		h.vib(h.rsDim, freqBlockDeactivate, pos.x, pos.y, pos.z, 0)
	}
	pitch := float32(0.5) // LeverBlock.playSound: 0.3 volume, 0.6 on and 0.5 off
	if on {
		pitch = 0.6
	}
	h.rsSound(players, "minecraft:block.lever.click", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, pitch)
	h.scheduleSignalAround(players, pos)
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

// arrowInCell reports a projectile lying in a block's cell (the arrow that
// holds a wooden button down or presses a wooden plate).
func (h *hub) arrowInCell(dim int, pos blockPos) bool {
	for _, a := range h.arrows {
		if a.dim == dim && floorInt(a.x) == pos.x && floorInt(a.y) == pos.y && floorInt(a.z) == pos.z {
			return true
		}
	}
	return false
}
