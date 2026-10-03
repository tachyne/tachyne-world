package worldgen

import (
	"math"
	"strconv"
	"sync"
)

// vtState is 26.3's RandomState for one dimension's noise settings and a
// world seed: the positional random every noise is seeded from (the
// settings' algorithm, xoroshiro or legacy), the noises made so far, the
// positional factories (aquifer, ore, surface…), and the density-function
// compiler. Safe for concurrent use once built.
type vtState struct {
	seed   int64
	legacy bool
	random vtPositional

	mu        sync.Mutex // factories
	noiseMu   sync.Mutex // noises
	compMu    sync.Mutex // comp
	noises    map[string]*vtStack
	factories map[string]vtPositional
	comp      *vdCompiler
}

func newVTState(seed int64, legacy bool) *vtState {
	rs := &vtState{
		seed:      seed,
		legacy:    legacy,
		random:    vtNewRandom(legacy, seed).forkPositional(),
		noises:    map[string]*vtStack{},
		factories: map[string]vtPositional{},
	}
	rs.comp = newVDCompiler(rs)
	return rs
}

// noise is CompileContext.createNoiseSampler / getOrCreateNoise.
func (rs *vtState) noise(name string) (*vtStack, error) {
	rs.noiseMu.Lock()
	defer rs.noiseMu.Unlock()
	if n, ok := rs.noises[name]; ok {
		return n, nil
	}
	np, err := vtNoiseParams(name)
	if err != nil {
		return nil, err
	}
	var n *vtStack
	switch name {
	case "minecraft:nether/temperature":
		n = vtLegacyNetherNoise(newVTLegacy(rs.seed), np)
	case "minecraft:nether/vegetation":
		n = vtLegacyNetherNoise(newVTLegacy(rs.seed+1), np)
	default:
		n = vtNormalNoise(rs.random.fromHashOf(name), np)
	}
	rs.noises[name] = n
	return n, nil
}

// mustNoise is noise for the names the engine itself asks for (the
// material system's noises), which the data always has.
func (rs *vtState) mustNoise(name string) *vtStack {
	n, err := rs.noise(name)
	if err != nil {
		panic(err)
	}
	return n
}

// createRandom is CompileContext.createRandom.
func (rs *vtState) createRandom(name string) vtRandom {
	if rs.legacy && name == "minecraft:terrain" {
		return newVTLegacy(rs.seed)
	}
	return rs.random.fromHashOf(name)
}

// randomFactory is getOrCreateRandomFactory.
func (rs *vtState) randomFactory(name string) vtPositional {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if f, ok := rs.factories[name]; ok {
		return f
	}
	f := rs.random.fromHashOf(name).forkPositional()
	rs.factories[name] = f
	return f
}

// compileNamed compiles a named density function (a reference).
func (rs *vtState) compileNamed(name string) (vdSampler, error) {
	return rs.compile(&vdFn{kind: "ref", name: name, key: strconv.Quote(name)})
}

// compile is getSampler. Compilation is serialised, as vanilla's is.
func (rs *vtState) compile(f *vdFn) (vdSampler, error) {
	smp, _, err := rs.compileAxes(f)
	return smp, err
}

// compileAxes is compile with the function's domain axes (vdAxisX|Y|Z).
func (rs *vtState) compileAxes(f *vdFn) (vdSampler, int, error) {
	rs.compMu.Lock()
	defer rs.compMu.Unlock()
	g, err := rs.comp.optimize(f)
	if err != nil {
		return nil, 0, err
	}
	smp, err := rs.comp.build(g)
	return smp, vdDomain(g), err
}

// vtLegacyNetherNoise is NormalNoise.createForLegacyNetherBiome.
func vtLegacyNetherNoise(r *vtLegacy, np vnNoiseParams) *vtStack {
	amps := np.mods
	n := np.octaves
	if n == 0 {
		n = 1
	}
	if len(amps) == 0 {
		amps = make([]float64, n)
		for i := range amps {
			amps[i] = 1
		}
	}
	first := vtLegacyFbm(r, np.baseOctave, amps)
	second := vtLegacyFbm(r, np.baseOctave, amps)
	// The normalisation of the parameters' octaves (NormalNoise's factor).
	norm := vtNormalization(np)
	vf := float32(norm * np.baseAmp)
	out := &vtStack{}
	for _, l := range first.layers {
		out.layers = append(out.layers, vtLayer{l.noise, l.freq * 1.0, l.amp * vf})
	}
	for _, l := range second.layers {
		out.layers = append(out.layers, vtLayer{l.noise, l.freq * 1.0181268882175227, l.amp * vf})
	}
	return out
}

