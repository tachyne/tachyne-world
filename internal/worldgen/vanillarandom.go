package worldgen

// Vanilla's RandomSource for the vanilla generator, behind one interface so
// the noise router and the material rules work the same over both of 26.3's
// algorithms: XoroshiroRandomSource (the overworld; vnXoroshiro in
// vanillanoise.go) and LegacyRandomSource (java.util.Random's LCG, which the
// Nether and End settings ask for with legacy_random_source), each with its
// positional factory (forkPositional: at(x, y, z) and fromHashOf(name)).

// vtRandom is RandomSource.
type vtRandom interface {
	nextLong() int64
	nextInt(bound int32) int32
	nextFloat() float32
	nextDouble() float64
	nextBoolean() bool
	forkPositional() vtPositional
}

// vtPositional is PositionalRandomFactory.
type vtPositional interface {
	at(x, y, z int32) vtRandom
	fromHashOf(name string) vtRandom
}

// vtNextIntBetween is RandomSource.nextIntBetweenInclusive.
func vtNextIntBetween(r vtRandom, lo, hi int32) int32 { return r.nextInt(hi-lo+1) + lo }

// vtXoro adapts vnXoroshiro to vtRandom.
type vtXoro struct{ r *vnXoroshiro }

func newVTXoroSeed(seed int64) vtXoro { return vtXoro{newVNXoroshiroSeed(seed)} }

func (x vtXoro) nextLong() int64           { return int64(x.r.nextLong()) }
func (x vtXoro) nextInt(bound int32) int32 { return x.r.nextInt(bound) }
func (x vtXoro) nextDouble() float64       { return x.r.nextDouble() }
func (x vtXoro) nextBoolean() bool         { return x.r.nextLong()&1 != 0 }

// nextFloat is XoroshiroRandomSource.nextFloat: the top 24 bits.
func (x vtXoro) nextFloat() float32 { return float32(x.r.nextLong()>>40) * float32(5.9604645e-8) }

func (x vtXoro) forkPositional() vtPositional { return vtXoroPos{x.r.forkPositional()} }

// vtXoroPos is XoroshiroPositionalRandomFactory.
type vtXoroPos struct{ p vnPositional }

func (p vtXoroPos) at(x, y, z int32) vtRandom       { return vtXoro{p.p.at(x, y, z)} }
func (p vtXoroPos) fromHashOf(name string) vtRandom { return vtXoro{p.p.fromHashOf(name)} }

// vtLegacy is LegacyRandomSource: java.util.Random's 48-bit LCG.
type vtLegacy struct{ seed int64 }

func newVTLegacy(seed int64) *vtLegacy {
	return &vtLegacy{(seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *vtLegacy) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

// nextInt is BitRandomSource.nextInt(bound).
func (r *vtLegacy) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32(int64(bound) * int64(r.next(31)) >> 31)
	}
	for {
		sample := r.next(31)
		mod := sample % bound
		if sample-mod+(bound-1) >= 0 {
			return mod
		}
	}
}

func (r *vtLegacy) nextLong() int64 {
	upper := int64(r.next(32))
	lower := int64(r.next(32))
	return upper<<32 + lower
}

func (r *vtLegacy) nextBoolean() bool  { return r.next(1) != 0 }
func (r *vtLegacy) nextFloat() float32 { return float32(r.next(24)) * float32(5.9604645e-8) }

func (r *vtLegacy) nextDouble() float64 {
	upper := int64(r.next(26))
	lower := int64(r.next(27))
	return float64(upper<<27+lower) * vnDoubleUnit
}

func (r *vtLegacy) forkPositional() vtPositional { return vtLegacyPos{r.nextLong()} }

// vtLegacyPos is LegacyRandomSource.LegacyPositionalRandomFactory.
type vtLegacyPos struct{ seed int64 }

func (p vtLegacyPos) at(x, y, z int32) vtRandom {
	return newVTLegacy(mthGetSeed(x, y, z) ^ p.seed)
}

func (p vtLegacyPos) fromHashOf(name string) vtRandom {
	return newVTLegacy(int64(javaStringHash(name)) ^ p.seed)
}

// javaStringHash is String.hashCode.
func javaStringHash(s string) int32 {
	var h int32
	for _, c := range []rune(s) {
		if c >= 0x10000 { // a surrogate pair, as Java's UTF-16 holds it
			c -= 0x10000
			h = 31*h + int32(0xD800+(c>>10))
			h = 31*h + int32(0xDC00+(c&0x3FF))
			continue
		}
		h = 31*h + int32(c)
	}
	return h
}

// vtAlgorithm is WorldgenRandom.Algorithm: a new source for a seed.
func vtNewRandom(legacy bool, seed int64) vtRandom {
	if legacy {
		return newVTLegacy(seed)
	}
	return newVTXoroSeed(seed)
}
