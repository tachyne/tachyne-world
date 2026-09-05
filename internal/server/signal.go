package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone signal model — a port of vanilla's SignalGetter and each block's
// getSignal / getDirectSignal rules, so power behaves the way players expect
// from the real game:
//
//   - A source emits WEAK power to its neighbours (getSignal). Weak power
//     lights a lamp, opens a door, fires a piston — but does not travel any
//     further through a solid block.
//   - Some sources also emit DIRECT ("strong") power into ONE neighbour
//     (getDirectSignal): a lever or button into the block it is attached to,
//     a torch into the block above it, a plate, detector rail, lectern or
//     sculk sensor into the block beneath, a repeater / comparator /
//     observer out of its front, and dust into the block it points at.
//   - A block that CONDUCTS (isRedstoneConductor — a full collision cube,
//     minus vanilla's exemptions) receiving direct power becomes a source
//     itself: it emits that strength weakly on every other side. This is
//     what lets a lever behind a wall drive the lamp on the far side, and it
//     is exactly what the engine lacked — every consumer used to see only
//     sources it touched directly.
//
// Direction convention (vanilla's): the `d` passed to a signal query points
// FROM the receiver TOWARD the emitter. `level.getSignal(pos.below(), DOWN)`
// asks the block below what it sends upward. Every rule below is written in
// those terms, so it can be checked line by line against the reference.

// rsDir is a vanilla Direction, in vanilla's order.
type rsDir int

const (
	dDown rsDir = iota
	dUp
	dNorth
	dSouth
	dWest
	dEast
)

var rsDelta = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
var rsDirName = [6]string{"down", "up", "north", "south", "west", "east"}

func (d rsDir) opposite() rsDir { return d ^ 1 }
func (d rsDir) delta() (int, int, int) {
	v := rsDelta[d]
	return v[0], v[1], v[2]
}

// dirOf parses a facing property value.
func dirOf(name string) (rsDir, bool) {
	for i, n := range rsDirName {
		if n == name {
			return rsDir(i), true
		}
	}
	return 0, false
}

// horizontalDirs are vanilla's Direction.Plane.HORIZONTAL, in its order.
var horizontalDirs = [4]rsDir{dNorth, dSouth, dWest, dEast}

// clockwise is Direction.getClockWise for the horizontal plane.
func (d rsDir) clockwise() rsDir {
	switch d {
	case dNorth:
		return dEast
	case dEast:
		return dSouth
	case dSouth:
		return dWest
	default:
		return dNorth
	}
}

// ---- the conductor rule -------------------------------------------------

// neverConducts are vanilla's isRedstoneConductor(Blocks::never) overrides
// that are (or can be) full collision cubes — glass and its kin, ice, the
// glowing blocks, pistons, TNT, leaves, the copper grates and bulbs. Names
// absent from this version's registry simply don't resolve.
var neverConductNames = []string{
	"glass", "tinted_glass",
	"white_stained_glass", "orange_stained_glass", "magenta_stained_glass", "light_blue_stained_glass",
	"yellow_stained_glass", "lime_stained_glass", "pink_stained_glass", "gray_stained_glass",
	"light_gray_stained_glass", "cyan_stained_glass", "purple_stained_glass", "blue_stained_glass",
	"brown_stained_glass", "green_stained_glass", "red_stained_glass", "black_stained_glass",
	"ice", "frosted_ice", "glowstone", "sea_lantern", "beacon", "redstone_block",
	"observer", "repeater", "piston", "sticky_piston", "moving_piston", "tnt",
	"chorus_flower", "bamboo", "scaffolding", "powder_snow", "pointed_dripstone",
	"copper_grate", "exposed_copper_grate", "weathered_copper_grate", "oxidized_copper_grate",
	"waxed_copper_grate", "waxed_exposed_copper_grate", "waxed_weathered_copper_grate", "waxed_oxidized_copper_grate",
	"copper_bulb", "exposed_copper_bulb", "weathered_copper_bulb", "oxidized_copper_bulb",
	"waxed_copper_bulb", "waxed_exposed_copper_bulb", "waxed_weathered_copper_bulb", "waxed_oxidized_copper_bulb",
}

// alwaysConductNames are the isRedstoneConductor(Blocks::always) overrides:
// blocks that are NOT full cubes but conduct anyway.
var alwaysConductNames = []string{"soul_sand", "mud"}

