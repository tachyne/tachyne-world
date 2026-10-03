package worldgen

import (
	"crypto/md5"
	"encoding/binary"
	"math"
	"math/bits"
	"strconv"
)

// Vanilla's noise, as 26.3 builds it, for the vanilla cave generator
// (vanillacaves.go): the Xoroshiro128++ random source and the positional
// factories that seed every noise from the world seed and the noise's
// name, the gradient (Perlin) noise each octave samples, and NormalNoise —
// two Perlin stacks, the second at 1.0181268882175227 times the frequency,
// normalised so the sum has the target deviation. Arithmetic is float32
// where vanilla's is, so the same seed samples the same values.

// vnXoroshiro is Xoroshiro128PlusPlus behind XoroshiroRandomSource.
type vnXoroshiro struct{ lo, hi uint64 }

func newVNXoroshiro(lo, hi uint64) *vnXoroshiro {
	if lo|hi == 0 {
		lo, hi = 0x9E3779B97F4A7C15, 0x6A09E667F3BCC909 // GOLDEN_RATIO_64, SILVER_RATIO_64
	}
	return &vnXoroshiro{lo, hi}
}

// vnMixStafford13 is RandomSupport.mixStafford13.
func vnMixStafford13(z uint64) uint64 {
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	return z ^ z>>31
}

// newVNXoroshiroSeed is new XoroshiroRandomSource(long): the seed upgraded
// to 128 bits and mixed.
func newVNXoroshiroSeed(seed int64) *vnXoroshiro {
	lo := uint64(seed) ^ 0x6A09E667F3BCC909
	hi := lo + 0x9E3779B97F4A7C15
	return newVNXoroshiro(vnMixStafford13(lo), vnMixStafford13(hi))
}

func (r *vnXoroshiro) nextLong() uint64 {
	s0, s1 := r.lo, r.hi
	out := bits.RotateLeft64(s0+s1, 17) + s0
	s1 ^= s0
	r.lo = bits.RotateLeft64(s0, 49) ^ s1 ^ s1<<21
	r.hi = bits.RotateLeft64(s1, 28)
	return out
}

// nextInt is XoroshiroRandomSource.nextInt(bound): Lemire's multiply with
// the unbiased-bucket retry.
func (r *vnXoroshiro) nextInt(bound int32) int32 {
	m := uint64(uint32(r.nextLong())) * uint64(bound)
	frac := m & 0xFFFFFFFF
	if frac < uint64(bound) {
		threshold := uint64(uint32(-bound) % uint32(bound))
		for frac < threshold {
			m = uint64(uint32(r.nextLong())) * uint64(bound)
			frac = m & 0xFFFFFFFF
		}
	}
	return int32(m >> 32)
}

// vnDoubleUnit is the float literal 1.110223E-16F that nextDouble scales by
// (not quite 2^-53: the literal is rounded to a float first).
var vnDoubleUnit = func() float64 { f := float32(1.110223e-16); return float64(f) }()

func (r *vnXoroshiro) nextDouble() float64 {
	return float64(r.nextLong()>>(64-53)) * vnDoubleUnit
}

// forkPositional is XoroshiroRandomSource.forkPositional.
func (r *vnXoroshiro) forkPositional() vnPositional {
	lo := r.nextLong()
	hi := r.nextLong()
	return vnPositional{lo, hi}
}

// vnPositional is XoroshiroPositionalRandomFactory.
type vnPositional struct{ lo, hi uint64 }

// fromHashOf seeds a source from the MD5 of name xored with the factory.
func (p vnPositional) fromHashOf(name string) *vnXoroshiro {
	h := md5.Sum([]byte(name))
	return newVNXoroshiro(binary.BigEndian.Uint64(h[0:8])^p.lo, binary.BigEndian.Uint64(h[8:16])^p.hi)
}

// vnWorldPositional is RandomState's factory for an overworld seed (the
// overworld does not use the legacy random source).
func vnWorldPositional(seed int64) vnPositional {
	return newVNXoroshiroSeed(seed).forkPositional()
}

// vnGrad is GradientNoise.GRADIENT.
var vnGrad = [16][3]float32{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
	{1, 1, 0}, {0, -1, 1}, {-1, 1, 0}, {0, -1, -1},
}

// vnPerlin is 26.3's PerlinNoise: one gradient-noise octave with a random
// offset and permutation.
type vnPerlin struct {
	perms            [256]uint8
	offX, offY, offZ float64
}

func newVNPerlin(r *vnXoroshiro) *vnPerlin {
	p := &vnPerlin{}
	p.offX = r.nextDouble() * 256
	p.offY = r.nextDouble() * 256
	p.offZ = r.nextDouble() * 256
	for i := range p.perms {
		p.perms[i] = uint8(i)
	}
	for i := 0; i < 256; i++ {
		o := int(r.nextInt(int32(256 - i)))
		p.perms[i], p.perms[o+i] = p.perms[o+i], p.perms[i]
	}
	return p
}

func (p *vnPerlin) permute(x int) int { return int(p.perms[x&0xFF]) }

func vnGradDot(hash int, x, y, z float32) float32 {
	g := vnGrad[hash&15]
	return g[0]*x + g[1]*y + g[2]*z
}

func vnSmoothstep(x float32) float32 { return x * x * x * (x*(x*6-15) + 10) }

