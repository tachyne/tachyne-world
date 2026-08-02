package worldgen

import "math"

// The foliage placers, and the one trunk placer complicated enough to want its
// own room — fancy, which is the large oak.

// axisLog returns a log state turned onto an axis: 0 = X, 1 = Y, 2 = Z. Log
// states run x,y,z from the base, which is why a branch laid sideways reads
// correctly only if the axis is set as it is placed.
func axisLog(log uint32, axis int) uint32 { return log + uint32(axis) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// foliageHeightOf is FoliagePlacer.foliageHeight per placer.
func (c *TreeConfig) foliageHeightOf(rng TreeRNG, treeHeight int) int {
	switch c.Foliage {
	case FoliageBlob, FoliageBush, FoliageMegaJungle:
		return c.FoliageH
	case FoliageFancy:
		return c.FoliageH
	case FoliageDarkOak:
		return 4
	case FoliageAcacia:
		return 0
	case FoliageSpruce:
		return max(4, treeHeight-sampleInt(rng, c.TrunkHeightMin, c.TrunkHeightMax))
	case FoliagePine, FoliageMegaPine, FoliageCherry, FoliageRandomSpread:
		return sampleInt(rng, c.FoliageHMin, c.FoliageHMax)
	}
	return c.FoliageH
}

// foliageRadiusOf is FoliagePlacer.foliageRadius. Only pine overrides it, and
// its extra term is why a tall pine wears a wider crown than a short one.
func (c *TreeConfig) foliageRadiusOf(rng TreeRNG, trunkHeight int) int {
	r := sampleInt(rng, c.RadiusMin, c.RadiusMax)
	if c.Foliage == FoliagePine {
		r += rng.Intn(max(trunkHeight+1, 1))
	}
	return r
}

// createFoliage grows one blob from one attachment.
func (c *TreeConfig) createFoliage(rng TreeRNG, a foliageAttachment, treeHeight, foliageHeight, leafRadius, offset int, set TreeSetter) {
	row := func(ox, oy, oz, radius, y int) {
		c.placeLeavesRow(rng, ox, oy, oz, radius, y, a.doubleTrunk, set)
	}
	switch c.Foliage {
	case FoliageBlob:
		for yo := offset; yo >= offset-foliageHeight; yo-- {
			r := max(leafRadius+a.radiusOffset-1-yo/2, 0)
			row(a.x, a.y, a.z, r, yo)
		}

	case FoliageFancy:
		for yo := offset; yo >= offset-foliageHeight; yo-- {
			r := leafRadius
			if yo != offset && yo != offset-foliageHeight {
				r++
			}
			row(a.x, a.y, a.z, r, yo)
		}

	case FoliageDarkOak:
		y := a.y + offset
		if a.doubleTrunk {
			row(a.x, y, a.z, leafRadius+2, -1)
			row(a.x, y, a.z, leafRadius+3, 0)
			row(a.x, y, a.z, leafRadius+2, 1)
			if rng.Intn(2) == 0 {
				row(a.x, y, a.z, leafRadius, 2)
			}
		} else {
			row(a.x, y, a.z, leafRadius+2, -1)
			row(a.x, y, a.z, leafRadius+1, 0)
		}

	case FoliageAcacia:
		y := a.y + offset
		row(a.x, y, a.z, leafRadius+a.radiusOffset, -1-foliageHeight)
		row(a.x, y, a.z, leafRadius-1, -foliageHeight)
		row(a.x, y, a.z, leafRadius+a.radiusOffset-1, 0)

	case FoliageBush:
		for yo := offset; yo >= offset-foliageHeight; yo-- {
			row(a.x, a.y, a.z, leafRadius+a.radiusOffset-1-yo, yo)
		}

	case FoliageSpruce:
		r := rng.Intn(2)
		maxR, minR := 1, 0
		for yo := offset; yo >= -foliageHeight; yo-- {
			row(a.x, a.y, a.z, r, yo)
			if r >= maxR {
				r = minR
				minR = 1
				if maxR+1 < leafRadius+a.radiusOffset {
					maxR++
				} else {
					maxR = leafRadius + a.radiusOffset
				}
			} else {
				r++
			}
		}

	case FoliagePine:
		r := 0
		for yo := offset; yo >= offset-foliageHeight; yo-- {
			row(a.x, a.y, a.z, r, yo)
			switch {
			case r >= 1 && yo == offset-foliageHeight+1:
				r--
			case r < leafRadius+a.radiusOffset:
				r++
			}
		}

	case FoliageMegaPine:
		prev := 0
		for yy := a.y - foliageHeight + offset; yy <= a.y+offset; yy++ {
			yo := a.y - yy
			smooth := leafRadius + a.radiusOffset + int(math.Floor(float64(yo)/float64(foliageHeight)*3.5))
			jagged := smooth
			if yo > 0 && smooth == prev && yy&1 == 0 {
				jagged = smooth + 1
			}
			c.placeLeavesRow(rng, a.x, yy, a.z, jagged, 0, a.doubleTrunk, set)
			prev = smooth
		}

	case FoliageMegaJungle:
		leafHeight := foliageHeight
		if !a.doubleTrunk {
			leafHeight = 1 + rng.Intn(2)
		}
		for yo := offset; yo >= offset-leafHeight; yo-- {
			row(a.x, a.y, a.z, leafRadius+a.radiusOffset+1-yo, yo)
		}

	case FoliageRandomSpread:
		for i := 0; i < c.LeafPlacementAttempts; i++ {
			dx := rng.Intn(leafRadius) - rng.Intn(leafRadius)
			dy := rng.Intn(foliageHeight) - rng.Intn(foliageHeight)
			dz := rng.Intn(leafRadius) - rng.Intn(leafRadius)
			set(a.x+dx, a.y+dy, a.z+dz, c.Leaves, true)
		}

	case FoliageCherry:
		y := a.y + offset
		r := leafRadius + a.radiusOffset - 1
		row(a.x, y, a.z, r-2, foliageHeight-3)
		row(a.x, y, a.z, r-1, foliageHeight-4)
		for yy := foliageHeight - 5; yy >= 0; yy-- {
			row(a.x, y, a.z, r, yy)
		}
		c.cherryHangingRow(rng, a.x, y, a.z, r, -1, a.doubleTrunk, set)
		c.cherryHangingRow(rng, a.x, y, a.z, r-1, -2, a.doubleTrunk, set)
	}
}

// placeLeavesRow is the shared row painter. The doubleTrunk offset is what
// makes a 2x2-trunked canopy square rather than lopsided: the row runs one
// further in +x/+z, and the skip test folds a coordinate to whichever of the
// two trunk columns it is nearer.
func (c *TreeConfig) placeLeavesRow(rng TreeRNG, ox, oy, oz, radius, y int, doubleTrunk bool, set TreeSetter) {
	off := 0
	if doubleTrunk {
		off = 1
	}
	for dx := -radius; dx <= radius+off; dx++ {
		for dz := -radius; dz <= radius+off; dz++ {
			if c.skipSigned(rng, dx, y, dz, radius, doubleTrunk) {
				continue
			}
			set(ox+dx, oy+y, oz+dz, c.Leaves, true)
		}
	}
}

// skipSigned folds the signed offsets for a double trunk, then asks the
// placer. Dark oak overrides the signed form itself to knock a corner out.
func (c *TreeConfig) skipSigned(rng TreeRNG, dx, y, dz, radius int, doubleTrunk bool) bool {
	if c.Foliage == FoliageDarkOak && y == 0 && doubleTrunk &&
		(dx == -radius || dx >= radius) && (dz == -radius || dz >= radius) {
		return true
	}
	adx, adz := abs(dx), abs(dz)
	if doubleTrunk {
		adx = min(abs(dx), abs(dx-1))
		adz = min(abs(dz), abs(dz-1))
	}
	return c.shouldSkip(rng, adx, y, adz, radius, doubleTrunk)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// shouldSkip is each placer's own hole rule. These are what give a species its
// silhouette — the rounded oak, the square-cut acacia, the ragged mega pine —
// and they are the part a hand-rolled blob cannot express at all.
func (c *TreeConfig) shouldSkip(rng TreeRNG, dx, y, dz, radius int, doubleTrunk bool) bool {
	corner := dx == radius && dz == radius
	switch c.Foliage {
	case FoliageBlob:
		return corner && (rng.Intn(2) == 0 || y == 0)
	case FoliageFancy:
		fx, fz := float64(dx)+0.5, float64(dz)+0.5
		return fx*fx+fz*fz > float64(radius*radius)
	case FoliageDarkOak:
		if y == -1 && !doubleTrunk {
			return corner
		}
		if y == 1 {
			return dx+dz > radius*2-2
		}
		return false
	case FoliageAcacia:
		if y == 0 {
			return (dx > 1 || dz > 1) && dx != 0 && dz != 0
		}
		return corner && radius > 0
	case FoliageBush:
		return corner && rng.Intn(2) == 0
	case FoliageSpruce, FoliagePine:
		return corner && radius > 0
	case FoliageMegaPine, FoliageMegaJungle:
		if dx+dz >= 7 {
			return true
		}
		return dx*dx+dz*dz > radius*radius
	case FoliageCherry:
		if y == -1 && (dx == radius || dz == radius) && rng.Float64() < c.WideBottomHoleChance {
			return true
		}
		if radius > 2 {
			return corner || (dx+dz > radius*2-2 && rng.Float64() < c.CornerHoleChance)
		}
		return corner && rng.Float64() < c.CornerHoleChance
	}
	return false
}

// cherryHangingRow is the cherry's drooping fringe: a normal row, then one or
// two leaves hung under the rim wherever there are leaves above.
func (c *TreeConfig) cherryHangingRow(rng TreeRNG, ox, oy, oz, radius, y int, doubleTrunk bool, set TreeSetter) {
	c.placeLeavesRow(rng, ox, oy, oz, radius, y, doubleTrunk, set)
	off := 0
	if doubleTrunk {
		off = 1
	}
	for _, along := range horiz {
		// The clockwise perpendicular, which is how vanilla walks the rim.
		toEdgeX, toEdgeZ := -along[1], along[0]
		edge := radius
		if toEdgeX > 0 || toEdgeZ > 0 {
			edge = radius + off
		}
		px := ox + toEdgeX*edge - along[0]*radius
		pz := oz + toEdgeZ*edge - along[1]*radius
		py := oy + y - 1
		for i := -radius; i < radius+off; i++ {
			if abs(px-ox)+abs(py-(oy-1))+abs(pz-oz) < 7 && rng.Float64() <= c.HangingLeavesChance {
				set(px, py, pz, c.Leaves, true)
				if abs(px-ox)+abs(py-1-(oy-1))+abs(pz-oz) < 7 && rng.Float64() <= c.HangingExtChance {
					set(px, py-1, pz, c.Leaves, true)
				}
			}
			px += along[0]
			pz += along[1]
		}
	}
}

// ---- fancy (large oak) -------------------------------------------------------

const (
	fancyTrunkHeightScale  = 0.618
	fancyClusterDensity    = 1.382
	fancyBranchSlope       = 0.381
	fancyBranchLengthMagic = 0.328
)

// fancyTrunk is the large oak: a tall trunk with limbs arcing out to clusters
// of foliage, each of which becomes its own attachment.
func (c *TreeConfig) fancyTrunk(rng TreeRNG, x, y, z, treeHeight int, set TreeSetter, free TreeFree) []foliageAttachment {
	height := treeHeight + 2
	trunkHeight := int(math.Floor(float64(height) * fancyTrunkHeightScale))

	// NOTE: vanilla writes Math.min here, not max, so this is always <= 1 —
	// a Mojang slip that has shipped for years and is part of the tree's
	// actual look. Reproduced deliberately; "fixing" it would thicken every
	// large oak away from what players know.
	clusters := min(1, int(math.Floor(fancyClusterDensity+math.Pow(float64(height)/13.0, 2))))

	trunkTop := y + trunkHeight
	type coord struct {
		x, y, z    int
		branchBase int
	}
	coords := []coord{{x, y + height - 5, z, trunkTop}}

	for relY := height - 5; relY >= 0; relY-- {
		shape := fancyTreeShape(height, relY)
		if shape < 0 {
			continue
		}
		for i := 0; i < clusters; i++ {
			radius := float64(shape) * (rng.Float64() + fancyBranchLengthMagic)
			angle := rng.Float64() * 2 * math.Pi
			sx := x + int(math.Floor(radius*math.Sin(angle)+0.5))
			sz := z + int(math.Floor(radius*math.Cos(angle)+0.5))
			sy := y + relY - 1
			if !c.fancyLimb(sx, sy, sz, sx, sy+5, sz, false, set, free) {
				continue
			}
			dx, dz := x-sx, z-sz
			bh := float64(sy) - math.Sqrt(float64(dx*dx+dz*dz))*fancyBranchSlope
			branchTop := int(bh)
			if bh > float64(trunkTop) {
				branchTop = trunkTop
			}
			if c.fancyLimb(x, branchTop, z, sx, sy, sz, false, set, free) {
				coords = append(coords, coord{sx, sy, sz, branchTop})
			}
		}
	}
	c.fancyLimb(x, y, z, x, y+trunkHeight, z, true, set, free)

	var atts []foliageAttachment
	for _, co := range coords {
		if co.branchBase-y < int(float64(height)*0.2) {
			continue
		}
		if !(co.x == x && co.branchBase == co.y && co.z == z) {
			c.fancyLimb(x, co.branchBase, z, co.x, co.y, co.z, true, set, free)
		}
		atts = append(atts, foliageAttachment{co.x, co.y, co.z, 0, false})
	}
	return atts
}

// fancyLimb walks a straight line of logs between two points — or, dry, only
// reports whether the line is clear.
func (c *TreeConfig) fancyLimb(x0, y0, z0, x1, y1, z1 int, place bool, set TreeSetter, free TreeFree) bool {
	if !place && x0 == x1 && y0 == y1 && z0 == z1 {
		return true
	}
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	steps := max(abs(dx), max(abs(dy), abs(dz)))
	if steps == 0 {
		return true
	}
	fx := float64(dx) / float64(steps)
	fy := float64(dy) / float64(steps)
	fz := float64(dz) / float64(steps)
	for i := 0; i <= steps; i++ {
		px := x0 + int(math.Floor(0.5+float64(i)*fx))
		py := y0 + int(math.Floor(0.5+float64(i)*fy))
		pz := z0 + int(math.Floor(0.5+float64(i)*fz))
		if place {
			set(px, py, pz, axisLog(c.Log, fancyAxis(x0, z0, px, pz)), false)
		} else if !free(px, py, pz) {
			return false
		}
	}
	return true
}

// fancyAxis lays a limb's logs along whichever horizontal axis it travels
// furthest on, or upright if it is climbing.
func fancyAxis(x0, z0, px, pz int) int {
	xd, zd := abs(px-x0), abs(pz-z0)
	m := max(xd, zd)
	if m == 0 {
		return 1 // Y
	}
	if xd == m {
		return 0 // X
	}
	return 2 // Z
}

// fancyTreeShape is the profile the clusters are scattered on: nothing below
// 30% of the height, then a half-ellipse.
func fancyTreeShape(height, y int) float64 {
	if float64(y) < float64(height)*0.3 {
		return -1
	}
	r := float64(height) / 2
	adj := r - float64(y)
	d := math.Sqrt(r*r - adj*adj)
	if adj == 0 {
		d = r
	} else if math.Abs(adj) >= r {
		return 0
	}
	return d * 0.5
}
