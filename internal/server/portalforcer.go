package server

import (
	"log"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Nether portal linking, the way vanilla's PortalForcer does it: no pair is
// ever remembered. Every trip — a player's, a mob's, a dropped item's, a
// falling block's — scales the traveller's position into the other dimension
// (8:1) and searches there for the closest portal block, within 16 blocks
// going into the Nether and 128 coming out (findClosestPortalPosition). When
// there is none, a fresh 2×3 portal is built on the best spot within 16
// blocks (createPortal), or on a small obsidian platform when nowhere fits.
// The traveller comes out at the same place in the far portal as it went into
// the near one, turned a quarter if the two portals face different ways
// (NetherPortalBlock.getExitPortal and createDimensionTransition).

const (
	portalSearchNether    = 16  // PortalForcer.NETHER_PORTAL_RADIUS
	portalSearchOverworld = 128 // PortalForcer.OVERWORLD_PORTAL_RADIUS
	portalCreateRadius    = 16  // createPortal's spiral
	portalRectLimit       = 21  // PortalShape.MAX_WIDTH / MAX_HEIGHT
)

// portalRect is BlockUtil.FoundRectangle for a portal sheet: its bottom
// corner at the low end of the axis, its width along the axis and height.
type portalRect struct {
	min  blockPos
	w, h int
}

// portalArrival is a resolved trip: where the traveller lands and by how
// much its heading turns (0 or 90 degrees).
type portalArrival struct {
	dim     int
	x, y, z float64
	turn    float32
}

// portalAxisZ reports a portal block's axis (the sheet runs along z).
func portalAxisZ(s uint32) bool { return s == portalZ }

// portalMaxPlaceableY is createPortal's maxPlaceableY: the top of the
// dimension's logical height, which in the Nether is under the bedrock roof.
func (h *hub) portalMaxPlaceableY(dim int) int {
	if dim == dimNether {
		return worldgen.NetherRoof
	}
	return h.worldFor(dim).Ceiling() - 1
}

// netherPortalExit is NetherPortalBlock.getPortalDestination for anything
// standing in a nether portal at entry: the far portal found or built, and
// the spot in it matching where the traveller stands in this one. width and
// height are the traveller's box; a spectator never builds a portal.
func (h *hub) netherPortalExit(players map[int32]*tracked, fromDim int, entry blockPos, x, y, z, width, height float64, spectator bool) (portalArrival, bool) {
	if fromDim != dimOverworld && fromDim != dimNether {
		return portalArrival{}, false
	}
	toDim := dimNether
	scale := 1.0 / 8
	if fromDim == dimNether {
		toDim, scale = dimOverworld, 8
	}
	toNether := toDim == dimNether
	if h.worldFor(toDim) == nil || (toNether && h.nether == nil) {
		return portalArrival{}, false
	}
	// WorldBorder.clampToBounds, then BlockPos.containing.
	ax, az := h.clampToBorder(toDim, x*scale, z*scale)
	approx := blockPos{floorInt(ax), floorInt(y), floorInt(az)}

	fw := h.worldFor(fromDim)
	entryState := fw.At(entry.x, entry.y, entry.z)
	if !isPortalBlock(entryState) {
		entryState = portalX // an entry without an axis: X (getOptionalValue.orElse)
	}
	var exit portalRect
	if pos, ok := h.findClosestPortal(toDim, approx, toNether); ok {
		tw := h.worldFor(toDim)
		st := tw.At(pos.x, pos.y, pos.z)
		exit = largestRectAround(pos, portalAxisZ(st), func(p blockPos) bool { return tw.At(p.x, p.y, p.z) == st })
	} else {
		if spectator {
			return portalArrival{}, false
		}
		var built bool
		exit, built = h.createNetherPortal(players, toDim, approx, portalAxisZ(entryState))
		if !built {
			log.Printf("portal: unable to create a portal near (%d,%d,%d) dim %d", approx.x, approx.y, approx.z, toDim)
			return portalArrival{}, false
		}
	}

	// getDimensionTransitionFromExit: where in the near portal the traveller
	// stands, relative to its sheet.
	entryAxisZ := portalAxisZ(entryState)
	var rx, ry, rz float64
	if isPortalBlock(fw.At(entry.x, entry.y, entry.z)) {
		in := largestRectAround(entry, entryAxisZ, func(p blockPos) bool { return fw.At(p.x, p.y, p.z) == entryState })
		rx, ry, rz = portalRelativePosition(in, entryAxisZ, x, y, z, width, height)
	} else {
		rx, ry, rz = 0.5, 0, 0
	}
	return h.portalTransition(toDim, exit, entryAxisZ, rx, ry, rz, width, height), true
}

// portalTransition is createDimensionTransition: the relative position laid
// onto the exit sheet, and a quarter turn when the two sheets differ.
func (h *hub) portalTransition(toDim int, exit portalRect, entryAxisZ bool, rx, ry, rz, width, height float64) portalArrival {
	tw := h.worldFor(toDim)
	exitAxisZ := portalAxisZ(tw.At(exit.min.x, exit.min.y, exit.min.z))
	var turn float32
	if entryAxisZ != exitAxisZ {
		turn = 90
	}
	right := width/2 + (float64(exit.w)-width)*rx
	up := (float64(exit.h) - height) * ry
	forward := 0.5 + rz
	a := portalArrival{dim: toDim, y: float64(exit.min.y) + up, turn: turn}
	if !exitAxisZ {
		a.x, a.z = float64(exit.min.x)+right, float64(exit.min.z)+forward
	} else {
		a.x, a.z = float64(exit.min.x)+forward, float64(exit.min.z)+right
	}
	return a
}

// portalRelativePosition is PortalShape.getRelativePosition: across the
// sheet (0..1), up it (0..1), and the offset through it from its middle.
func portalRelativePosition(r portalRect, axisZ bool, x, y, z, width, height float64) (float64, float64, float64) {
	along, through := x, z
	minAlong, minThrough := float64(r.min.x), float64(r.min.z)
	if axisZ {
		along, through = z, x
		minAlong, minThrough = float64(r.min.z), float64(r.min.x)
	}
	right := 0.5
	if span := float64(r.w) - width; span > 0 {
		right = clampF((along-(minAlong+width/2))/span, 0, 1)
	}
	up := 0.0
	if span := float64(r.h) - height; span > 0 {
		up = clampF((y-float64(r.min.y))/span, 0, 1)
	}
	return right, up, through - (minThrough + 0.5)
}

// rotateDelta is the ROTATE_DELTA half of a relative teleport: the motion
// turns with the heading (Vec3.yRot by the old minus the new yaw).
func rotateDelta(vx, vz float64, turn float32) (float64, float64) {
	if turn == 0 {
		return vx, vz
	}
	a := -float64(turn) * math.Pi / 180
	c, s := math.Cos(a), math.Sin(a)
	return vx*c + vz*s, vz*c - vx*s
}

// findClosestPortal is PortalForcer.findClosestPortalPosition: every nether
// portal block within the square (radius 16 into the Nether, 128 out of it)
// and inside the border, the closest by block distance, then the lowest.
// Portal blocks exist only where a player or the engine lit them, so they are
// all edits: the search reads those and never generates a chunk.
func (h *hub) findClosestPortal(dim int, approx blockPos, toNether bool) (blockPos, bool) {
	w := h.worldFor(dim)
	r := portalSearchOverworld
	if toNether {
		r = portalSearchNether
	}
	cr := r/16 + 1 // PoiManager.getInSquare: floorDiv(radius, 16) + 1 chunks round
	ccx, ccz := approx.x>>4, approx.z>>4
	var best blockPos
	bestD, found := 0, false
	for cx := ccx - cr; cx <= ccx+cr; cx++ {
		for cz := ccz - cr; cz <= ccz+cr; cz++ {
			w.ForEachEditIn(int32(cx), int32(cz), func(x, y, z int, s uint32) {
				if !isPortalBlock(s) {
					return
				}
				dx, dy, dz := x-approx.x, y-approx.y, z-approx.z
				if dx < -r || dx > r || dz < -r || dz > r || !h.withinBorder(dim, float64(x), float64(z)) {
					return
				}
				d := dx*dx + dy*dy + dz*dz
				if !found || d < bestD || (d == bestD && y < best.y) {
					best, bestD, found = blockPos{x, y, z}, d, true
				}
			})
		}
	}
	return best, found
}

// largestRectAround is BlockUtil.getLargestRectangleAround for a portal
// sheet: axis 1 is the sheet's horizontal axis, axis 2 is up, each limited
// to 21, and test says which cells belong to the sheet.
func largestRectAround(center blockPos, axisZ bool, test func(blockPos) bool) portalRect {
	step := func(p blockPos, d1, d2 int) blockPos { // d1 along the axis, d2 up
		if axisZ {
			return blockPos{p.x, p.y + d2, p.z + d1}
		}
		return blockPos{p.x + d1, p.y + d2, p.z}
	}
	limit := func(from blockPos, d1, d2, lim int) int {
		n := 0
		for p := from; n < lim; n++ {
			p = step(p, d1, d2)
			if !test(p) {
				break
			}
		}
		return n
	}
	neg1 := limit(center, -1, 0, portalRectLimit)
	pos1 := limit(center, 1, 0, portalRectLimit)
	type bounds struct{ min, max int }
	by1 := make([]bounds, neg1+1+pos1)
	by1[neg1] = bounds{limit(center, 0, -1, portalRectLimit), limit(center, 0, 1, portalRectLimit)}
	c2 := by1[neg1].min
	for i := 1; i <= neg1; i++ {
		last := by1[neg1-(i-1)]
		at := step(center, -i, 0)
		by1[neg1-i] = bounds{limit(at, 0, -1, last.min), limit(at, 0, 1, last.max)}
	}
	for i := 1; i <= pos1; i++ {
		last := by1[neg1+i-1]
		at := step(center, i, 0)
		by1[neg1+i] = bounds{limit(at, 0, -1, last.min), limit(at, 0, 1, last.max)}
	}
	min1, min2, size1, size2 := 0, 0, 0, 0
	cols := make([]int, len(by1))
	for i2 := c2; i2 >= 0; i2-- {
		for i1, b := range by1 {
			lo, hi := c2-b.min, c2+b.max
			if i2 >= lo && i2 <= hi {
				cols[i1] = hi + 1 - i2
			} else {
				cols[i1] = 0
			}
		}
		start, end, height := maxRectangleLocation(cols)
		if n1 := 1 + end - start; n1*height > size1*size2 {
			min1, min2, size1, size2 = start, i2, n1, height
		}
	}
	return portalRect{min: step(center, min1-neg1, min2-c2), w: size1, h: size2}
}

// maxRectangleLocation is BlockUtil.getMaxRectangleLocation: the largest
// rectangle under a histogram, as its first and last column and height.
func maxRectangleLocation(cols []int) (start, end, height int) {
	maxStart, maxEnd, maxHeight := 0, 0, 0
	stack := []int{0}
	for col := 1; col <= len(cols); col++ {
		h := 0
		if col < len(cols) {
			h = cols[col]
		}
		for len(stack) > 0 {
			sh := cols[stack[len(stack)-1]]
			if h >= sh {
				stack = append(stack, col)
				break
			}
			stack = stack[:len(stack)-1]
			s := 0
			if len(stack) > 0 {
				s = stack[len(stack)-1] + 1
			}
			if sh*(col-s) > maxHeight*(maxEnd-maxStart) {
				maxEnd, maxStart, maxHeight = col, s, sh
			}
		}
		if len(stack) == 0 {
			stack = append(stack, col)
		}
	}
	return maxStart, maxEnd - 1, maxHeight
}

// portalCanReplace is PortalForcer.canPortalReplaceBlock: a replaceable
// block with no fluid in it.
func portalCanReplace(s uint32) bool {
	if worldgen.HoldsWater(s) || worldgen.IsLava(s) {
		return false
	}
	return s == worldgen.Air || inRanges2(s, replaceableRanges)
}

// spiralAround is BlockPos.spiralAround(origin, radius, EAST, SOUTH): the
// origin, then outwards leg by leg — east, south, west, north — each pair of
// legs a block longer.
func spiralAround(ox, oz, radius int, fn func(x, z int)) {
	dirs := [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} // EAST, SOUTH, WEST, NORTH
	legs := 4 * radius
	leg, legSize, legIndex := -1, 0, 0
	lx, lz := ox, oz+1 // cursor starts one step along the second direction
	for {
		d := dirs[(leg+4)%4]
		lx, lz = lx+d[0], lz+d[1]
		if legIndex >= legSize {
			if leg >= legs {
				return
			}
			leg++
			legIndex = 0
			legSize = leg/2 + 1
		}
		legIndex++
		fn(lx, lz)
	}
}

// createNetherPortal is PortalForcer.createPortal: the closest spot within 16
// blocks with room for a whole frame and solid ground under it — ideally with
// room on both sides too — else a fallback on a 3×2 obsidian platform with
// air above it, clamped into the border; then the obsidian frame and the lit
// 2×3 sheet along the entry portal's axis.
func (h *hub) createNetherPortal(players map[int32]*tracked, dim int, origin blockPos, axisZ bool) (portalRect, bool) {
	w := h.worldFor(dim)
	dx, dz := 1, 0   // direction: the axis' positive way (EAST for X)
	cwx, cwz := 0, 1 // its clockwise turn (SOUTH)
	state := portalX
	if axisZ {
		dx, dz, cwx, cwz, state = 0, 1, -1, 0, portalZ // SOUTH, then WEST
	}
	minY := worldgen.MinY
	maxY := h.portalMaxPlaceableY(dim)
	canHost := func(o blockPos, offset int) bool {
		for wd := -1; wd < 3; wd++ {
			for ht := -1; ht < 4; ht++ {
				s := w.At(o.x+dx*wd+cwx*offset, o.y+ht, o.z+dz*wd+cwz*offset)
				if ht < 0 && !worldgen.IsSolid(s) {
					return false
				}
				if ht >= 0 && !portalCanReplace(s) {
					return false
				}
			}
		}
		return true
	}
	fullD, partD := -1, -1
	var full, part blockPos
	spiralAround(origin.x, origin.z, portalCreateRadius, func(cx, cz int) {
		height := min(maxY, h.motionBlockingTop(dim, cx, cz))
		if !h.withinBorder(dim, float64(cx), float64(cz)) || !h.withinBorder(dim, float64(cx+dx), float64(cz+dz)) {
			return
		}
		for y := height; y >= minY; y-- {
			if !portalCanReplace(w.At(cx, y, cz)) {
				continue
			}
			firstEmpty := y
			for y > minY && portalCanReplace(w.At(cx, y-1, cz)) {
				y--
			}
			if y+4 > maxY {
				continue
			}
			if dy := firstEmpty - y; dy > 0 && dy < 3 {
				continue
			}
			p := blockPos{cx, y, cz}
			if !canHost(p, 0) {
				continue
			}
			d := sqI(p.x-origin.x) + sqI(p.y-origin.y) + sqI(p.z-origin.z)
			if canHost(p, -1) && canHost(p, 1) && (fullD == -1 || fullD > d) {
				fullD, full = d, p
			}
			if fullD == -1 && (partD == -1 || partD > d) {
				partD, part = d, p
			}
		}
	})
	if fullD == -1 && partD != -1 {
		full, fullD = part, partD
	}
	if fullD == -1 {
		// Nowhere fits: a platform in mid-air (or carved into the rock),
		// kept between y 70 and nine under the top.
		minStart := max(minY+1, 70)
		maxStart := maxY - 9
		if maxStart < minStart {
			return portalRect{}, false
		}
		fx, fz := h.clampToBorder(dim, float64(origin.x-dx), float64(origin.z-dz))
		full = blockPos{floorInt(fx), min(max(origin.y, minStart), maxStart), floorInt(fz)}
		for box := -1; box < 2; box++ {
			for wd := 0; wd < 2; wd++ {
				for ht := -1; ht < 3; ht++ {
					st := worldgen.Air
					if ht < 0 {
						st = worldgen.Obsidian
					}
					h.setBlockAt(players, dim, blockPos{full.x + wd*dx + box*cwx, full.y + ht, full.z + wd*dz + box*cwz}, st)
				}
			}
		}
	}
	for wd := -1; wd < 3; wd++ {
		for ht := -1; ht < 4; ht++ {
			if wd == -1 || wd == 2 || ht == -1 || ht == 3 {
				h.setBlockAt(players, dim, blockPos{full.x + wd*dx, full.y + ht, full.z + wd*dz}, worldgen.Obsidian)
			}
		}
	}
	// The sheet goes in without neighbour updates (flags 18), so no cell is
	// judged before the next one is there.
	for wd := 0; wd < 2; wd++ {
		for ht := 0; ht < 3; ht++ {
			x, y, z := full.x+wd*dx, full.y+ht, full.z+wd*dz
			w.SetBlock(x, y, z, state)
			h.broadcastBlockIn(players, dim, x, y, z, state)
		}
	}
	log.Printf("portal: built a portal at (%d,%d,%d) dim %d", full.x, full.y, full.z, dim)
	return portalRect{min: full, w: 2, h: 3}, true
}