// The lightning rod family (weathering + waxed): a strong source into the
// block it is mounted on for eight ticks after a strike.
var lightningRodNames = []string{
	"lightning_rod", "exposed_lightning_rod", "weathered_lightning_rod", "oxidized_lightning_rod",
	"waxed_lightning_rod", "waxed_exposed_lightning_rod", "waxed_weathered_lightning_rod", "waxed_oxidized_lightning_rod",
}

var lightningRodRanges []stateRange

func isLightningRod(s uint32) bool { return inRanges(lightningRodRanges, s) }

var neverConductRanges, alwaysConductRanges []stateRange

func init() {
	for _, n := range neverConductNames {
		if lo, hi := worldgen.BlockRange(n); lo != 0 {
			neverConductRanges = append(neverConductRanges, stateRange{lo, hi})
		}
	}
	for _, n := range alwaysConductNames {
		if lo, hi := worldgen.BlockRange(n); lo != 0 {
			alwaysConductRanges = append(alwaysConductRanges, stateRange{lo, hi})
		}
	}
	for _, n := range lightningRodNames {
		if lo, hi := worldgen.BlockRange(n); lo != 0 {
			lightningRodRanges = append(lightningRodRanges, stateRange{lo, hi})
		}
	}
}

// dirFromDelta maps a unit offset to its rsDir.
func dirFromDelta(dx, dy, dz int) (rsDir, bool) {
	for d := dDown; d <= dEast; d++ {
		if v := rsDelta[d]; v[0] == dx && v[1] == dy && v[2] == dz {
			return d, true
		}
	}
	return 0, false
}

// conducts is BlockState.isRedstoneConductor: the default is "the collision
// shape is the full cube" (per state — a double slab yes, a single slab no,
// an extended piston no), with vanilla's explicit exemptions and additions.
func conducts(state uint32) bool {
	if state == worldgen.Air {
		return false
	}
	if worldgen.IsLeaves(state) || inRanges(neverConductRanges, state) {
		return false
	}
	if inRanges(alwaysConductRanges, state) {
		return true
	}
	return worldgen.IsFullCube(state)
}

// ---- per-block rules ----------------------------------------------------

// propDir reads a direction-valued property ("facing", "face" mapped by the
// caller) as an rsDir.
func propDir(state uint32, prop string) (rsDir, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0, false
	}
	return dirOf(worldgen.GetProperty(info, state, prop))
}

// attachedDir is FaceAttachedHorizontalDirectionalBlock.getConnectedDirection
// for levers and buttons: UP for a floor switch, DOWN for a ceiling one, its
// facing for a wall one — the direction from the SUPPORT to the switch,
// which is exactly the d the support asks with when it queries direct power
// (receiver below → emitter above → d = UP). A switch drives its support.
func attachedDir(state uint32) (rsDir, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0, false
	}
	switch worldgen.GetProperty(info, state, "face") {
	case "ceiling":
		return dDown, true
	case "floor":
		return dUp, true
	}
	return dirOf(worldgen.GetProperty(info, state, "facing"))
}

// rsWallTorchDir is the wall torch's FACING (the direction it points, away
// from its support).
func rsWallTorchDir(state uint32) rsDir {
	return [4]rsDir{dNorth, dSouth, dWest, dEast}[(state-rsWallTorchMin)/2]
}

// wireArms is RedStoneWireBlock.getConnectionState: which of a dust cell's
// four arms connect, computed from the WORLD — vanilla recomputes this on
// every signal query rather than trusting the stored state. Two rules do
// most of the work: an arm connects toward dust (level, one up over a
// supporting block when nothing solid caps this cell, one down past a
// non-conductor), toward a repeater along its axis, an observer at its
// face, or any other signal source; and a cell with no connection on one
// axis extends both arms along the other, so a straight line ends "side"
// and a lone dot is a cross. That second rule is why dust ending beside a
// lamp powers it.
func (h *hub) wireArms(x, y, z int) [6]bool {
	var arms [6]bool
	aboveConducts := conducts(h.world.At(x, y+1, z))
	for _, d := range horizontalDirs {
		dx, _, dz := d.delta()
		nx, nz := x+dx, z+dz
		ns := h.world.At(nx, y, nz)
		if !aboveConducts && canHoldDust(ns) && isWire(h.world.At(nx, y+1, nz)) {
			arms[d] = true // climbs onto the neighbour
			continue
		}
		if h.wireConnectsTo(ns, d) {
			arms[d] = true
		} else if !conducts(ns) && isWire(h.world.At(nx, y-1, nz)) {
			arms[d] = true // steps down past a non-conductor
		}
	}
	noNS := !arms[dNorth] && !arms[dSouth]
	noEW := !arms[dEast] && !arms[dWest]
	if noNS {
		arms[dWest], arms[dEast] = true, true
	}
	if noEW {
		arms[dNorth], arms[dSouth] = true, true
	}
	return arms
}

