package worldgen

// The canyon carver (26.3's CanyonWorldCarver, configured carver "canyon",
// the third in every overworld biome's list) for the vanilla generator,
// drawn from vanilla's own random as the cave carvers are
// (vanillacarvers.go): a start chunk's WorldgenRandom set by
// setLargeFeatureSeed(seed + 2, chunk), one chunk in a hundred, a ravine
// wandering up to seven chunks with its per-height width factors from its
// own java.util.Random. The engine's own ravines (canyons.go) stay the
// native generator's.

const (
	vcCanyonProbability = float32(0.01)
	vcCanyonMinY        = 10 // y: uniform 10..67 (absolute)
	vcCanyonMaxY        = 67
)

// vtCarveMask runs the overworld's three carvers (cave, cave_extra_underground,
// canyon) from every start chunk within eight into chunk (cx, cz)'s mask,
// over carvable rows minY..maxY.
func vtCarveMask(seed int64, cx, cz int32, minY, maxY, genDepth int, mask []uint64) {
	out := &vcCarve{cx: cx, cz: cz, mask: mask, minY: minY, maxY: maxY}
	for dx := int32(-vcCarverRange); dx <= vcCarverRange; dx++ {
		for dz := int32(-vcCarverRange); dz <= vcCarverRange; dz++ {
			sx, sz := cx+dx, cz+dz
			for i, c := range vcCaveCarvers {
				r := vcLargeFeatureRandom(seed+int64(i), sx, sz)
				if r.nextFloat() <= c.probability {
					c.carve(r, sx, sz, out)
				}
			}
			r := vcLargeFeatureRandom(seed+2, sx, sz)
			if r.nextFloat() <= vcCanyonProbability {
				out.canyonStart(r, sx, sz, genDepth)
			}
		}
	}
}

// canyonStart is CanyonWorldCarver.carve from start chunk (sx, sz).
func (out *vcCarve) canyonStart(r *javaRandom, sx, sz int32, genDepth int) {
	const maxDistance = vcCarverMaxDistance
	x := float64(int(sx)*16 + int(r.nextInt(16)))
	y := float64(int(r.nextInt(vcCanyonMaxY-vcCanyonMinY+1)) + vcCanyonMinY)
	z := float64(int(sz)*16 + int(r.nextInt(16)))
	hRot := r.nextFloat() * vcTwoPi
	vRot := vcUniform(r, -0.125, 0.125)
	const yScale = 3.0
	// thickness: trapezoid 0..6, plateau 2
	thick := float32(0) + r.nextFloat()*float32(4) + r.nextFloat()*float32(2)
	dist := int(float32(maxDistance) * vcUniform(r, 0.75, 1))
	out.canyon(genDepth, r.nextLong(), x, y, z, thick, hRot, vRot, 0, dist, yScale)
}

// canyon is CanyonWorldCarver.doCarve.
func (out *vcCarve) canyon(genDepth int, seed int64, x, y, z float64, thick, hRot, vRot float32, step, dist int, yScale float64) {
	r := newJavaRandom(seed)
	width := make([]float32, genDepth)
	wf := float32(1)
	for i := 0; i < genDepth; i++ {
		if i == 0 || r.nextInt(3) == 0 {
			wf = 1 + r.nextFloat()*r.nextFloat()
		}
		width[i] = wf * wf
	}
	var yRota, xRota float32
	for cur := step; cur < dist; cur++ {
		hr := 1.5 + float64(mthSin(float64(float32(cur)*vcPiF/float32(dist)))*thick)
		vr := hr * yScale
		hr *= float64(vcUniform(r, 0.75, 1))
		// updateVerticalRadius: default factor 1, centre factor 0
		mult := 1 - vcAbs(0.5-float32(cur)/float32(dist))*2
		factor := float32(1) + float32(0)*mult
		vr = float64(factor) * vr * float64(vcUniform(r, 0.75, 1))
		xc := mthCos(float64(vRot))
		xs := mthSin(float64(vRot))
		x += float64(mthCos(float64(hRot)) * xc)
		y += float64(xs)
		z += float64(mthSin(float64(hRot)) * xc)
		vRot *= 0.7
		vRot += xRota * 0.05
		hRot += yRota * 0.05
		xRota *= 0.8
		yRota *= 0.5
		a, b, c := r.nextFloat(), r.nextFloat(), r.nextFloat()
		xRota += (a - b) * c * 2
		a, b, c = r.nextFloat(), r.nextFloat(), r.nextFloat()
		yRota += (a - b) * c * 4
		if r.nextInt(4) != 0 {
			if !out.canReach(x, z, cur, dist, thick) {
				return
			}
			out.canyonEllipsoid(x, y, z, hr, vr, width)
		}
	}
}

// canyonEllipsoid is WorldCarver.carveEllipsoid with the canyon's skip
// rule (the wall's width factor at the height).
func (out *vcCarve) canyonEllipsoid(x, y, z, hr, vr float64, width []float32) {
	bx, bz := int(out.cx)*16, int(out.cz)*16
	maxDelta := 16 + hr*2
	if vcAbs64(x-float64(bx+8)) > maxDelta || vcAbs64(z-float64(bz+8)) > maxDelta {
		return
	}
	minX := max(vtMthFloor(x-hr)-bx-1, 0)
	maxX := min(vtMthFloor(x+hr)-bx, 15)
	minY := max(vtMthFloor(y-vr)-1, out.minY)
	maxY := min(vtMthFloor(y+vr)+1, out.maxY)
	minZ := max(vtMthFloor(z-hr)-bz-1, 0)
	maxZ := min(vtMthFloor(z+hr)-bz, 15)
	for lx := minX; lx <= maxX; lx++ {
		xd := (float64(bx+lx) + 0.5 - x) / hr
		for lz := minZ; lz <= maxZ; lz++ {
			zd := (float64(bz+lz) + 0.5 - z) / hr
			if xd*xd+zd*zd >= 1 {
				continue
			}
			for wy := maxY; wy > minY; wy-- {
				yd := (float64(wy) - 0.5 - y) / vr
				if (xd*xd+zd*zd)*float64(width[wy-MinY-1])+yd*yd/6 >= 1 {
					continue
				}
				i := (wy-MinY)*256 + lz*16 + lx
				out.mask[i>>6] |= 1 << (i & 63)
			}
		}
	}
}

func vcAbs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
