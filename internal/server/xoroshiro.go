package server

import "math/bits"

// Vanilla's Xoroshiro128++ random source, as far as the block entities that
// draw from it need: the seed upgrade (a legacy 64-bit seed mixed into two
// halves), the generator, nextInt/nextInt(bound)/nextIntBetweenInclusive,
// and forkPositional().at(x, y, z) — the per-block stream a geyser draws its
// timings from, so the same geyser in the same world always keeps the same
// rhythm.

const (
	xoroGolden = 0x9E3779B97F4A7C15 // RandomSupport.GOLDEN_RATIO_64
	xoroSilver = 0x6A09E667F3BCC909 // RandomSupport.SILVER_RATIO_64
)

// xoroshiro is Xoroshiro128PlusPlus behind XoroshiroRandomSource.
type xoroshiro struct{ lo, hi uint64 }

// mixStafford13 is RandomSupport.mixStafford13 (the SplitMix64 finaliser).
func mixStafford13(z uint64) uint64 {
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

// newXoroshiroPair is the two-long constructor: an all-zero state is
// replaced by the golden and silver ratios.
func newXoroshiroPair(lo, hi uint64) *xoroshiro {
	if lo|hi == 0 {
		lo, hi = xoroGolden, xoroSilver
	}
	return &xoroshiro{lo, hi}
}

// newXoroshiro is XoroshiroRandomSource(long): upgradeSeedTo128bit.
func newXoroshiro(seed int64) *xoroshiro {
	lo := uint64(seed) ^ xoroSilver
	hi := lo + xoroGolden
	return newXoroshiroPair(mixStafford13(lo), mixStafford13(hi))
}

func (r *xoroshiro) nextLong() uint64 {
	s0, s1 := r.lo, r.hi
	out := bits.RotateLeft64(s0+s1, 17) + s0
	s1 ^= s0
	r.lo = bits.RotateLeft64(s0, 49) ^ s1 ^ s1<<21
	r.hi = bits.RotateLeft64(s1, 28)
	return out
}

func (r *xoroshiro) nextInt() int32 { return int32(r.nextLong()) }

// nextIntN is nextInt(bound): Lemire's multiply-shift with the unbiased
// rejection vanilla uses.
func (r *xoroshiro) nextIntN(bound int32) int32 {
	b := uint64(uint32(bound))
	m := uint64(uint32(r.nextInt())) * b
	if frac := m & 0xFFFFFFFF; frac < b {
		start := uint64(uint32(-bound) % uint32(bound))
		for frac < start {
			m = uint64(uint32(r.nextInt())) * b
			frac = m & 0xFFFFFFFF
		}
	}
	return int32(m >> 32)
}

// nextIntBetween is RandomSource.nextIntBetweenInclusive.
func (r *xoroshiro) nextIntBetween(lo, hi int32) int32 { return r.nextIntN(hi-lo+1) + lo }

// xoroPositional is XoroshiroPositionalRandomFactory.
type xoroPositional struct{ lo, hi uint64 }

func (r *xoroshiro) forkPositional() xoroPositional {
	lo := r.nextLong()
	return xoroPositional{lo, r.nextLong()}
}

// at is the factory's per-position source: Mth.getSeed xored into the low half.
func (f xoroPositional) at(x, y, z int) *xoroshiro {
	return newXoroshiroPair(uint64(mthGetSeed(int32(x), int32(y), int32(z)))^f.lo, f.hi)
}

// mthGetSeed is Mth.getSeed(x, y, z). The x product is an int product in
// vanilla and wraps at 32 bits before it is widened.
func mthGetSeed(x, y, z int32) int64 {
	seed := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	seed = seed*seed*42317861 + seed*11
	return seed >> 16
}