// vtNormalization is NormalNoise's normalizationFactor for parameters.
func vtNormalization(np vnNoiseParams) float64 {
	n := np.octaves
	if n == 0 {
		n = 1
	}
	amp := np.baseAmp * (math.Pow(0.5, -float64(n-1)) / (math.Pow(0.5, -float64(n)) - 1))
	target, variance := 0.0, 0.0
	for i := 0; i < n; i++ {
		m := 1.0
		if len(np.mods) > 0 {
			m = np.mods[i]
		}
		if m != 0 {
			a := math.Abs(amp * m)
			target += a
			d := 0.2702247831245211 * a
			variance += d * d
		}
		amp *= 0.5
	}
	dev := math.Sqrt(variance)
	if dev == 0 {
		return 0
	}
	return (target * 0.3333333333333333) / (dev * math.Sqrt(2))
}

// vtLegacyFbm is LegacyFbmInitializer.createForLegacyNetherBiome.
func vtLegacyFbm(r *vtLegacy, firstOctave int, amps []float64) *vtStack {
	octaves := len(amps)
	zero := -firstOctave
	levels := make([]*vnPerlin, octaves)
	zeroOctave := newVNPerlin(r)
	if zero >= 0 && zero < octaves && amps[zero] != 0 {
		levels[zero] = zeroOctave
	}
	for i := zero - 1; i >= 0; i-- {
		if i < octaves && amps[i] != 0 {
			levels[i] = newVNPerlin(r)
		} else {
			for k := 0; k < 262; k++ {
				r.next(32)
			}
		}
	}
	factor := math.Pow(2, -float64(zero))
	valueFactor := math.Pow(2, float64(octaves-1)) / (math.Pow(2, float64(octaves)) - 1)
	s := &vtStack{}
	for i, p := range levels {
		if p != nil {
			s.layers = append(s.layers, vtLayer{p, factor, float32(valueFactor * amps[i])})
		}
		factor *= 2
		valueFactor /= 2
	}
	return s
}

// vtSimplex is SimplexNoise (the biome temperature noises, the End islands).
type vtSimplex struct {
	perms            [256]uint8
	offX, offY, offZ float64
}

// newVTSimplex is new SimplexNoise(random, true): the offsets are drawn and
// discarded.
func newVTSimplex(r vnPerlinSource) *vtSimplex {
	s := &vtSimplex{}
	r.nextDouble()
	r.nextDouble()
	r.nextDouble()
	for i := range s.perms {
		s.perms[i] = uint8(i)
	}
	for i := 0; i < 256; i++ {
		o := int(r.nextInt(int32(256 - i)))
		s.perms[i], s.perms[o+i] = s.perms[o+i], s.perms[i]
	}
	return s
}

func (s *vtSimplex) permute(x int) int { return int(s.perms[x&0xFF]) }

var (
	vtSqrt3 = math.Sqrt(3)
	vtF2    = 0.5 * (vtSqrt3 - 1)
	vtG2    = (3 - vtSqrt3) / 6
)

func vtCorner(index int, x, y, z, base float64) float64 {
	t := base - x*x - y*y - z*z
	if t < 0 {
		return 0
	}
	t *= t
	g := vnGrad[index]
	return t * t * (float64(g[0])*x + float64(g[1])*y + float64(g[2])*z)
}

// get2 is SimplexNoise.get(x, y).
func (s *vtSimplex) get2(xi, yi float64) float32 {
	xin, yin := xi+s.offX, yi+s.offY
	sk := (xin + yin) * vtF2
	i := vtMthFloor(xin + sk)
	j := vtMthFloor(yin + sk)
	t := float64(i+j) * vtG2
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1 := x0 - float64(i1) + vtG2
	y1 := y0 - float64(j1) + vtG2
	x2 := x0 - 1 + 2*vtG2
	y2 := y0 - 1 + 2*vtG2
	ii, jj := i&0xFF, j&0xFF
	gi0 := s.permute(ii+s.permute(jj)) % 12
	gi1 := s.permute(ii+i1+s.permute(jj+j1)) % 12
	gi2 := s.permute(ii+1+s.permute(jj+1)) % 12
	n0 := vtCorner(gi0, x0, y0, 0, 0.5)
	n1 := vtCorner(gi1, x1, y1, 0, 0.5)
	n2 := vtCorner(gi2, x2, y2, 0, 0.5)
	return float32(70 * (n0 + n1 + n2))
}
