package worldgen

import (
	"math"
	"strconv"
)

// The legacy half of vanilla's noise, for the Nether and the End in vanilla
// mode (vanillanether.go, vanillaend.go). Both dimensions' noise settings
// set legacy_random_source, so 26.3's RandomState seeds them from
// LegacyRandomSource — java.util.Random's 48-bit LCG — rather than
// Xoroshiro: the positional factory hashes a name with String.hashCode, the
// old blended noise (old_blended_noise, BlendedNoise) is drawn from the seed
// itself, the Nether's biome noises take the legacy octave initializer, and
// the End's islands sample a SimplexNoise. Arithmetic is float32 where
// vanilla's is, so the same seed samples the same values; the Perlin octave
// and NormalNoise stack are vanillanoise.go's.

// vdLegacy is LegacyRandomSource (a BitRandomSource).
type vdLegacy struct{ seed int64 }

func newVDLegacy(seed int64) *vdLegacy {
	return &vdLegacy{(seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *vdLegacy) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

// nextInt is BitRandomSource.nextInt(bound).
func (r *vdLegacy) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := r.next(31)
		val := bits % bound
		if bits-val+(bound-1) >= 0 {
			return val
		}
	}
}

func (r *vdLegacy) nextLong() int64 {
	hi := r.next(32)
	lo := r.next(32)
	return int64(hi)<<32 + int64(lo)
}

func (r *vdLegacy) nextFloat() float32 { return float32(r.next(24)) * float32(5.9604645e-8) }

func (r *vdLegacy) nextDouble() float64 {
	hi := int64(r.next(26))
	lo := int64(r.next(27))
	return float64(hi<<27+lo) * vnDoubleUnit
}

// consume is RandomSource.consumeCount: n nextInt() calls.
func (r *vdLegacy) consume(n int) {
	for i := 0; i < n; i++ {
		r.next(32)
	}
}

// forkPositional is LegacyRandomSource.forkPositional.
func (r *vdLegacy) forkPositional() vdLegacyPos { return vdLegacyPos(r.nextLong()) }

// vdLegacyPos is LegacyPositionalRandomFactory.
type vdLegacyPos int64

// fromHashOf seeds a source from String.hashCode(name) xored with the
// factory's seed.
func (p vdLegacyPos) fromHashOf(name string) *vdLegacy {
	return newVDLegacy(int64(javaStringHash(name)) ^ int64(p))
}

// at is LegacyPositionalRandomFactory.at: Mth.getSeed of the block.
func (p vdLegacyPos) at(x, y, z int) *vdLegacy {
	return newVDLegacy(mthGetSeed(int32(x), int32(y), int32(z)) ^ int64(p))
}

// javaStringHash is String.hashCode (UTF-16 units; the names are ASCII).
func javaStringHash(s string) int32 {
	var h int32
	for i := 0; i < len(s); i++ {
		h = 31*h + int32(s[i])
	}
	return h
}

// newVDPerlin is GradientNoise's constructor over a legacy source
// (newVNPerlin's, drawn from java.util.Random's sequence): offsets scaled by
// offScale (256, or 0 for a simplex noise that discards them — the draws
// still happen).
func newVDPerlin(r *vdLegacy, offScale float64) *vnPerlin {
	p := &vnPerlin{}
	p.offX = r.nextDouble() * offScale
	p.offY = r.nextDouble() * offScale
	p.offZ = r.nextDouble() * offScale
	for i := range p.perms {
		p.perms[i] = uint8(i)
	}
	for i := 0; i < 256; i++ {
		o := int(r.nextInt(int32(256 - i)))
		p.perms[i], p.perms[o+i] = p.perms[o+i], p.perms[i]
	}
	return p
}

// vdNormalFactor is NormalNoise's octave list and normalization factor for
// a noise file (newVNNormal's arithmetic).
type vdOctave struct {
	index      int
	freq, ampl float64
}

