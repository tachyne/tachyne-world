package worldgen

import "math"

// The vanilla climate the biome source reads (vanillabiomesrc.go), for
// when the terrain core hands it no VanillaClimate: the noise router's six
// climate density functions for the overworld (normal, large_biomes and
// amplified noise settings), the Nether's two legacy climate noises, and
// the End's island erosion. Each is the server's arithmetic — float32
// where the 26.3 density functions are — so the same seed samples the
// same quantized TargetPoint.
//
//	temperature  noise temperature (_large), shifted, xz scale 1/4
//	vegetation   noise vegetation (_large), shifted, xz scale 1/4
//	continents   noise continentalness (_large), shifted
//	erosion      noise erosion (_large), shifted
//	ridges       noise ridge, shifted
//	depth        y gradient 1.5 at -64 to -1.5 at 320, plus overworld/offset
//	             (-0.50375 plus the offset spline over continents, erosion
//	             and ridges_folded; vanillaoffset_gen.go)
//
// "Shifted" is shift_x/shift_z: the offset noise at a quarter scale, times
// four, added to the sample position.

// vbSpline coordinates.
const (
	vbcContinents = iota
	vbcErosion
	vbcRidgesFolded
)

// vbSpline is a CubicSpline: a constant (vals nil) or a multipoint.
type vbSpline struct {
	value float32
	coord int
	locs  []float32
	ders  []float32
	vals  []*vbSpline
}

func vbK(v float32) *vbSpline { return &vbSpline{value: v} }

func vbLinearExtend(input float32, locs []float32, value float32, ders []float32, i int) float32 {
	if d := ders[i]; d != 0 {
		return value + d*(input-locs[i])
	}
	return value
}

// sample is CubicSpline.Multipoint.sample.
func (s *vbSpline) sample(in *[3]float32) float32 {
	if s.vals == nil {
		return s.value
	}
	input := in[s.coord]
	start := len(s.locs)
	for i, l := range s.locs {
		if input < l {
			start = i
			break
		}
	}
	start--
	last := len(s.locs) - 1
	switch {
	case start < 0:
		return vbLinearExtend(input, s.locs, s.vals[0].sample(in), s.ders, 0)
	case start == last:
		return vbLinearExtend(input, s.locs, s.vals[last].sample(in), s.ders, last)
	}
	x1, x2 := s.locs[start], s.locs[start+1]
	t := (input - x1) / (x2 - x1)
	d1, d2 := s.ders[start], s.ders[start+1]
	y1 := s.vals[start].sample(in)
	y2 := s.vals[start+1].sample(in)
	a := d1*(x2-x1) - (y2 - y1)
	b := -d2*(x2-x1) + (y2 - y1)
	return vnLerp(t, y1, y2) + t*(1-t)*vnLerp(t, a, b)
}

// vbOffset is overworld/offset (unblended): base plus the spline.
type vbOffset struct {
	base   float32
	spline *vbSpline
}

// vbClimateNoise is a worldgen/noise file the climate reads.
var vbClimateNoise = map[string]vnNoiseParams{
	"offset":                {baseOctave: -3, baseAmp: 0.9381732587751005, octaves: 4, mods: []float64{1, 1, 1, 0}},
	"temperature":           {baseOctave: -10, baseAmp: 1.2453007926713473, octaves: 6, mods: []float64{1.5, 0, 1, 0, 0, 0}},
	"vegetation":            {baseOctave: -8, baseAmp: 0.9494731054427978, octaves: 6, mods: []float64{1, 1, 0, 0, 0, 0}},
	"continentalness":       {baseOctave: -9, baseAmp: 0.8880832896205223, octaves: 9, mods: []float64{1, 1, 2, 2, 2, 1, 1, 1, 1}},
	"erosion":               {baseOctave: -9, baseAmp: 1.063180125160734, octaves: 5, mods: []float64{1, 1, 0, 1, 1}},
	"ridge":                 {baseOctave: -7, baseAmp: 0.9147152149950137, octaves: 6, mods: []float64{1, 2, 1, 0, 0, 0}},
	"temperature_large":     {baseOctave: -12, baseAmp: 1.2453007926713473, octaves: 6, mods: []float64{1.5, 0, 1, 0, 0, 0}},
	"vegetation_large":      {baseOctave: -10, baseAmp: 0.9494731054427978, octaves: 6, mods: []float64{1, 1, 0, 0, 0, 0}},
	"continentalness_large": {baseOctave: -11, baseAmp: 0.8880832896205223, octaves: 9, mods: []float64{1, 1, 2, 2, 2, 1, 1, 1, 1}},
	"erosion_large":         {baseOctave: -11, baseAmp: 1.063180125160734, octaves: 5, mods: []float64{1, 1, 0, 1, 1}},
}

