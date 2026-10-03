package worldgen

import "math"

// Vanilla's worldgen random sources, for the vanilla generator's placement
// (vanillaplace.go): LegacyRandomSource (java.util.Random's 48-bit LCG),
// and WorldgenRandom — a LegacyRandomSource whose bits come from a wrapped
// source, either another legacy one (structure placement) or a
// XoroshiroRandomSource (biome decoration), with the seeding helpers
// setDecorationSeed, setFeatureSeed, setLargeFeatureSeed and
// setLargeFeatureWithSalt. Every draw (nextInt, nextFloat, nextDouble…) is
// BitRandomSource's, built from next(bits), so a Xoroshiro-backed
// WorldgenRandom draws ints with the java.util.Random rejection loop, not
// Xoroshiro's own Lemire bound.

// vwRandom is a WorldgenRandom (or a bare LegacyRandomSource: legacy, with
// nothing wrapped — they draw the same).
type vwRandom struct {
	xoro  bool
	lseed int64 // the LCG's 48 bits (legacy)
	xo    vnXoroshiro
	count int // WorldgenRandom.count: next() calls
	// wrapper marks a WorldgenRandom: its own Gaussian pair survives a
	// reseed (WorldgenRandom.setSeed reseeds the wrapped source only),
	// where a bare LegacyRandomSource's is reset.
	wrapper    bool
	haveGauss  bool
	nextGaussV float64
}

// newVWLegacy is new WorldgenRandom(new LegacyRandomSource(seed)) — or a
// LegacyRandomSource(seed) on its own.
func newVWLegacy(seed int64) *vwRandom {
	r := &vwRandom{wrapper: true}
	r.setSeed(seed)
	return r
}

// newVWXoroshiro is new WorldgenRandom(new XoroshiroRandomSource(seed)).
func newVWXoroshiro(seed int64) *vwRandom {
	r := &vwRandom{xoro: true, wrapper: true}
	r.setSeed(seed)
	return r
}

// setSeed reseeds the wrapped source (Xoroshiro: the seed upgraded to 128
// bits, as new XoroshiroRandomSource(seed) does).
func (r *vwRandom) setSeed(seed int64) {
	if !r.wrapper {
		r.haveGauss = false
	}
	if r.xoro {
		r.xo = *newVNXoroshiroSeed(seed)
		return
	}
	r.lseed = (seed ^ 0x5DEECE66D) & (1<<48 - 1)
}

// next is next(bits).
func (r *vwRandom) next(bits uint) int32 {
	r.count++
	if r.xoro {
		return int32(r.xo.nextLong() >> (64 - bits))
	}
	r.lseed = (r.lseed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.lseed >> (48 - bits))
}

func (r *vwRandom) nextInt() int32 { return r.next(32) }

// nextIntN is nextInt(bound), bound > 0.
func (r *vwRandom) nextIntN(bound int32) int32 {
	if bound <= 0 {
		panic("vwRandom: bound must be positive")
	}
	if bound&(bound-1) == 0 {
		return int32(int64(bound) * int64(r.next(31)) >> 31)
	}
	for {
		s := r.next(31)
		m := s % bound
		if s-m+(bound-1) >= 0 {
			return m
		}
	}
}

// nextIntIn is nextInt(origin, bound): origin inclusive, bound exclusive.
func (r *vwRandom) nextIntIn(origin, bound int32) int32 {
	return origin + r.nextIntN(bound-origin)
}

// nextIntBetween is nextIntBetweenInclusive(min, max).
func (r *vwRandom) nextIntBetween(lo, hi int32) int32 { return r.nextIntN(hi-lo+1) + lo }

func (r *vwRandom) nextLong() int64 {
	hi := r.next(32)
	lo := r.next(32)
	return int64(hi)<<32 + int64(lo)
}

func (r *vwRandom) nextBool() bool { return r.next(1) != 0 }

func (r *vwRandom) nextFloat() float32 { return float32(r.next(24)) * 5.9604645e-8 }

func (r *vwRandom) nextDouble() float64 {
	hi := r.next(26)
	lo := r.next(27)
	return float64(int64(hi)<<27+int64(lo)) * vnDoubleUnit
}

// nextGaussian is MarsagliaPolarGaussian.nextGaussian over this source.
func (r *vwRandom) nextGaussian() float64 {
	if r.haveGauss {
		r.haveGauss = false
		return r.nextGaussV
	}
	for {
		x := 2*r.nextDouble() - 1
		y := 2*r.nextDouble() - 1
		s := x*x + y*y
		if s < 1 && s != 0 {
			m := math.Sqrt(-2 * math.Log(s) / s)
			r.nextGaussV, r.haveGauss = y*m, true
			return x * m
		}
	}
}

// setDecorationSeed is WorldgenRandom.setDecorationSeed: the seed for a
// chunk's decoration, from the block coordinates of its corner.
func (r *vwRandom) setDecorationSeed(seed int64, x, z int32) int64 {
	r.setSeed(seed)
	xs := r.nextLong() | 1
	zs := r.nextLong() | 1
	res := int64(x)*xs + int64(z)*zs ^ seed
	r.setSeed(res)
	return res
}

// setFeatureSeed is WorldgenRandom.setFeatureSeed: one placed feature's (or
// structure's) stream in a decoration step.
func (r *vwRandom) setFeatureSeed(decorationSeed int64, index, step int) {
	r.setSeed(decorationSeed + int64(index) + 10000*int64(step))
}

// setLargeFeatureSeed is WorldgenRandom.setLargeFeatureSeed (carvers,
// structure starts, a structure set's pick between its structures).
func (r *vwRandom) setLargeFeatureSeed(seed int64, cx, cz int32) {
	r.setSeed(seed)
	xs := r.nextLong()
	zs := r.nextLong()
	r.setSeed(int64(cx)*xs ^ int64(cz)*zs ^ seed)
}

// setLargeFeatureWithSalt is WorldgenRandom.setLargeFeatureWithSalt (the
// random-spread placement's grid cell).
func (r *vwRandom) setLargeFeatureWithSalt(seed int64, x, z, salt int32) {
	r.setSeed(int64(x)*341873128712 + int64(z)*132897987541 + seed + int64(salt))
}

// fork is RandomSource.fork: a legacy source forks a new LegacyRandomSource
// seeded from nextLong (and Xoroshiro a new Xoroshiro from two longs).
func (r *vwRandom) fork() *vwRandom {
	if r.xoro {
		lo, hi := r.xo.nextLong(), r.xo.nextLong()
		return &vwRandom{xoro: true, xo: *newVNXoroshiro(lo, hi)}
	}
	return newVWLegacySource(r.nextLong())
}

// newVWLegacySource is a bare LegacyRandomSource (RandomSource.create):
// unlike a WorldgenRandom it forgets a pending Gaussian when reseeded.
func newVWLegacySource(seed int64) *vwRandom {
	r := &vwRandom{}
	r.setSeed(seed)
	return r
}
