package server

import (
	"math"
	"strconv"

	"github.com/tachyne/tachyne-world/internal/world"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Pointed dripstone: it grows, it drips, and it hurts to land on.
//
// A stalactite hanging from a dripstone block slowly lengthens, or grows a
// stalagmite up from the floor beneath it. Water or lava standing above the
// stalactite drips through and fills a cauldron under the tip. And a stalagmite
// tip is the one block in the game that makes a fall WORSE — landing on one
// counts as two and a half blocks further and doubles the damage.

var (
	dripstoneMin, dripstoneMax, _ = worldgen.BlockRangeOK("pointed_dripstone")
	dripstoneBlock                = worldgen.BlockBase("dripstone_block")
)

const (
	// The state layout is thickness(5) x vertical_direction(2) x waterlogged(2),
	// waterlogged varying fastest, so these are the digit strides.
	dripThickStride = 4
	dripDirStride   = 2

	dripGrowChance  = 0.011377778 // vanilla randomTick roll for a stalactite
	dripWaterChance = 0.17578125  // …and the roll that sends a drop of water down
	dripLavaChance  = 0.05859375  // …or of lava
	dripMaxGrow     = 7           // how far a stalactite will search for its tip
	dripMaxDrip     = 11          // …and how far a drop will travel to find one
	dripStalagFloor = 10          // how far below a tip a stalagmite may start
	dripFallBonus   = 2.5         // a stalagmite tip adds this to the fall
	dripFallScale   = 2.0         // …and doubles what it deals
)

// dripstoneParts pulls a pointed dripstone state apart. Thickness is the index
// into [tip_merge tip frustum middle base]; up reports which way the point aims.
func dripstoneParts(st uint32) (thickness int, up, waterlogged, ok bool) {
	if dripstoneMax == 0 || st < dripstoneMin || st > dripstoneMax {
		return 0, false, false, false
	}
	off := st - dripstoneMin
	return int(off / dripThickStride), (off/dripDirStride)%2 == 0, off%2 == 0, true
}

// dripstoneState builds one back up.
func dripstoneState(thickness int, up, waterlogged bool) uint32 {
	st := dripstoneMin + uint32(thickness)*dripThickStride
	if !up {
		st += dripDirStride
	}
	if !waterlogged {
		st++
	}
	return st
}

const (
	dripTipMerge = 0
	dripTip      = 1
)

// isStalactite reports a dripstone pointing DOWN — the hanging kind.
func isStalactite(st uint32) bool {
	_, up, _, ok := dripstoneParts(st)
	return ok && !up
}

// isStalagmiteTip reports the standing point that impales a falling entity.
func isStalagmiteTip(st uint32) bool {
	th, up, _, ok := dripstoneParts(st)
	return ok && up && th == dripTip
}

// dripstoneTip walks down a stalactite to its tip, within maxLen.
func dripstoneTip(w interface{ At(x, y, z int) uint32 }, x, y, z, maxLen int) (int, bool) {
	for i := 0; i < maxLen; i++ {
		st := w.At(x, y-i, z)
		th, up, _, ok := dripstoneParts(st)
		if !ok || up {
			return 0, false
		}
		if th == dripTip || th == dripTipMerge {
			return y - i, true
		}
	}
	return 0, false
}

// speleoFamily is one SpeleothemBlock: pointed dripstone, which grows only
// from a dripstone block with a water source on top of it and carries water
// and lava down to a cauldron, and the sulfur spike, which grows from sulfur
// and at most two long.
type speleoFamily struct {
	min       uint32 // its first state (the same thickness × direction × waterlogged layout)
	growOn    uint32 // the block it grows from (blockToGrowOn)
	maxGrow   int    // getMaxGrowthLength
	dripstone bool
}

var (
	sulfurSpikeMin, _, sulfurSpikeOK = worldgen.BlockRangeOK("sulfur_spike")
	sulfurBlockState                 = worldgen.BlockBase("sulfur")

	speleoFamilies = []speleoFamily{
		{min: dripstoneMin, growOn: dripstoneBlock, maxGrow: dripMaxGrow, dripstone: true},
		{min: sulfurSpikeMin, growOn: sulfurBlockState, maxGrow: 2},
	}
)

// speleoOf pulls a speleothem state apart: its family, thickness index
// (tip_merge tip frustum middle base), which way it points, waterlogged.
func speleoOf(st uint32) (f *speleoFamily, thickness int, up, waterlogged, ok bool) {
	for i := range speleoFamilies {
		fam := &speleoFamilies[i]
		if fam.min == 0 || (fam.min == sulfurSpikeMin && !sulfurSpikeOK) || st < fam.min || st >= fam.min+20 {
			continue
		}
		off := st - fam.min
		return fam, int(off / dripThickStride), (off/dripDirStride)%2 == 0, off%2 == 0, true
	}
	return nil, 0, false, false, false
}

// state builds a state of the family.
func (f *speleoFamily) state(thickness int, up, waterlogged bool) uint32 {
	return dripstoneState(thickness, up, waterlogged) - dripstoneMin + f.min
}

// tickDripstone is the random tick of a speleothem (SpeleothemBlock.
// randomTick, and PointedDripstoneBlock's fluid transfer before it).
func (h *hub) tickDripstone(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	fam, _, _, _, ok := speleoOf(state)
	if !ok {
		return false
	}
	pos := blockPos{x, y, z}
	if fam.dripstone {
		h.dripstoneTransfer(players, dim, pos, state, h.rng.Float32())
	}
	if h.rng.Float32() < dripGrowChance && h.stalactiteStart(dim, pos, state) {
		h.growSpeleothem(players, dim, fam, pos)
	}
	return true
}

// stalactiteStart is isStalactiteStartPos: a speleothem pointing down with
// no more of the same block above it.
func (h *hub) stalactiteStart(dim int, pos blockPos, state uint32) bool {
	fam, _, up, _, ok := speleoOf(state)
	if !ok || up {
		return false
	}
	above, _, _, _, ok := speleoOf(h.worldFor(dim).At(pos.x, pos.y+1, pos.z))
	return !ok || above != fam
}

// speleoTip is findTip: the speleothem at pos if it is a tip, else the
// first tip along its direction within maxLen (the path of the same block
// pointing the same way).
func (h *hub) speleoTip(dim int, pos blockPos, state uint32, maxLen int, includeMerged bool) (blockPos, bool) {
	fam, th, up, _, _ := speleoOf(state)
	isTip := func(t int) bool { return t == dripTip || (includeMerged && t == dripTipMerge) }
	if isTip(th) {
		return pos, true
	}
	dy := -1
	if up {
		dy = 1
	}
	w := h.worldFor(dim)
	p := pos
	for i := 1; i < maxLen; i++ {
		p.y += dy
		f2, t2, up2, _, ok := speleoOf(w.At(p.x, p.y, p.z))
		if ok && isTip(t2) {
			return p, true
		}
		if !h.inWorldYIn(dim, p.y) || !ok || f2 != fam || up2 != up {
			return blockPos{}, false
		}
	}
	return blockPos{}, false
}

// growSpeleothem is growStalactiteOrStalagmiteIfPossible.
func (h *hub) growSpeleothem(players map[int32]*tracked, dim int, fam *speleoFamily, start blockPos) {
	w := h.worldFor(dim)
	// canGrow: the block it grows from above it — and for dripstone a water
	// source on top of that.
	if w.At(start.x, start.y+1, start.z) != fam.growOn {
		return
	}
	if fam.dripstone && !isWaterSource(w.At(start.x, start.y+2, start.z)) {
		return
	}
	tip, ok := h.speleoTip(dim, start, w.At(start.x, start.y, start.z), fam.maxGrow, false)
	if !ok {
		return
	}
	ts := w.At(tip.x, tip.y, tip.z)
	if th, up, wl, _ := speleoParts(ts); up || th != dripTip || wl || !h.speleoTipCanGrow(dim, fam, tip, ts) {
		return // isFreeHangingStalactite && canTipGrow
	}
	if h.rng.Intn(2) == 0 { // random.nextBoolean
		h.speleoGrow(players, dim, fam, tip, false)
	} else {
		h.growStalagmiteBelow(players, dim, fam, tip)
	}
}

// speleoParts is speleoOf without the family.
func speleoParts(st uint32) (thickness int, up, waterlogged, ok bool) {
	_, thickness, up, waterlogged, ok = speleoOf(st)
	return
}

// speleoTipCanGrow is canTipGrow: the cell the tip points into holds no
// fluid and is air, or an unmerged tip of the same block pointing back.
func (h *hub) speleoTipCanGrow(dim int, fam *speleoFamily, tip blockPos, ts uint32) bool {
	_, _, up, _, _ := speleoOf(ts)
	g := tip
	if up {
		g.y++
	} else {
		g.y--
	}
	gs := h.worldFor(dim).At(g.x, g.y, g.z)
	if worldgen.HoldsWater(gs) || worldgen.IsLava(gs) {
		return false
	}
	return isAirState(gs) || h.speleoUnmergedTip(fam, gs, !up)
}

// speleoUnmergedTip is isUnmergedTipWithDirection for the family.
func (h *hub) speleoUnmergedTip(fam *speleoFamily, s uint32, up bool) bool {
	f, th, u, _, ok := speleoOf(s)
	return ok && f == fam && th == dripTip && u == up
}

// speleoGrow is grow(from, direction): the cell beyond takes a new tip, or
// merges with the tip of the same block already pointing back at it.
func (h *hub) speleoGrow(players map[int32]*tracked, dim int, fam *speleoFamily, from blockPos, up bool) {
	t := from
	if up {
		t.y++
	} else {
		t.y--
	}
	w := h.worldFor(dim)
	ts := w.At(t.x, t.y, t.z)
	switch {
	case h.speleoUnmergedTip(fam, ts, !up):
		// createMergedTips: the stalactite and the stalagmite meet.
		lower, upper := t, blockPos{t.x, t.y + 1, t.z}
		if up { // the target is a stalactite tip: it and the stalagmite below
			lower, upper = blockPos{t.x, t.y - 1, t.z}, t
		}
		h.setBlockAt(players, dim, upper, fam.state(dripTipMerge, false, worldgen.HoldsWater(w.At(upper.x, upper.y, upper.z))))
		h.setBlockAt(players, dim, lower, fam.state(dripTipMerge, true, worldgen.HoldsWater(w.At(lower.x, lower.y, lower.z))))
	case isAirState(ts) || worldgen.IsWater(ts):
		h.setBlockAt(players, dim, t, fam.state(dripTip, up, worldgen.IsWater(ts)))
	}
}

// growStalagmiteBelow looks down from the tip, ten cells at most, for the
// stalagmite to lengthen or the floor to start one on: a fluid ends it, and
// for dripstone so does a block a drop cannot pass.
func (h *hub) growStalagmiteBelow(players map[int32]*tracked, dim int, fam *speleoFamily, tip blockPos) {
	w := h.worldFor(dim)
	p := tip
	for i := 0; i < dripStalagFloor; i++ {
		p.y--
		st := w.At(p.x, p.y, p.z)
		if worldgen.HoldsWater(st) || worldgen.IsLava(st) {
			return
		}
		if h.speleoUnmergedTip(fam, st, true) && h.speleoTipCanGrow(dim, fam, p, st) {
			h.speleoGrow(players, dim, fam, p, true)
			return
		}
		if speleothemValid(w, p, fam.min, "up") && !worldgen.HoldsWater(w.At(p.x, p.y-1, p.z)) {
			h.speleoGrow(players, dim, fam, blockPos{p.x, p.y - 1, p.z}, true)
			return
		}
		if fam.dripstone && !canDripThrough(st) {
			return // blocksStalagmiteScan
		}
	}
}

// growStalactite is the growth roll's outcome for a pointed dripstone
// stalactite start (kept for the tests that drive it directly).
func (h *hub) growStalactite(players map[int32]*tracked, dim, x, y, z int) {
	h.growSpeleothem(players, dim, &speleoFamilies[0], blockPos{x, y, z})
}

// canDripThrough is PointedDripstoneBlock.canDripThrough: air, or a block
// that is not solid-render, holds no fluid and whose collision shape
// leaves the drip's column (the middle four pixels) clear.
func canDripThrough(st uint32) bool {
	switch {
	case isAirState(st):
		return true
	case isSolidRender(st), worldgen.HoldsWater(st), worldgen.IsLava(st):
		return false
	}
	return !shapeMeetsBox(st, 0, 0, 0, [3]float64{6.0 / 16, 0, 6.0 / 16}, [3]float64{10.0 / 16, 1, 10.0 / 16})
}

// Level events of a drip.
const (
	levelEventDripstoneDrip = 1504 // DRIPSTONE_DRIP: the drop falling from the tip
	levelEventDripLava      = 1046 // the lava drip landing in a cauldron
	levelEventDripWater     = 1047 // the water drip landing in a cauldron
)

// stalactiteFluid is getFluidAboveStalactite: up the stalactite to its root
// (the first block that is not more of it, within 11), and the fluid on top
// of that root — water for mud where water does not evaporate. fluid is
// cauldronWater, cauldronLava or -1; mud reports whether it came from mud.
func (h *hub) stalactiteFluid(dim int, pos blockPos, state uint32) (above blockPos, fluid int, mud bool) {
	fam, _, up, _, ok := speleoOf(state)
	if !ok || up {
		return blockPos{}, -1, false
	}
	w := h.worldFor(dim)
	p := pos
	for i := 1; i < dripMaxDrip; i++ {
		p.y++
		st := w.At(p.x, p.y, p.z)
		f, _, u, _, ok := speleoOf(st)
		if !ok || f != fam {
			a := blockPos{p.x, p.y + 1, p.z}
			as := w.At(a.x, a.y, a.z)
			switch {
			case as == worldgen.Mud && !dimType(dim).WaterEvaporates:
				return a, cauldronWater, true
			case isWaterSource(as):
				return a, cauldronWater, false
			case as == worldgen.LavaBase:
				return a, cauldronLava, false
			}
			return a, -1, false
		}
		if !h.inWorldYIn(dim, p.y) || u != up {
			return blockPos{}, -1, false
		}
	}
	return blockPos{}, -1, false
}

// dripstoneTransfer is PointedDripstoneBlock.maybeTransferFluid: from a
// stalactite's start, water (at 0.17578125) or lava (at 0.05859375) above
// its root drips from the tip — turning mud to clay, or into the first
// cauldron within 11 below that can take it, which fills when the drop
// lands (a tick scheduled 50 + the fall's length out).
func (h *hub) dripstoneTransfer(players map[int32]*tracked, dim int, pos blockPos, state uint32, r float32) {
	if r > dripWaterChance {
		return
	}
	if !h.stalactiteStart(dim, pos, state) {
		return
	}
	above, fluid, mud := h.stalactiteFluid(dim, pos, state)
	prob := float32(dripWaterChance)
	switch fluid {
	case cauldronWater:
	case cauldronLava:
		prob = dripLavaChance
	default:
		return
	}
	if r >= prob {
		return
	}
	tip, ok := h.speleoTip(dim, pos, state, dripMaxDrip, false)
	if !ok {
		return
	}
	if mud {
		h.setBlockAt(players, dim, above, worldgen.Clay)
		h.vib(dim, freqBlockChange, above.x, above.y, above.z, 0)
		h.levelEvent(players, dim, levelEventDripstoneDrip, tip.x, tip.y, tip.z, 0)
		return
	}
	w := h.worldFor(dim)
	p := tip
	for i := 1; i < dripMaxDrip; i++ {
		p.y--
		st := w.At(p.x, p.y, p.z)
		if cauldronTakesDrip(st, fluid) {
			h.levelEvent(players, dim, levelEventDripstoneDrip, tip.x, tip.y, tip.z, 0)
			h.inDim(dim, func() { h.scheduleTick(p, uint64(50+tip.y-p.y), tickNormal) })
			return
		}
		if !h.inWorldYIn(dim, p.y) || !canDripThrough(st) {
			return
		}
	}
}

// cauldronTakesDrip is canReceiveStalactiteDrip: an empty cauldron takes
// either, a water cauldron water (full or not: a full one just stays full).
func cauldronTakesDrip(st uint32, fluid int) bool {
	kind, _, ok := cauldronOf(st)
	return ok && (kind == cauldronEmpty || (kind == cauldronWater && fluid == cauldronWater))
}

// cauldronDripTick is AbstractCauldronBlock.tick: the drop lands. The
// stalactite above (within 11, through cells a drop passes) decides what it
// was, and the cauldron takes it (receiveStalactiteDrip).
func (h *hub) cauldronDripTick(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	w := h.worldFor(dim)
	p := pos
	var tip blockPos
	found := false
	for i := 1; i < dripMaxDrip; i++ {
		p.y++
		st := w.At(p.x, p.y, p.z)
		if th, up, wl, ok := speleoParts(st); ok && !up && th == dripTip && !wl {
			tip, found = p, true
			break
		}
		if !h.inWorldYIn(dim, p.y) || !canDripThrough(st) {
			return
		}
	}
	if !found {
		return
	}
	_, fluid, _ := h.stalactiteFluid(dim, tip, w.At(tip.x, tip.y, tip.z))
	if fluid != cauldronWater && fluid != cauldronLava || !cauldronTakesDrip(state, fluid) {
		return
	}
	kind, level, _ := cauldronOf(state)
	switch {
	case kind == cauldronEmpty && fluid == cauldronWater:
		h.setBlockAt(players, dim, pos, waterCauldronBase)
		h.levelEvent(players, dim, levelEventDripWater, pos.x, pos.y, pos.z, 0)
	case kind == cauldronEmpty:
		h.setBlockAt(players, dim, pos, lavaCauldronState)
		h.levelEvent(players, dim, levelEventDripLava, pos.x, pos.y, pos.z, 0)
	case level < 3:
		h.setBlockAt(players, dim, pos, waterCauldronBase+uint32(level))
		h.levelEvent(players, dim, levelEventDripWater, pos.x, pos.y, pos.z, 0)
	default:
		return // full: nothing changes
	}
	h.vib(dim, freqBlockChange, pos.x, pos.y, pos.z, 0)
}

// stalagmiteFallExtra is what landing on a stalagmite tip adds to a fall: the
// distance counts 2.5 blocks longer and the damage doubles.
func stalagmiteFallExtra(landedOn uint32, dist float64) (float64, bool) {
	if !isStalagmiteTip(landedOn) {
		return 0, false
	}
	return math.Max(0, (dist+dripFallBonus-3)*dripFallScale), true
}

// SpeleothemBlock (pointed dripstone, sulfur spikes): placement and the
// thickness a column keeps as it grows or loses a piece.

func isSpeleothem(s uint32) bool { return worldgen.SupportFor(s) == worldgen.SupportSpeleothem }

// speleothemWith is isSpeleothemWithDirection for this block family.
func speleothemWith(s, family uint32, dir string) bool {
	if !isSpeleothem(s) || !sameBlockFamily(s, family) {
		return false
	}
	info, _ := worldgen.InfoForState(s)
	return worldgen.GetProperty(info, s, "vertical_direction") == dir
}

func vertDelta(dir string) int {
	if dir == "up" {
		return 1
	}
	return -1
}

func oppositeVert(dir string) string {
	if dir == "up" {
		return "down"
	}
	return "up"
}

// speleothemValid is isValidSpeleothemPlacement: behind the tip, a sturdy
// face or more of the same, pointing the same way.
func speleothemValid(w *world.World, pos blockPos, family uint32, tip string) bool {
	b := w.At(pos.x, pos.y-vertDelta(tip), pos.z)
	return holdsBlock(b) && !isSpeleothem(b) || speleothemWith(b, family, tip)
}

// speleothemThickness is calculateSpeleothemThickness.
func speleothemThickness(w *world.World, pos blockPos, family uint32, tip string, merge bool) string {
	front := w.At(pos.x, pos.y+vertDelta(tip), pos.z)
	if speleothemWith(front, family, oppositeVert(tip)) {
		fi, _ := worldgen.InfoForState(front)
		if !merge && worldgen.GetProperty(fi, front, "thickness") != "tip_merge" {
			return "tip"
		}
		return "tip_merge"
	}
	if !speleothemWith(front, family, tip) {
		return "tip"
	}
	fi, _ := worldgen.InfoForState(front)
	if t := worldgen.GetProperty(fi, front, "thickness"); t == "tip" || t == "tip_merge" {
		return "frustum"
	}
	if !speleothemWith(w.At(pos.x, pos.y-vertDelta(tip), pos.z), family, tip) {
		return "base"
	}
	return "middle"
}

// speleothemPlaced is SpeleothemBlock.getStateForPlacement: the tip points
// away from the way the player looks (up when looking down), or the other
// way if that is the only one that holds; sneaking keeps two tips apart.
func speleothemPlaced(w *world.World, pos blockPos, def uint32, pitch float32, sneaking, water bool) (uint32, bool) {
	tip := "up" // looking down: nearest vertical is DOWN, the tip is its opposite
	if pitch < 0 {
		tip = "down"
	}
	if !speleothemValid(w, pos, def, tip) {
		tip = oppositeVert(tip)
		if !speleothemValid(w, pos, def, tip) {
			return def, false
		}
	}
	info, _ := worldgen.InfoForState(def)
	st := worldgen.SetProperty(info, def, "vertical_direction", tip)
	st = worldgen.SetProperty(info, st, "thickness", speleothemThickness(w, pos, def, tip, !sneaking))
	return worldgen.SetProperty(info, st, "waterlogged", strconv.FormatBool(water)), true
}