// vbOverworldClimate is the overworld router's Climate.Sampler.
type vbOverworldClimate struct {
	shift, temp, veg, cont, ero, ridge *vnNormal
	offset                             *vbOffset
}

// NewVanillaOverworldClimate is the overworld's climate for a seed and
// preset: the large_biomes preset reads the *_large noises, amplified its
// own offset spline; every other preset the normal router.
func NewVanillaOverworldClimate(seed int64, p WorldPreset) VanillaClimate {
	w := vnWorldPositional(seed)
	n := func(name string) *vnNormal { return newVNNormal(w, "minecraft:"+name, vbClimateNoise[name]) }
	suffix := ""
	if p == PresetLargeBiomes {
		suffix = "_large"
	}
	c := &vbOverworldClimate{
		shift: n("offset"), temp: n("temperature" + suffix), veg: n("vegetation" + suffix),
		cont: n("continentalness" + suffix), ero: n("erosion" + suffix), ridge: n("ridge"),
		offset: &vbOffsetNormal,
	}
	if p == PresetAmplified {
		c.offset = &vbOffsetAmplified
	}
	return c
}

// column is the climate's two-dimensional part at a quart column, and the
// offset depth adds to its y gradient.
func (c *vbOverworldClimate) column(qx, qz int) (p ClimatePoint, offset float32) {
	x, z := qx<<2, qz<<2
	sx := c.shift.get(float64(x)*0.25, 0, float64(z)*0.25) * 4
	sz := c.shift.get(float64(z)*0.25, float64(x)*0.25, 0) * 4
	nx := float64(x)*0.25 + float64(sx)
	nz := float64(z)*0.25 + float64(sz)
	cont := c.cont.get(nx, 0, nz)
	ero := c.ero.get(nx, 0, nz)
	ridges := c.ridge.get(nx, 0, nz)
	in := [3]float32{cont, ero, vfFold(ridges)}
	offset = c.offset.base + c.offset.spline.sample(&in)
	return ClimatePoint{
		Temperature:     QuantizeClimate(c.temp.get(nx, 0, nz)),
		Humidity:        QuantizeClimate(c.veg.get(nx, 0, nz)),
		Continentalness: QuantizeClimate(cont),
		Erosion:         QuantizeClimate(ero),
		Weirdness:       QuantizeClimate(ridges),
	}, offset
}

// vbDepth is overworld/depth at a quart height: the y gradient plus offset.
func vbDepth(qy int, offset float32) int64 {
	return QuantizeClimate(vcGradient(qy<<2, -64, 320, 1.5, -1.5) + offset)
}

// Sample is Climate.Sampler.sample at a quart.
func (c *vbOverworldClimate) Sample(qx, qy, qz int) ClimatePoint {
	p, offset := c.column(qx, qz)
	p.Depth = vbDepth(qy, offset)
	return p
}

// SampleColumn is Sample over a quart column.
func (c *vbOverworldClimate) SampleColumn(qx, qz, qy0 int, out []ClimatePoint) {
	p, offset := c.column(qx, qz)
	for i := range out {
		p.Depth = vbDepth(qy0+i, offset)
		out[i] = p
	}
}

// vbConstClimate is a router whose climate is all constants (the caves and
// floating_islands noise settings: every value 0).
type vbConstClimate ClimatePoint

func (c vbConstClimate) Sample(qx, qy, qz int) ClimatePoint { return ClimatePoint(c) }

// ---- the legacy random and the Nether ----

// vbLegacyRandom is LegacyRandomSource (java.util.Random's generator).
type vbLegacyRandom struct{ seed int64 }

