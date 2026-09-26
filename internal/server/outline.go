package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Block outline shapes. A ClipContext.Block.OUTLINE ray stops at the shape
// the crosshair draws, not the one an entity bumps into: a flower, a tuft of
// grass or a torch has no collision at all but still stands in the way.
// outline_gen.go carries every state's outline boxes from the running game
// (scripts/extract, fields outlineBoxes and offset, via gen_outline.py), and
// a plant's per-position nudge is put back here as vanilla's offset function
// computes it.

// outlineBox is one box of a shape, in block-local coordinates.
type outlineBox [6]float64

// outlineRun is one run of outline_gen.go: states lo..hi share shape.
type outlineRun struct {
	lo, hi uint32
	shape  int
}

// outlineOffset is a block whose shape is nudged per position.
type outlineOffset struct {
	lo, hi     uint32
	xyz        bool
	maxH, maxV float64
}

// fullCubeOutline is the default: every state not listed in outlineRuns.
var fullCubeOutline = []outlineBox{{0, 0, 0, 1, 1, 1}}

// outlineOf is a state's outline boxes, un-nudged; empty for no outline.
func outlineOf(s uint32) []outlineBox {
	i, j := 0, len(outlineRuns)
	for i < j {
		m := (i + j) / 2
		if outlineRuns[m].hi < s {
			i = m + 1
		} else {
			j = m
		}
	}
	if i < len(outlineRuns) && outlineRuns[i].lo <= s {
		return outlineShapes[outlineRuns[i].shape]
	}
	return fullCubeOutline
}

// outlineNudge is BlockState.getOffset(pos): the random XZ (or XYZ) shift a
// flower, grass or bamboo takes from its column, zero for anything else.
func outlineNudge(s uint32, x, z int) (dx, dy, dz float64) {
	for _, o := range outlineOffsets {
		if s < o.lo || s > o.hi {
			continue
		}
		seed := mthGetSeed(int32(x), 0, int32(z))
		dx = clampF((float64(float32(seed&15)/15)-0.5)*0.5, -o.maxH, o.maxH)
		dz = clampF((float64(float32(seed>>8&15)/15)-0.5)*0.5, -o.maxH, o.maxH)
		if o.xyz {
			dy = (float64(float32(seed>>4&15)/15) - 1) * o.maxV
		}
		return dx, dy, dz
	}
	return 0, 0, 0
}

// clipOutline is Level.clip with ClipContext.Block.OUTLINE and Fluid.NONE:
// the first cell along the segment whose outline the segment passes
// through, cells visited in order from the one the ray starts in. hit is
// false when nothing stops it, and vanilla's miss then names the cell the
// segment ends in. A segment of no length is a miss.
func (h *hub) clipOutline(dim int, x0, y0, z0, x1, y1, z1 float64) (pos blockPos, hit bool) {
	end := blockPos{floorInt(x1), floorInt(y1), floorInt(z1)}
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	w := h.worldFor(dim)
	if w == nil || dx*dx+dy*dy+dz*dz < 1e-18 {
		return end, false
	}
	cx, cy, cz := floorInt(x0), floorInt(y0), floorInt(z0)
	sx, tMaxX, tDeltaX := ddaAxis(x0, dx)
	sy, tMaxY, tDeltaY := ddaAxis(y0, dy)
	sz, tMaxZ, tDeltaZ := ddaAxis(z0, dz)
	for steps := 0; steps < 3*int(sightMaxDist)+3; steps++ {
		if s := w.At(cx, cy, cz); s != worldgen.Air {
			nx, ny, nz := outlineNudge(s, cx, cz)
			ox, oy, oz := float64(cx)+nx, float64(cy)+ny, float64(cz)+nz
			for _, b := range outlineOf(s) {
				if segmentHitsBox(x0-ox, y0-oy, z0-oz, dx, dy, dz, b) {
					return blockPos{cx, cy, cz}, true
				}
			}
		}
		if cx == end.x && cy == end.y && cz == end.z {
			break
		}
		switch {
		case tMaxX < tMaxY && tMaxX < tMaxZ:
			cx += sx
			tMaxX += tDeltaX
		case tMaxY < tMaxZ:
			cy += sy
			tMaxY += tDeltaY
		default:
			cz += sz
			tMaxZ += tDeltaZ
		}
	}
	return end, false
}

// segmentHitsBox is AABB.clip for the segment o + t·d, t in [0, 1]: the slab
// test, touching counting as a hit as it does for a VoxelShape.
func segmentHitsBox(ox, oy, oz, dx, dy, dz float64, b outlineBox) bool {
	lo, hi := 0.0, 1.0
	for i, od := range [3][2]float64{{ox, dx}, {oy, dy}, {oz, dz}} {
		o, d := od[0], od[1]
		mn, mx := b[i], b[i+3]
		if math.Abs(d) < 1e-12 {
			if o < mn || o > mx {
				return false
			}
			continue
		}
		t0, t1 := (mn-o)/d, (mx-o)/d
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		lo, hi = math.Max(lo, t0), math.Min(hi, t1)
		if lo > hi {
			return false
		}
	}
	return true
}