// canHoldDust approximates RedStoneWireBlock.canSurviveOn (a sturdy top
// face, or a hopper): full cubes and hoppers. Upside-down stairs and top
// slabs also qualify in vanilla and are not covered here.
func canHoldDust(s uint32) bool { return worldgen.IsFullCube(s) || isHopper(s) }

// ownSignal is BlockBehaviour.ownSignal: a source's omnidirectional strength.
func (h *hub) ownSignal(x, y, z int, s uint32) int {
	switch {
	case isLever(s) || isButton(s):
		if boolProp(s, "powered") {
			return 15
		}
	case isRSTorch(s):
		if torchLit(s) {
			return 15
		}
	case isWire(s):
		return wirePower(s)
	case isRepeater(s):
		if boolProp(s, "powered") {
			return 15
		}
	case isComparator(s):
		return h.compOut[blockPos{x, y, z}]
	case isObserver(s):
		if boolProp(s, "powered") {
			return 15
		}
	case s == redstoneBlock:
		return 15
	case isPlate(s):
		return platePower(s)
	case isDetectorRail(s):
		if railPowered(s) {
			return 15
		}
	case isLectern(s), isTripwireHook(s):
		if boolProp(s, "powered") {
			return 15
		}
	case isDaylight(s):
		return daylightPower(s)
	case isTarget(s):
		return targetPower(s)
	case isAnySensor(s):
		return sensorPower(s)
	case isLightningRod(s):
		if boolProp(s, "powered") {
			return 15
		}
	}
	return 0
}

// isSignalSource is BlockBehaviour.isSignalSource.
func (h *hub) isSignalSource(s uint32) bool {
	if isWire(s) {
		return !h.wiresSilent
	}
	return isLever(s) || isButton(s) || isRSTorch(s) || isRepeater(s) || isComparator(s) ||
		isObserver(s) || s == redstoneBlock || isPlate(s) || isDetectorRail(s) || isLectern(s) ||
		isTripwireHook(s) || isDaylight(s) || isTarget(s) || isAnySensor(s) || isLightningRod(s)
}

// weakSignal is BlockState.getSignal(level, pos, d): what the block at
// (x,y,z) sends toward a receiver that lies in direction d.opposite — d
// pointing from that receiver to this block.
func (h *hub) weakSignal(x, y, z int, s uint32, d rsDir) int {
	switch {
	case isWire(s):
		// RedStoneWireBlock.getSignal: nothing downward; upward the full
		// strength; sideways only where an arm connects toward the receiver.
		if h.wiresSilent || d == dDown {
			return 0
		}
		p := wirePower(s)
		if p == 0 {
			return 0
		}
		if d != dUp && !h.wireArms(x, y, z)[d.opposite()] {
			return 0
		}
		return p
	case isRSTorch(s):
		if s >= rsWallTorchMin && s <= rsWallTorchMax {
			if rsWallTorchDir(s) == d { // never into its own support
				return 0
			}
		} else if d == dUp { // a floor torch never powers the block it stands on
			return 0
		}
		return h.ownSignal(x, y, z, s)
	case isRepeater(s), isComparator(s), isObserver(s):
		// DiodeBlock / ObserverBlock.getSignal: out of the front only. FACING
		// points at the INPUT (vanilla and tachyne agree), so the receiver in
		// front sees this block in direction FACING.
		if f, ok := propDir(s, "facing"); ok && f == d {
			return h.ownSignal(x, y, z, s)
		}
		return 0
	case isCalibSensor(s):
		if f, ok := propDir(s, "facing"); ok && f == d {
			return 0
		}
		return h.ownSignal(x, y, z, s)
	}
	return h.ownSignal(x, y, z, s)
}