func newVBLegacyRandom(seed int64) *vbLegacyRandom {
	return &vbLegacyRandom{(seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *vbLegacyRandom) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

// nextInt is BitRandomSource.nextInt(bound).
func (r *vbLegacyRandom) nextInt(bound int32) int32 {
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

func (r *vbLegacyRandom) nextDouble() float64 {
	upper := int64(r.next(26))
	lower := int64(r.next(27))
	return float64(upper<<27+lower) * vnDoubleUnit
}

// consumeCount is RandomSource.consumeCount: n nextInt() calls.
func (r *vbLegacyRandom) consumeCount(n int) {
	for i := 0; i < n; i++ {
		r.next(32)
	}
}

// vbNewPerlin is new PerlinNoise(random) (GradientNoise's constructor)
// from a legacy random, with the offsets scaled by scale (256, or 0 for a
// noise that discards them).
func vbNewPerlin(r *vbLegacyRandom, scale float64) *vnPerlin {
	p := &vnPerlin{}
	p.offX = r.nextDouble() * scale
	p.offY = r.nextDouble() * scale
	p.offZ = r.nextDouble() * scale
	for i := range p.perms {
		p.perms[i] = uint8(i)
	}
	for i := 0; i < 256; i++ {
		o := int(r.nextInt(int32(256 - i)))
		p.perms[i], p.perms[o+i] = p.perms[o+i], p.perms[i]
	}
	return p
}

// vbLegacyNetherStack is LegacyFbmInitializer.createForLegacyNetherBiome
// for amplitudes all 1: the octaves below zero built from the top down,
// after a zero octave that is made and dropped, with 262 draws skipped
// for each octave outside the stack.
func vbLegacyNetherStack(r *vbLegacyRandom, firstOctave, octaves int) []vnLayer {
	zero := -firstOctave
	levels := make([]*vnPerlin, octaves)
	z := vbNewPerlin(r, 256)
	if zero >= 0 && zero < octaves {
		levels[zero] = z
	}
	for i := zero - 1; i >= 0; i-- {
		if i < octaves {
			levels[i] = vbNewPerlin(r, 256)
		} else {
			r.consumeCount(262)
		}
	}
	factor := math.Pow(2, float64(-zero))
	value := math.Pow(2, float64(octaves-1)) / (math.Pow(2, float64(octaves)) - 1)
	var out []vnLayer
	for _, n := range levels {
		if n != nil {
			out = append(out, vnLayer{n, factor, float32(value)})
		}
		factor *= 2
		value /= 2
	}
	return out
}

// vbLegacyNetherNoise is NormalNoise.createForLegacyNetherBiome for a
// normalised noise with no amplitude modifiers.
func vbLegacyNetherNoise(r *vbLegacyRandom, np vnNoiseParams) *vnNormal {
	// The normalisation factor NormalNoise computes from its octaves.
	amp := np.baseAmp * (math.Pow(0.5, -float64(np.octaves-1)) / (math.Pow(0.5, -float64(np.octaves)) - 1))
	target, variance := 0.0, 0.0
	for i := 0; i < np.octaves; i++ {
		target += math.Abs(amp)
		d := 0.2702247831245211 * math.Abs(amp)
		variance += d * d
		amp *= 0.5
	}
	norm := (target * 0.3333333333333333) / (math.Sqrt(variance) * math.Sqrt(2))
	vf := float32(norm * np.baseAmp)
	first := vbLegacyNetherStack(r, np.baseOctave, np.octaves)
	second := vbLegacyNetherStack(r, np.baseOctave, np.octaves)
	out := &vnNormal{}
	for _, l := range first {
		out.layers = append(out.layers, vnLayer{l.noise, l.freq * 1.0, l.amp * vf})
	}
	for _, l := range second {
		out.layers = append(out.layers, vnLayer{l.noise, l.freq * 1.0181268882175227, l.amp * vf})
	}
	return out
}

// vbNetherClimate is the Nether router's Climate.Sampler: temperature and
// vegetation from nether/temperature and nether/vegetation seeded by the
// legacy random at seed and seed+1, every other value 0.
type vbNetherClimate struct{ temp, veg *vnNormal }

// NewVanillaNetherClimate is the Nether's climate for a seed.
func NewVanillaNetherClimate(seed int64) VanillaClimate {
	np := vnNoiseParams{baseOctave: -7, baseAmp: 0.9494731054427981, octaves: 2}
	return &vbNetherClimate{
		temp: vbLegacyNetherNoise(newVBLegacyRandom(seed), np),
		veg:  vbLegacyNetherNoise(newVBLegacyRandom(seed+1), np),
	}
}

// SampleColumn is Sample over a quart column (the Nether's climate has no y).
func (c *vbNetherClimate) SampleColumn(qx, qz, qy0 int, out []ClimatePoint) {
	p := c.Sample(qx, qy0, qz)
	for i := range out {
		out[i] = p
	}
}

func (c *vbNetherClimate) Sample(qx, qy, qz int) ClimatePoint {
	x, z := float64(qx<<2)*0.25, float64(qz<<2)*0.25
	return ClimatePoint{
		Temperature: QuantizeClimate(c.temp.get(x, 0, z)),
		Humidity:    QuantizeClimate(c.veg.get(x, 0, z)),
	}
}

// ---- the End ----

// vbSimplex is SimplexNoise (2-D).
type vbSimplex struct {
	perms      [256]uint8
	offX, offY float64
}

var (
	vbF2 = 0.5 * (math.Sqrt(3) - 1)
	vbG2 = (3 - math.Sqrt(3)) / 6
)

func (s *vbSimplex) permute(x int) int { return int(s.perms[x&0xFF]) }

func vbCorner(i int, x, y, base float64) float64 {
	t := base - x*x - y*y
	if t < 0 {
		return 0
	}
	t *= t
	g := vnGrad[i]
	return t * t * (float64(g[0])*x + float64(g[1])*y)
}

// get is SimplexNoise.get(x, y).
func (s *vbSimplex) get(xin, yin float64) float32 {
	xin += s.offX
	yin += s.offY
	sk := (xin + yin) * vbF2
	i := int(math.Floor(xin + sk))
	j := int(math.Floor(yin + sk))
	t := float64(i+j) * vbG2
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1 := x0 - float64(i1) + vbG2
	y1 := y0 - float64(j1) + vbG2
	x2 := x0 - 1 + 2*vbG2
	y2 := y0 - 1 + 2*vbG2
	ii, jj := i&0xFF, j&0xFF
	g0 := s.permute(ii+s.permute(jj)) % 12
	g1 := s.permute(ii+i1+s.permute(jj+j1)) % 12
	g2 := s.permute(ii+1+s.permute(jj+1)) % 12
	return float32(70 * (vbCorner(g0, x0, y0, 0.5) + vbCorner(g1, x1, y1, 0.5) + vbCorner(g2, x2, y2, 0.5)))
}

// vbEndIslands is the End router's erosion beyond the main island:
// end_outer_islands (EndIslandFunction), the island noise seeded by the
// legacy random at the world seed after 17292 draws.
type vbEndIslands struct{ noise *vbSimplex }

func newVBEndIslands(seed int64) *vbEndIslands {
	r := newVBLegacyRandom(seed)
	r.consumeCount(17292)
	p := vbNewPerlin(r, 0) // GradientNoise's constructor, offsets discarded
	return &vbEndIslands{&vbSimplex{perms: p.perms}}
}

// height is EndIslandFunction.getHeightValue.
func (e *vbEndIslands) height(sx, sz int) float32 {
	cx, cz := sx/2, sz/2
	subX, subZ := sx%2, sz%2
	doffs := float32(-100)
	for xo := -12; xo <= 12; xo++ {
		for zo := -12; zo <= 12; zo++ {
			tx, tz := int64(cx+xo), int64(cz+zo)
			if tx*tx+tz*tz > 4096 && e.noise.get(float64(tx), float64(tz)) < -0.9 {
				size := float32(math.Mod(float64(vbAbs32(float32(tx))*3439+vbAbs32(float32(tz))*147), 13)) + 9
				xd := float32(subX - xo*2)
				zd := float32(subZ - zo*2)
				d := 100 - float32(math.Sqrt(float64(xd*xd+zd*zd)))*size
				d = vcClamp(d, -100, 80)
				if d > doffs {
					doffs = d
				}
			}
		}
	}
	return doffs
}

// erosion is end/islands at a block outside the main island's reach (the
// main island's term is at its floor, -0.84375, which the outer islands
// never fall below): the outer islands' height value, (h-8)/128.
func (e *vbEndIslands) erosion(x, z int) float32 {
	return (e.height(x/8, z/8) - 8) / 128
}

func vbAbs32(v float32) float32 { return float32(math.Abs(float64(v))) }
