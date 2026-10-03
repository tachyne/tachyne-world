package worldgen

import (
	"math"
	"sync"
)

// The cave carvers of a vanilla-caves world (vanillacaves.go): 26.3's
// configured carvers "cave" and "cave_extra_underground", both
// CaveWorldCarver, which every overworld biome lists ahead of "canyon" (the
// engine's ravines, canyons.go, carved in both modes). Each start chunk
// draws its caves from a java.util.Random set as
// WorldgenRandom.setLargeFeatureSeed(seed + carver index, chunk) — one in
// 0.15 (cave) or 0.07 (extra underground) chunks starts — up to 14 caves
// biased to few, each maybe a room and one to four wandering tunnels that
// split once; a chunk collects the cells every start chunk within eight
// carves into it, through vanilla's ellipsoid, floor-level and reach rules.
// The cells feed the chunk's cave mask, so the terrain every feature reads
// (dungeons included) has them, as vanilla's carvers run before features.

// vcCaveCarver is one CaveWorldCarver configuration (both share every
// field but these).
type vcCaveCarver struct {
	probability float32
	yMax        int // y: uniform from above_bottom 8 to this (absolute)
}

var vcCaveCarvers = [2]vcCaveCarver{
	{0.15, 180}, // cave
	{0.07, 47},  // cave_extra_underground
}

const (
	vcCarverRange       = 8                    // generateCarvers looks this many chunks out
	vcCarverMaxDistance = (4*2 - 1) * 16       // CaveWorldCarver: range 4
	vcCarverMinY        = MinY + 8             // above_bottom 8
	vcCarverTopProtect  = 7                    // cells under the ceiling a carver leaves
	vcTwoPi             = float32(math.Pi * 2) // (float) (Math.PI * 2)
	vcHalfPi            = float32(math.Pi / 2) // (float) (Math.PI / 2)
	vcPiF               = float32(math.Pi)     // (float) Math.PI
)

// mthSinTable is Mth.SIN: 65536 float sines.
var (
	mthSinOnce  sync.Once
	mthSinTable []float32
)

func mthSinInit() {
	mthSinTable = make([]float32, 65536)
	for i := range mthSinTable {
		mthSinTable[i] = float32(math.Sin(float64(i) / 10430.378350470453))
	}
}

// mthSin is Mth.sin: the table looked up at the truncated angle.
func mthSin(a float64) float32 {
	mthSinOnce.Do(mthSinInit)
	return mthSinTable[int(int64(a*10430.378350470453)&65535)]
}

// mthCos is Mth.cos.
func mthCos(a float64) float32 {
	mthSinOnce.Do(mthSinInit)
	return mthSinTable[int(int64(a*10430.378350470453+16384)&65535)]
}

// nextFloat is java.util.Random.nextFloat.
func (r *javaRandom) nextFloat() float32 {
	return float32(r.next(24)) * float32(5.9604645e-8)
}

// vcUniform is UniformFloat.sample: min + nextFloat()·(max - min).
func vcUniform(r *javaRandom, lo, hi float32) float32 {
	return r.nextFloat()*(hi-lo) + lo
}

// vcLargeFeatureRandom is WorldgenRandom.setLargeFeatureSeed(seed, x, z).
func vcLargeFeatureRandom(seed int64, x, z int32) *javaRandom {
	r := newJavaRandom(seed)
	xs := r.nextLong()
	zs := r.nextLong()
	return newJavaRandom(int64(x)*xs ^ int64(z)*zs ^ seed)
}

// vcCarve is one target chunk's carving: its mask and range.
type vcCarve struct {
	cx, cz     int32
	mask       []uint64
	minY, maxY int
}

// carveCaves runs both cave carvers from every start chunk in range into
// chunk (cx, cz)'s mask.
func (v *vanillaCaves) carveCaves(g *Generator, cx, cz int32, mask []uint64) {
	out := &vcCarve{cx: cx, cz: cz, mask: mask, minY: MinY + 1, maxY: g.Ceiling() - 1 - vcCarverTopProtect}
	for dx := int32(-vcCarverRange); dx <= vcCarverRange; dx++ {
		for dz := int32(-vcCarverRange); dz <= vcCarverRange; dz++ {
			sx, sz := cx+dx, cz+dz
			for i, c := range vcCaveCarvers {
				r := vcLargeFeatureRandom(v.seed+int64(i), sx, sz)
				if r.nextFloat() <= c.probability {
					c.carve(r, sx, sz, out)
				}
			}
		}
	}
}

