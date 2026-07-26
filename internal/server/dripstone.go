package server

import (
	"math"

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

// tickDripstone is the random tick: grow, and maybe let a drop through.
func (h *hub) tickDripstone(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isStalactite(state) {
		return false
	}
	// Only the topmost segment of a stalactite acts — the rest of the column
	// is along for the ride.
	if isStalactite(h.worldFor(dim).At(x, y+1, z)) {
		return true
	}
	h.dripThroughStalactite(players, dim, x, y, z)
	if h.rng.Float64() < dripGrowChance {
		h.growStalactite(players, dim, x, y, z)
	}
	return true
}

// growStalactite lengthens the stalactite, or starts a stalagmite under it.
func (h *hub) growStalactite(players map[int32]*tracked, dim, x, y, z int) {
	w := h.worldFor(dim)
	if w.At(x, y+1, z) != dripstoneBlock {
		return // vanilla canGrow: only from the stone it hangs off
	}
	tipY, ok := dripstoneTip(w, x, y, z, dripMaxGrow)
	if !ok {
		return
	}
	below := w.At(x, tipY-1, z)
	if below != worldgen.Air {
		return // vanilla canTipGrow: the space below must be free and dry
	}
	if h.rng.Intn(2) == 0 {
		// Grow down: the old tip thickens to frustum, a new tip below it.
		h.setBlockAt(players, dim, blockPos{x, tipY, z}, dripstoneState(2, false, false))
		h.setBlockAt(players, dim, blockPos{x, tipY - 1, z}, dripstoneState(dripTip, false, false))
		return
	}
	// Or drip a stalagmite into being on the floor below.
	for i := 1; i <= dripStalagFloor; i++ {
		fy := tipY - i
		st := w.At(x, fy, z)
		if st == worldgen.Air {
			continue
		}
		if isStalagmiteTip(st) { // an existing stalagmite grows one taller
			h.setBlockAt(players, dim, blockPos{x, fy, z}, dripstoneState(2, true, false))
			h.setBlockAt(players, dim, blockPos{x, fy + 1, z}, dripstoneState(dripTip, true, false))
			return
		}
		if worldgen.IsSolidFull(st) {
			h.setBlockAt(players, dim, blockPos{x, fy + 1, z}, dripstoneState(dripTip, true, false))
		}
		return
	}
}

// dripThroughStalactite is the drop of water or lava that falls from a tip
// into a cauldron below it.
func (h *hub) dripThroughStalactite(players map[int32]*tracked, dim, x, y, z int) {
	w := h.worldFor(dim)
	above := w.At(x, y+2, z) // the block on top of the dripstone block
	var chance float64
	var fill int
	switch {
	case worldgen.IsWater(above):
		chance, fill = dripWaterChance, cauldronWater
	case worldgen.IsLava(above):
		chance, fill = dripLavaChance, cauldronLava
	default:
		return
	}
	if w.At(x, y+1, z) != dripstoneBlock || h.rng.Float64() >= chance {
		return
	}
	tipY, ok := dripstoneTip(w, x, y, z, dripMaxDrip)
	if !ok {
		return
	}
	// Look down from the tip for a cauldron the drop can fill.
	for cy := tipY - 1; cy > tipY-dripMaxDrip*2 && h.inWorldY(cy); cy-- {
		st := w.At(x, cy, z)
		if st == worldgen.Air {
			continue
		}
		kind, level, isCauldron := cauldronOf(st)
		if !isCauldron {
			return // something solid in the way
		}
		switch {
		case fill == cauldronWater && kind == cauldronEmpty:
			h.setBlockAt(players, dim, blockPos{x, cy, z}, waterCauldronBase)
		case fill == cauldronWater && kind == cauldronWater && level < 3:
			h.setBlockAt(players, dim, blockPos{x, cy, z}, waterCauldronBase+uint32(level))
		case fill == cauldronLava && kind == cauldronEmpty:
			// Lava fills a cauldron in one go — there is no partial lava level.
			h.setBlockAt(players, dim, blockPos{x, cy, z}, lavaCauldronState)
		}
		return
	}
}

// stalagmiteFallExtra is what landing on a stalagmite tip adds to a fall: the
// distance counts 2.5 blocks longer and the damage doubles.
func stalagmiteFallExtra(landedOn uint32, dist float64) (float64, bool) {
	if !isStalagmiteTip(landedOn) {
		return 0, false
	}
	return math.Max(0, (dist+dripFallBonus-3)*dripFallScale), true
}
