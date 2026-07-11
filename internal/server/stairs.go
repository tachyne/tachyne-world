package server

// stairs.go — vanilla StairBlock corner shapes. A stair's SHAPE property
// (straight / inner_left / inner_right / outer_left / outer_right) derives
// from the stairs it meets: a perpendicular same-half stair on the high side
// makes an outer corner, one on the low side an inner corner — unless a
// parallel same-facing stair alongside vetoes it (canTakeShape), which is
// what keeps straight runs straight. Recomputed on placement and whenever a
// horizontal neighbour changes, like vanilla updateShape.

import (
	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// stairInfo classifies a state as a stair: the only family with facing +
// half + shape (rails have a shape property but no facing/half).
func stairInfo(state uint32) (worldgen.BlockInfo, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("shape") || !info.HasProperty("facing") || !info.HasProperty("half") {
		return worldgen.BlockInfo{}, false
	}
	return info, true
}

// stairShape recomputes the SHAPE property from the two stairs a corner can
// form with — vanilla StairBlock.getStairsShape.
func stairShape(w *world.World, x, y, z int, info worldgen.BlockInfo, state uint32) uint32 {
	facing := worldgen.GetProperty(info, state, "facing")
	half := worldgen.GetProperty(info, state, "half")
	shape := "straight"

	// the block on the stair's high side (vanilla pos.relative(facing))
	dx, dz := facingDelta(facing)
	if bi, ok := stairInfo(w.Block(x+dx, y, z+dz)); ok {
		b := w.Block(x+dx, y, z+dz)
		bf := worldgen.GetProperty(bi, b, "facing")
		if worldgen.GetProperty(bi, b, "half") == half &&
			facingAxisX(bf) != facingAxisX(facing) &&
			canTakeShape(w, x, y, z, facing, half, oppositeFacing(bf)) {
			if bf == leftOf(facing) {
				shape = "outer_left"
			} else {
				shape = "outer_right"
			}
		}
	}
	if shape == "straight" { // the block on the low side
		dx, dz = facingDelta(oppositeFacing(facing))
		if fi, ok := stairInfo(w.Block(x+dx, y, z+dz)); ok {
			f := w.Block(x+dx, y, z+dz)
			ff := worldgen.GetProperty(fi, f, "facing")
			if worldgen.GetProperty(fi, f, "half") == half &&
				facingAxisX(ff) != facingAxisX(facing) &&
				canTakeShape(w, x, y, z, facing, half, ff) {
				if ff == leftOf(facing) {
					shape = "inner_left"
				} else {
					shape = "inner_right"
				}
			}
		}
	}
	return worldgen.SetProperty(info, state, "shape", shape)
}

// canTakeShape mirrors vanilla: the corner is vetoed when the block on the
// given side is a stair with the SAME facing and half (a parallel run).
func canTakeShape(w *world.World, x, y, z int, facing, half, side string) bool {
	dx, dz := facingDelta(side)
	ni, ok := stairInfo(w.Block(x+dx, y, z+dz))
	if !ok {
		return true
	}
	n := w.Block(x+dx, y, z+dz)
	return worldgen.GetProperty(ni, n, "facing") != facing || worldgen.GetProperty(ni, n, "half") != half
}