func vnLerp(a, p0, p1 float32) float32 { return p0 + a*(p1-p0) }

func vnLerp2(a1, a2, x00, x10, x01, x11 float32) float32 {
	return vnLerp(a2, vnLerp(a1, x00, x10), vnLerp(a1, x01, x11))
}

func vnLerp3(a1, a2, a3, x000, x100, x010, x110, x001, x101, x011, x111 float32) float32 {
	return vnLerp(a3, vnLerp2(a1, a2, x000, x100, x010, x110), vnLerp2(a1, a2, x001, x101, x011, x111))
}

// vnWrap is GradientNoise.wrap: coordinates past 2^24 fold back.
func vnWrap(x float64) float64 {
	if x >= -vnHalfRoundOff && x < vnHalfRoundOff {
		return x
	}
	return x - math.Floor(x/3.3554432e7+0.5)*3.3554432e7
}

// vnHalfRoundOff is Math.nextDown(1.6777216E7).
var vnHalfRoundOff = math.Nextafter(1<<24, 0)

// get is PerlinNoise.get(x, y, z).
func (p *vnPerlin) get(x, y, z float64) float32 {
	x = vnWrap(x) + p.offX
	y = vnWrap(y) + p.offY
	z = vnWrap(z) + p.offZ
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	ix, iy, iz := int(fx), int(fy), int(fz)
	rx, ry, rz := float32(x-fx), float32(y-fy), float32(z-fz)
	x0 := p.permute(ix)
	x1 := p.permute(ix + 1)
	xy00 := p.permute(x0 + iy)
	xy01 := p.permute(x0 + iy + 1)
	xy10 := p.permute(x1 + iy)
	xy11 := p.permute(x1 + iy + 1)
	d000 := vnGradDot(p.permute(xy00+iz), rx, ry, rz)
	d100 := vnGradDot(p.permute(xy10+iz), rx-1, ry, rz)
	d010 := vnGradDot(p.permute(xy01+iz), rx, ry-1, rz)
	d110 := vnGradDot(p.permute(xy11+iz), rx-1, ry-1, rz)
	d001 := vnGradDot(p.permute(xy00+iz+1), rx, ry, rz-1)
	d101 := vnGradDot(p.permute(xy10+iz+1), rx-1, ry, rz-1)
	d011 := vnGradDot(p.permute(xy01+iz+1), rx, ry-1, rz-1)
	d111 := vnGradDot(p.permute(xy11+iz+1), rx-1, ry-1, rz-1)
	return vnLerp3(vnSmoothstep(rx), vnSmoothstep(ry), vnSmoothstep(rz), d000, d100, d010, d110, d001, d101, d011, d111)
}

// vnNoiseParams is a worldgen/noise file: base_octave, base_amplitude,
// octave_count (default 1) and amplitude_modifiers (default all 1). Every
// noise the caves read normalises (the default).
type vnNoiseParams struct {
	baseOctave int
	baseAmp    float64
	octaves    int
	mods       []float64
}

// vnLayer is one NoiseStack layer.
type vnLayer struct {
	noise *vnPerlin
	freq  float64
	amp   float32
}

// vnNormal is a created NormalNoise: its NoiseStack.
type vnNormal struct{ layers []vnLayer }

// get is NoiseStack.get(x, y, z).
func (n *vnNormal) get(x, y, z float64) float32 {
	var v float32
	for _, l := range n.layers {
		v += float32(l.amp * l.noise.get(x*l.freq, y*l.freq, z*l.freq))
	}
	return v
}

// newVNNormal is NormalNoise.create(world.fromHashOf(name)).
func newVNNormal(world vnPositional, name string, np vnNoiseParams) *vnNormal {
	n := np.octaves
	if n == 0 {
		n = 1
	}
	mod := func(i int) float64 {
		if len(np.mods) == 0 {
			return 1
		}
		return np.mods[i]
	}
	type octave struct {
		index      int
		freq, ampl float64
	}
	var octs []octave
	freq := math.Pow(2, float64(np.baseOctave))
	amp := np.baseAmp * (math.Pow(0.5, -float64(n-1)) / (math.Pow(0.5, -float64(n)) - 1))
	for i := 0; i < n; i++ {
		if m := mod(i); m != 0 {
			octs = append(octs, octave{np.baseOctave + i, freq, amp * m})
		}
		freq *= 2
		amp *= 0.5
	}
	target, variance := 0.0, 0.0
	for _, o := range octs {
		a := math.Abs(o.ampl)
		target += a
		d := 0.2702247831245211 * a
		variance += d * d
	}
	norm := 0.0
	if dev := math.Sqrt(variance); dev != 0 {
		norm = (target * 0.3333333333333333) / (dev * math.Sqrt(2))
	}
	r := world.fromHashOf(name)
	first := r.forkPositional()
	second := r.forkPositional()
	out := &vnNormal{}
	for _, o := range octs {
		seed := "octave_" + strconv.Itoa(o.index)
		f := newVNPerlin(first.fromHashOf(seed))
		s := newVNPerlin(second.fromHashOf(seed))
		vf := float32(norm * o.ampl)
		out.layers = append(out.layers, vnLayer{f, o.freq, vf}, vnLayer{s, o.freq * 1.0181268882175227, vf})
	}
	return out
}