// directSignal is BlockState.getDirectSignal(level, pos, d): the strong
// power this block drives INTO the neighbour in direction d.opposite (d
// again from the receiver's point of view).
func (h *hub) directSignal(x, y, z int, s uint32, d rsDir) int {
	switch {
	case isLever(s) || isButton(s):
		if a, ok := attachedDir(s); ok && a == d && boolProp(s, "powered") {
			return 15
		}
	case isRSTorch(s):
		if d == dDown { // the block ABOVE the torch (it asks with d = DOWN)
			return h.weakSignal(x, y, z, s, d)
		}
	case isWire(s):
		if h.wiresSilent {
			return 0
		}
		return h.weakSignal(x, y, z, s, d)
	case isRepeater(s), isComparator(s), isObserver(s):
		return h.weakSignal(x, y, z, s, d)
	case isPlate(s), isDetectorRail(s), isLectern(s), isAnySensor(s):
		if d == dUp { // the block BENEATH (it asks with d = UP)
			return h.weakSignal(x, y, z, s, d)
		}
	case isTripwireHook(s), isLightningRod(s):
		if f, ok := propDir(s, "facing"); ok && f == d && boolProp(s, "powered") {
			return 15
		}
	}
	return 0
}

// ---- SignalGetter -------------------------------------------------------

// directInto is SignalGetter.getDirectSignalTo(pos): the strongest direct
// power any neighbour drives into (x,y,z).
func (h *hub) directInto(x, y, z int) int {
	best := 0
	for d := dDown; d <= dEast; d++ {
		dx, dy, dz := d.delta()
		nx, ny, nz := x+dx, y+dy, z+dz
		if v := h.directSignal(nx, ny, nz, h.world.At(nx, ny, nz), d); v > best {
			best = v
			if best >= 15 {
				return 15
			}
		}
	}
	return best
}

// signal is SignalGetter.getSignal(pos, d): the block's own weak output in
// that direction, or — if it conducts — the strong power it is receiving,
// whichever is greater.
func (h *hub) signal(x, y, z int, d rsDir) int {
	s := h.world.At(x, y, z)
	v := h.weakSignal(x, y, z, s, d)
	if conducts(s) {
		if di := h.directInto(x, y, z); di > v {
			return di
		}
	}
	return v
}

// hasNeighborSignal is SignalGetter.hasNeighborSignal: is (x,y,z) powered by
// anything around it? The consumers' question — lamp, door, TNT, dispenser.
func (h *hub) hasNeighborSignal(x, y, z int) bool {
	for d := dDown; d <= dEast; d++ {
		dx, dy, dz := d.delta()
		if h.signal(x+dx, y+dy, z+dz, d) > 0 {
			return true
		}
	}
	return false
}

// bestNeighborSignal is SignalGetter.getBestNeighborSignal.
func (h *hub) bestNeighborSignal(x, y, z int) int {
	best := 0
	for d := dDown; d <= dEast; d++ {
		dx, dy, dz := d.delta()
		v := h.signal(x+dx, y+dy, z+dz, d)
		if v >= 15 {
			return 15
		}
		if v > best {
			best = v
		}
	}
	return best
}

// controlInputSignal is SignalGetter.getControlInputSignal: what a diode
// reads from a side block at (x,y,z) lying in direction d from it.
func (h *hub) controlInputSignal(x, y, z int, d rsDir, onlyDiodes bool) int {
	s := h.world.At(x, y, z)
	switch {
	case onlyDiodes:
		if isRepeater(s) || isComparator(s) {
			return h.directSignal(x, y, z, s, d)
		}
		return 0
	case s == redstoneBlock:
		return 15
	case isWire(s):
		return wirePower(s)
	case h.isSignalSource(s):
		return h.directSignal(x, y, z, s, d)
	}
	return 0
}

// ---- dust -----------------------------------------------------------------

// wireTargetStrength is DefaultRedstoneWireEvaluator.calculateTargetStrength:
// the strength this dust cell should carry.
func (h *hub) wireTargetStrength(x, y, z int) int {
	block := h.wireBlockSignal(x, y, z)
	if block == 15 {
		return 15
	}
	if in := h.incomingWireSignal(x, y, z); in > block {
		return in
	}
	return block
}