func vdNormalOctaves(np vnNoiseParams) ([]vdOctave, float64) {
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
	var octs []vdOctave
	freq := math.Pow(2, float64(np.baseOctave))
	amp := np.baseAmp * (math.Pow(0.5, -float64(n-1)) / (math.Pow(0.5, -float64(n)) - 1))
	for i := 0; i < n; i++ {
		if m := mod(i); m != 0 {
			octs = append(octs, vdOctave{np.baseOctave + i, freq, amp * m})
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
	return octs, norm
}

// newVDNormal is Noises.instantiate over a legacy positional factory:
// NormalNoise.create(world.fromHashOf("minecraft:" + name)).
func newVDNormal(world vdLegacyPos, name string, np vnNoiseParams) *vnNormal {
	octs, norm := vdNormalOctaves(np)
	r := world.fromHashOf("minecraft:" + name)
	first := r.forkPositional()
	second := r.forkPositional()
	out := &vnNormal{}
	for _, o := range octs {
		seed := "octave_" + strconv.Itoa(o.index)
		f := newVDPerlin(first.fromHashOf(seed), 256)
		s := newVDPerlin(second.fromHashOf(seed), 256)
		vf := float32(norm * o.ampl)
		out.layers = append(out.layers, vnLayer{f, o.freq, vf}, vnLayer{s, o.freq * 1.0181268882175227, vf})
	}
	return out
}

// newVDNetherBiomeNoise is NormalNoise.createForLegacyNetherBiome: two
// legacy octave stacks (LegacyFbmInitializer) drawn in turn from one
// source, the second at 1.0181268882175227 times the frequency.
func newVDNetherBiomeNoise(r *vdLegacy, np vnNoiseParams) *vnNormal {
	n := np.octaves
	if n == 0 {
		n = 1
	}
	amps := make([]float64, n)
	for i := range amps {
		amps[i] = 1
		if len(np.mods) > 0 {
			amps[i] = np.mods[i]
		}
	}
	_, norm := vdNormalOctaves(np)
	vf := float32(norm * np.baseAmp)
	first := vdLegacyFbm(r, np.baseOctave, amps)
	second := vdLegacyFbm(r, np.baseOctave, amps)
	out := &vnNormal{}
	for _, l := range first {
		out.layers = append(out.layers, vnLayer{l.noise, l.freq, l.amp * vf})
	}
	for _, l := range second {
		out.layers = append(out.layers, vnLayer{l.noise, l.freq * 1.0181268882175227, l.amp * vf})
	}
	return out
}

// vdLegacyFbm is LegacyFbmInitializer.createForLegacyNetherBiome: the zero
// octave first, then the octaves below it from the highest down, a skipped
// octave consuming its 262 draws; the layers run from the lowest frequency.
func vdLegacyFbm(r *vdLegacy, firstOctave int, amps []float64) []vnLayer {
	octaves := len(amps)
	zero := -firstOctave
	levels := make([]*vnPerlin, octaves)
	z := newVDPerlin(r, 256)
	if zero >= 0 && zero < octaves && amps[zero] != 0 {
		levels[zero] = z
	}
	for i := zero - 1; i >= 0; i-- {
		if i < octaves && amps[i] != 0 {
			levels[i] = newVDPerlin(r, 256)
		} else {
			r.consume(262)
		}
	}
	factor := math.Pow(2, float64(-zero))
	value := math.Pow(2, float64(octaves-1)) / (math.Pow(2, float64(octaves)) - 1)
	var out []vnLayer
	for i, p := range levels {
		if p != nil {
			out = append(out, vnLayer{p, factor, float32(value * amps[i])})
		}
		factor *= 2
		value /= 2
	}
	return out
}

// vdSmeared is SmearedPerlinNoise: a Perlin octave whose y lattice is
// smeared in steps of fudge (the old terrain noise's y treatment).
type vdSmeared struct {
	p     *vnPerlin
	fudge float64
}

// vdFudgeEps is the float literal 1.0E-7F, widened.
var vdFudgeEps = float64(float32(1e-7))

// get is SmearedPerlinNoise.get.
func (s *vdSmeared) get(x0, y0, z0 float64) float32 {
	p := s.p
	x := vnWrap(x0) + p.offX
	y := vnWrap(y0) + p.offY
	z := vnWrap(z0) + p.offZ
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	ix, iy, iz := int(fx), int(fy), int(fz)
	rx := float32(x - fx)
	ry := y - fy
	rz := float32(z - fz)
	limit := ry
	if y0 >= 0 && y0 < ry {
		limit = y0
	}
	fudged := float32(ry - math.Floor(limit/s.fudge+vdFudgeEps)*s.fudge)
	return p.sampleAndLerp(ix, iy, iz, rx, fudged, rz, float32(ry))
}

// sampleAndLerp is PerlinNoise.sampleAndLerp: the gradients dotted with
// the (possibly fudged) offset, smoothed on the true y fraction.
func (p *vnPerlin) sampleAndLerp(ix, iy, iz int, rx, ry, rz, origRY float32) float32 {
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
	return vnLerp3(vnSmoothstep(rx), vnSmoothstep(origRY), vnSmoothstep(rz), d000, d100, d010, d110, d001, d101, d011, d111)
}

// vdSmearStack is a NoiseStack of smeared octaves.
type vdSmearStack struct {
	layers []vdSmearLayer
}

type vdSmearLayer struct {
	noise vdSmeared
	freq  float64
	amp   float32
}

func (s *vdSmearStack) get(x, y, z float64) float32 {
	var v float32
	for i := range s.layers {
		l := &s.layers[i]
		v += l.amp * l.noise.get(x*l.freq, y*l.freq, z*l.freq)
	}
	return v
}

// vdBlended is BlendedNoise (old_blended_noise) compiled: a main noise
// choosing, through clamp(main + 0.5, 0, 1), between two limit noises.
type vdBlended struct {
	minLimit, maxLimit, main *vdSmearStack
	xzMul, yMul              float64 // the limit noises' block scales
	xzMain, yMain            float64 // the main noise's
}

// newVDBlended is BlendedNoise.compileSampler for a legacy world: the
// three octave stacks drawn in turn from LegacyRandomSource(seed).
func newVDBlended(seed int64, xzScale, yScale, xzFactor, yFactor, smear float64) *vdBlended {
	r := newVDLegacy(seed)
	xzMul := 684.412 * xzScale
	yMul := 684.412 * yScale
	limitSmear := yMul * smear
	mainSmear := limitSmear / yFactor
	b := &vdBlended{xzMul: xzMul, yMul: yMul, xzMain: xzMul / xzFactor, yMain: yMul / yFactor}
	b.minLimit = vdCreateFbm(r, -15, limitSmear, float64(float32(0.99998474)))
	b.maxLimit = vdCreateFbm(r, -15, limitSmear, float64(float32(0.99998474)))
	b.main = vdCreateFbm(r, -7, mainSmear, 12.75)
	return b
}

// vdCreateFbm is BlendedNoise.createFbm.
func vdCreateFbm(r *vdLegacy, firstOctave int, smearY, value float64) *vdSmearStack {
	octaves := -firstOctave + 1
	factor := 1.0
	value /= math.Pow(2, float64(octaves)) - 1
	s := &vdSmearStack{}
	for i := octaves - 1; i >= 0; i-- {
		p := newVDPerlin(r, 256)
		s.layers = append(s.layers, vdSmearLayer{vdSmeared{p, smearY * factor}, factor, float32(value)})
		factor /= 2
		value *= 2
	}
	return s
}

// sample is the compiled sampler at a block.
func (b *vdBlended) sample(x, y, z int) float32 {
	fx, fy, fz := float64(x), float64(y), float64(z)
	main := b.main.get(fx*b.xzMain, fy*b.yMain, fz*b.xzMain)
	alpha := vcClamp(main+0.5, 0, 1)
	if alpha == 0 {
		return b.minLimit.get(fx*b.xzMul, fy*b.yMul, fz*b.xzMul)
	}
	if alpha == 1 {
		return b.maxLimit.get(fx*b.xzMul, fy*b.yMul, fz*b.xzMul)
	}
	lo := b.minLimit.get(fx*b.xzMul, fy*b.yMul, fz*b.xzMul)
	hi := b.maxLimit.get(fx*b.xzMul, fy*b.yMul, fz*b.xzMul)
	return vnLerp(alpha, lo, hi)
}

// vdSimplex is SimplexNoise (the End islands' 2-D noise).
type vdSimplex struct{ p *vnPerlin }

var (
	vdSqrt3 = math.Sqrt(3)
	vdF2    = 0.5 * (vdSqrt3 - 1)
	vdG2    = (3 - vdSqrt3) / 6
)

func (s *vdSimplex) corner(i int, x, y float64) float64 {
	t := 0.5 - x*x - y*y - 0*0
	if t < 0 {
		return 0
	}
	t *= t
	g := vnGrad[i]
	return t * t * (float64(g[0])*x + float64(g[1])*y + float64(g[2])*0)
}

// get2 is SimplexNoise.get(x, y).
func (s *vdSimplex) get2(xin0, yin0 float64) float32 {
	p := s.p
	xin := xin0 + p.offX
	yin := yin0 + p.offY
	sk := (xin + yin) * vdF2
	i := int(math.Floor(xin + sk))
	j := int(math.Floor(yin + sk))
	t := float64(i+j) * vdG2
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1 := x0 - float64(i1) + vdG2
	y1 := y0 - float64(j1) + vdG2
	x2 := x0 - 1 + 2*vdG2
	y2 := y0 - 1 + 2*vdG2
	ii, jj := i&0xFF, j&0xFF
	gi0 := p.permute(ii+p.permute(jj)) % 12
	gi1 := p.permute(ii+i1+p.permute(jj+j1)) % 12
	gi2 := p.permute(ii+1+p.permute(jj+1)) % 12
	n0 := s.corner(gi0, x0, y0)
	n1 := s.corner(gi1, x1, y1)
	n2 := s.corner(gi2, x2, y2)
	return float32(70 * (n0 + n1 + n2))
}