// carve is CaveWorldCarver.carve from start chunk (sx, sz).
func (c vcCaveCarver) carve(r *javaRandom, sx, sz int32, out *vcCarve) {
	n := int(r.nextInt(r.nextInt(r.nextInt(14+1)+1) + 1)) // very_biased_to_bottom 0..14
	for cave := 0; cave < n; cave++ {
		x := float64(int(sx)*16 + int(r.nextInt(16)))
		y := float64(int(r.nextInt(int32(c.yMax-vcCarverMinY+1))) + vcCarverMinY)
		z := float64(int(sz)*16 + int(r.nextInt(16)))
		hMul := float64(vcUniform(r, 0.7, 1.4))
		vMul := float64(vcUniform(r, 0.8, 1.3))
		const startVMul = 1.0 // start_vertical_radius_multiplier: the default constant
		floor := float64(vcUniform(r, -1, -0.4))
		tunnels := 1
		if r.nextInt(4) == 0 {
			yScale := float64(vcUniform(r, 0.1, 0.9))
			thick := 1 + r.nextFloat()*6
			hr := 1.5 + float64(mthSin(float64(vcHalfPi))*thick)
			out.ellipsoid(x+1, y, z, hr, hr*yScale, floor)
			tunnels += int(r.nextInt(4))
		}
		for t := 0; t < tunnels; t++ {
			hRot := r.nextFloat() * vcTwoPi
			vRot := (r.nextFloat() - 0.5) / 4
			thick := r.nextFloat()*2 + r.nextFloat()*1 // trapezoid 0..3, plateau 1
			if r.nextInt(10) == 0 {                    // weird_thickness_bias
				thick *= r.nextFloat()*r.nextFloat()*3 + 1
			}
			dist := vcCarverMaxDistance - int(r.nextInt(vcCarverMaxDistance/4))
			seed := r.nextLong()
			out.tunnel(seed, x, y, z, hMul, vMul, thick, hRot, vRot, 0, dist, startVMul, floor)
		}
	}
}

// tunnel is CaveWorldCarver.createTunnel.
func (out *vcCarve) tunnel(seed int64, x, y, z, hMul, vMul float64, thick, hRot, vRot float32, step, dist int, yScale, floor float64) {
	r := newJavaRandom(seed)
	split := int(r.nextInt(int32(dist/2))) + dist/4
	steep := r.nextInt(6) == 0
	var yRota, xRota float32
	for cur := step; cur < dist; cur++ {
		hr := 1.5 + float64(mthSin(float64(vcPiF*float32(cur)/float32(dist)))*thick)
		vr := hr * yScale
		cosX := mthCos(float64(vRot))
		x += float64(mthCos(float64(hRot)) * cosX)
		y += float64(mthSin(float64(vRot)))
		z += float64(mthSin(float64(hRot)) * cosX)
		if steep {
			vRot *= 0.92
		} else {
			vRot *= 0.7
		}
		vRot += xRota * 0.1
		hRot += yRota * 0.1
		xRota *= 0.9
		yRota *= 0.75
		a, b, c := r.nextFloat(), r.nextFloat(), r.nextFloat()
		xRota += (a - b) * c * 2
		a, b, c = r.nextFloat(), r.nextFloat(), r.nextFloat()
		yRota += (a - b) * c * 4
		if cur == split && thick > 1 {
			s1 := r.nextLong()
			t1 := r.nextFloat()*0.5 + 0.5
			out.tunnel(s1, x, y, z, hMul, vMul, t1, hRot-vcHalfPi, vRot/3, cur, dist, 1, floor)
			s2 := r.nextLong()
			t2 := r.nextFloat()*0.5 + 0.5
			out.tunnel(s2, x, y, z, hMul, vMul, t2, hRot+vcHalfPi, vRot/3, cur, dist, 1, floor)
			return
		}
		if r.nextInt(4) != 0 {
			if !out.canReach(x, z, cur, dist, thick) {
				return
			}
			out.ellipsoid(x, y, z, hr*hMul, vr*vMul, floor)
		}
	}
}

// canReach is WorldCarver.canReach: could the tunnel's remaining steps
// still come within reach of the chunk's middle?
func (out *vcCarve) canReach(x, z float64, cur, total int, thick float32) bool {
	xd := x - float64(int(out.cx)*16+8)
	zd := z - float64(int(out.cz)*16+8)
	rem := float64(total - cur)
	rr := float64(thick + 2 + 16)
	return xd*xd+zd*zd-rem*rem <= rr*rr
}

// ellipsoid is WorldCarver.carveEllipsoid with CaveWorldCarver's skip rule
// (nothing at or under the floor level, nothing outside the ellipsoid).
func (out *vcCarve) ellipsoid(x, y, z, hr, vr, floor float64) {
	bx, bz := int(out.cx)*16, int(out.cz)*16
	maxDelta := 16 + hr*2
	if math.Abs(x-float64(bx+8)) > maxDelta || math.Abs(z-float64(bz+8)) > maxDelta {
		return
	}
	minX := max(int(math.Floor(x-hr))-bx-1, 0)
	maxX := min(int(math.Floor(x+hr))-bx, 15)
	minY := max(int(math.Floor(y-vr))-1, out.minY)
	maxY := min(int(math.Floor(y+vr))+1, out.maxY)
	minZ := max(int(math.Floor(z-hr))-bz-1, 0)
	maxZ := min(int(math.Floor(z+hr))-bz, 15)
	for lx := minX; lx <= maxX; lx++ {
		xd := (float64(bx+lx) + 0.5 - x) / hr
		for lz := minZ; lz <= maxZ; lz++ {
			zd := (float64(bz+lz) + 0.5 - z) / hr
			if xd*xd+zd*zd >= 1 {
				continue
			}
			for wy := maxY; wy > minY; wy-- {
				yd := (float64(wy) - 0.5 - y) / vr
				if yd <= floor || xd*xd+yd*yd+zd*zd >= 1 {
					continue
				}
				i := (wy-MinY)*256 + lz*16 + lx
				out.mask[i>>6] |= 1 << (i & 63)
			}
		}
	}
}