// wireBlockSignal is RedStoneWireBlock.getBlockSignal: the best neighbour
// signal with every dust cell silenced — dust listens to sources and to
// strongly powered blocks, never to other dust here (that is the -1 path).
// Silencing dust also silences the block a dust cell points INTO, which is
// why wire→block→wire does not carry: the block's direct input was dust.
func (h *hub) wireBlockSignal(x, y, z int) int {
	h.wiresSilent = true
	v := h.bestNeighborSignal(x, y, z)
	h.wiresSilent = false
	return v
}

// incomingWireSignal is RedstoneWireEvaluator.getIncomingWireSignal: the
// strongest neighbouring dust — level, one up over a conductor when nothing
// solid caps this cell, one down past a non-conductor — minus one.
func (h *hub) incomingWireSignal(x, y, z int) int {
	wireAt := func(px, py, pz int) int {
		if s := h.world.At(px, py, pz); isWire(s) {
			return wirePower(s)
		}
		return 0
	}
	best := 0
	aboveConducts := conducts(h.world.At(x, y+1, z))
	for _, d := range horizontalDirs {
		dx, _, dz := d.delta()
		nx, nz := x+dx, z+dz
		if v := wireAt(nx, y, nz); v > best {
			best = v
		}
		if nc := conducts(h.world.At(nx, y, nz)); nc && !aboveConducts {
			if v := wireAt(nx, y+1, nz); v > best {
				best = v
			}
		} else if !nc {
			if v := wireAt(nx, y-1, nz); v > best {
				best = v
			}
		}
	}
	if best-1 < 0 {
		return 0
	}
	return best - 1
}

// ---- diodes ---------------------------------------------------------------

// diodeInputSignal is DiodeBlock.getInputSignal: the signal arriving at the
// back, with dust read at full strength even where its arm does not point.
func (h *hub) diodeInputSignal(pos blockPos, s uint32) int {
	f, ok := propDir(s, "facing")
	if !ok {
		return 0
	}
	dx, dy, dz := f.delta()
	bx, by, bz := pos.x+dx, pos.y+dy, pos.z+dz
	in := h.signal(bx, by, bz, f)
	if in >= 15 {
		return 15
	}
	if bs := h.world.At(bx, by, bz); isWire(bs) {
		if p := wirePower(bs); p > in {
			return p
		}
	}
	return in
}

// diodeSideSignal is DiodeBlock.getAlternateSignal: the stronger of the two
// side inputs — diodes only for a repeater (that is what locks it), any
// source for a comparator.
func (h *hub) diodeSideSignal(pos blockPos, s uint32, onlyDiodes bool) int {
	f, ok := propDir(s, "facing")
	if !ok {
		return 0
	}
	best := 0
	for _, side := range [2]rsDir{f.clockwise(), f.clockwise().opposite()} {
		dx, _, dz := side.delta()
		if v := h.controlInputSignal(pos.x+dx, pos.y, pos.z+dz, side, onlyDiodes); v > best {
			best = v
		}
	}
	return best
}

// ---- pistons -------------------------------------------------------------

// pistonHasSignal is PistonBaseBlock.getNeighborSignal, quasi-connectivity
// and all: any side but the push face, then the same for the block above
// (except from below).
func (h *hub) pistonHasSignal(pos blockPos, push rsDir) bool {
	for d := dDown; d <= dEast; d++ {
		if d == push {
			continue
		}
		dx, dy, dz := d.delta()
		if h.signal(pos.x+dx, pos.y+dy, pos.z+dz, d) > 0 {
			return true
		}
	}
	ax, ay, az := pos.x, pos.y+1, pos.z
	for d := dDown; d <= dEast; d++ {
		if d == dDown {
			continue
		}
		dx, dy, dz := d.delta()
		if h.signal(ax+dx, ay+dy, az+dz, d) > 0 {
			return true
		}
	}
	return false
}

// ---- scheduling -----------------------------------------------------------

// scheduleSignalAround re-evaluates everything a signal change at pos can
// reach next tick: the six neighbours as before, and — because direct power
// crosses a conductor — the neighbours of every conducting neighbour too.
func (h *hub) scheduleSignalAround(pos blockPos) {
	h.scheduleAround(pos, 1)
	for d := dDown; d <= dEast; d++ {
		dx, dy, dz := d.delta()
		n := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
		if conducts(h.world.At(n.x, n.y, n.z)) {
			h.scheduleAround(n, 1)
		}
	}
}
