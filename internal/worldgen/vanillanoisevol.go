package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// The volume side of vanilla's noise, for the vanilla generator's density
// functions (vanilladensity.go). 26.3 samples a density function two ways:
// at one block (sampleValue) and over a whole DensityVolume at once
// (sampleVolume), and the two do not round alike — a noise's volume pass
// scales each octave's coordinates as blockX * (scale * frequency) where the
// point pass computes (blockX * scale) * frequency. The chunk fill samples
// final_density over the chunk's volume and the aquifer, the climate and the
// material rules sample points, so both passes are kept here, each as
// vanilla writes it: PerlinNoise.addToVolume, SmearedPerlinNoise (the
// blended noise's octaves), NoiseStack's layer walk, and BlendedNoise.

// vtVolume is DensityVolume: sizes, the first block, and the step between
// samples on each axis. Index order is y fastest, then x, then z.
type vtVolume struct {
	sx, sy, sz          int
	minX, minY, minZ    int
	stepX, stepY, stepZ int
}

func vtVol(sx, sy, sz, minX, minY, minZ int) vtVolume {
	return vtVolume{sx, sy, sz, minX, minY, minZ, 1, 1, 1}
}

func (v vtVolume) size() int                 { return v.sx * v.sy * v.sz }
func (v vtVolume) index(x, y, z int) int     { return y + (x+z*v.sx)*v.sy }
func (v vtVolume) blockX(i int) int          { return v.minX + i*v.stepX }
func (v vtVolume) blockY(i int) int          { return v.minY + i*v.stepY }
func (v vtVolume) blockZ(i int) int          { return v.minZ + i*v.stepZ }
func (v vtVolume) maxX() int                 { return v.minX + v.sx*v.stepX - 1 }
func (v vtVolume) maxY() int                 { return v.minY + v.sy*v.stepY - 1 }
func (v vtVolume) maxZ() int                 { return v.minZ + v.sz*v.stepZ - 1 }
func (v vtVolume) unitStep() bool            { return v.stepX == 1 && v.stepY == 1 && v.stepZ == 1 }
func (v vtVolume) equal(o vtVolume) bool     { return v == o }
func (v vtVolume) contains(x, y, z int) bool { return v.indexOf(x, y, z) >= 0 }
func floorModInt(a, b int) int               { return ((a % b) + b) % b }
func (v vtVolume) relOK(rx, ry, rz int) bool { return rx >= 0 && ry >= 0 && rz >= 0 }

// indexOf is indexOfBlock: the sample at a block, -1 if none.
func (v vtVolume) indexOf(x, y, z int) int {
	rx, ry, rz := x-v.minX, y-v.minY, z-v.minZ
	if v.unitStep() {
		if v.relOK(rx, ry, rz) && rx < v.sx && ry < v.sy && rz < v.sz {
			return v.index(rx, ry, rz)
		}
		return -1
	}
	if v.relOK(rx, ry, rz) && rx < v.sx*v.stepX && ry < v.sy*v.stepY && rz < v.sz*v.stepZ &&
		floorModInt(rx, v.stepX) == 0 && floorModInt(ry, v.stepY) == 0 && floorModInt(rz, v.stepZ) == 0 {
		return v.index(floorDiv(rx, v.stepX), floorDiv(ry, v.stepY), floorDiv(rz, v.stepZ))
	}
	return -1
}

// vtOctave is one noise layer's source: PerlinNoise or SmearedPerlinNoise.
type vtOctave interface {
	get(x, y, z float64) float32
	addToVolume(buf []float32, v vtVolume, xzScale, yScale float64, amp float32)
}

// vtMthFloor is Mth.floor(double).
func vtMthFloor(v float64) int { return int(math.Floor(v)) }

// addToVolume is PerlinNoise.addToVolume.
func (p *vnPerlin) addToVolume(buf []float32, v vtVolume, xzScale, yScale float64, amp float32) {
	var d000, d100, d010, d110, d001, d101, d011, d111 float32
	var g000, g100, g010, g110, g001, g101, g011, g111 float32
	i := 0
	for iz := 0; iz < v.sz; iz++ {
		z := vnWrap(float64(v.blockZ(iz))*xzScale) + p.offZ
		fz := vtMthFloor(z)
		rz := float32(z - float64(fz))
		az := vnSmoothstep(rz)
		for ix := 0; ix < v.sx; ix++ {
			x := vnWrap(float64(v.blockX(ix))*xzScale) + p.offX
			fx := vtMthFloor(x)
			rx := float32(x - float64(fx))
			x0 := p.permute(fx)
			x1 := p.permute(fx + 1)
			ax := vnSmoothstep(rx)
			lastFY := math.MinInt
			for iy := 0; iy < v.sy; iy++ {
				y := vnWrap(float64(v.blockY(iy))*yScale) + p.offY
				fy := vtMthFloor(y)
				ry := float32(y - float64(fy))
				ay := vnSmoothstep(ry)
				if lastFY != fy {
					xy00 := p.permute(x0 + fy)
					xy01 := p.permute(x0 + fy + 1)
					xy10 := p.permute(x1 + fy)
					xy11 := p.permute(x1 + fy + 1)
					d000, g000 = vtDotXz(p.permute(xy00+fz), rx, rz)
					d100, g100 = vtDotXz(p.permute(xy10+fz), rx-1, rz)
					d010, g010 = vtDotXz(p.permute(xy01+fz), rx, rz)
					d110, g110 = vtDotXz(p.permute(xy11+fz), rx-1, rz)
					d001, g001 = vtDotXz(p.permute(xy00+fz+1), rx, rz-1)
					d101, g101 = vtDotXz(p.permute(xy10+fz+1), rx-1, rz-1)
					d011, g011 = vtDotXz(p.permute(xy01+fz+1), rx, rz-1)
					d111, g111 = vtDotXz(p.permute(xy11+fz+1), rx-1, rz-1)
					lastFY = fy
				}
				buf[i] += amp * vnLerp3(ax, ay, az,
					d000+g000*ry, d100+g100*ry, d010+g010*(ry-1), d110+g110*(ry-1),
					d001+g001*ry, d101+g101*ry, d011+g011*(ry-1), d111+g111*(ry-1))
				i++
			}
		}
	}
}

// vtDotXz is Gradient.dotXz at hash's gradient, and its y component.
func vtDotXz(hash int, x, z float32) (float32, float32) {
	g := vnGrad[hash&15]
	return g[0]*x + g[2]*z, g[1]
}

// vtSmeared is SmearedPerlinNoise: the blended noise's octave, whose y
// sampling is quantized to a fudge scale.
type vtSmeared struct {
	p     *vnPerlin
	fudge float64
}

func (s *vtSmeared) fudgeY(origY, relY float64) float64 {
	limit := relY
	if origY >= 0 && origY < relY {
		limit = origY
	}
	return math.Floor(limit/s.fudge+float64(float32(1.0e-7))) * s.fudge
}

func (s *vtSmeared) get(ox, oy, oz float64) float32 {
	p := s.p
	x := vnWrap(ox) + p.offX
	y := vnWrap(oy) + p.offY
	z := vnWrap(oz) + p.offZ
	fx, fy, fz := vtMthFloor(x), vtMthFloor(y), vtMthFloor(z)
	rx := float32(x - float64(fx))
	ry := y - float64(fy)
	rz := float32(z - float64(fz))
	fudged := float32(ry - s.fudgeY(oy, ry))
	return p.sampleAndLerp(fx, fy, fz, rx, fudged, rz, float32(ry))
}

func (s *vtSmeared) addToVolume(buf []float32, v vtVolume, xzScale, yScale float64, amp float32) {
	p := s.p
	var d000, d100, d010, d110, d001, d101, d011, d111 float32
	var g000, g100, g010, g110, g001, g101, g011, g111 float32
	i := 0
	for iz := 0; iz < v.sz; iz++ {
		z := vnWrap(float64(v.blockZ(iz))*xzScale) + p.offZ
		fz := vtMthFloor(z)
		rz := float32(z - float64(fz))
		az := vnSmoothstep(rz)
		for ix := 0; ix < v.sx; ix++ {
			x := vnWrap(float64(v.blockX(ix))*xzScale) + p.offX
			fx := vtMthFloor(x)
			rx := float32(x - float64(fx))
			x0 := p.permute(fx)
			x1 := p.permute(fx + 1)
			ax := vnSmoothstep(rx)
			lastFY := math.MinInt
			for iy := 0; iy < v.sy; iy++ {
				oy := float64(v.blockY(iy)) * yScale
				y := vnWrap(oy) + p.offY
				fy := vtMthFloor(y)
				ry := y - float64(fy)
				ay := vnSmoothstep(float32(ry))
				if lastFY != fy {
					xy00 := p.permute(x0 + fy)
					xy01 := p.permute(x0 + fy + 1)
					xy10 := p.permute(x1 + fy)
					xy11 := p.permute(x1 + fy + 1)
					d000, g000 = vtDotXz(p.permute(xy00+fz), rx, rz)
					d100, g100 = vtDotXz(p.permute(xy10+fz), rx-1, rz)
					d010, g010 = vtDotXz(p.permute(xy01+fz), rx, rz)
					d110, g110 = vtDotXz(p.permute(xy11+fz), rx-1, rz)
					d001, g001 = vtDotXz(p.permute(xy00+fz+1), rx, rz-1)
					d101, g101 = vtDotXz(p.permute(xy10+fz+1), rx-1, rz-1)
					d011, g011 = vtDotXz(p.permute(xy01+fz+1), rx, rz-1)
					d111, g111 = vtDotXz(p.permute(xy11+fz+1), rx-1, rz-1)
					lastFY = fy
				}
				fr := float32(ry - s.fudgeY(oy, ry))
				buf[i] += amp * vnLerp3(ax, ay, az,
					d000+g000*fr, d100+g100*fr, d010+g010*(fr-1), d110+g110*(fr-1),
					d001+g001*fr, d101+g101*fr, d011+g011*(fr-1), d111+g111*(fr-1))
				i++
			}
		}
	}
}

// sampleAndLerp is PerlinNoise.sampleAndLerp (the smeared octave passes a
// fudged y for the gradients and the true one for the y fade).
func (p *vnPerlin) sampleAndLerp(x, y, z int, rx, ry, rz, origRY float32) float32 {
	x0 := p.permute(x)
	x1 := p.permute(x + 1)
	xy00 := p.permute(x0 + y)
	xy01 := p.permute(x0 + y + 1)
	xy10 := p.permute(x1 + y)
	xy11 := p.permute(x1 + y + 1)
	d000 := vnGradDot(p.permute(xy00+z), rx, ry, rz)
	d100 := vnGradDot(p.permute(xy10+z), rx-1, ry, rz)
	d010 := vnGradDot(p.permute(xy01+z), rx, ry-1, rz)
	d110 := vnGradDot(p.permute(xy11+z), rx-1, ry-1, rz)
	d001 := vnGradDot(p.permute(xy00+z+1), rx, ry, rz-1)
	d101 := vnGradDot(p.permute(xy10+z+1), rx-1, ry, rz-1)
	d011 := vnGradDot(p.permute(xy01+z+1), rx, ry-1, rz-1)
	d111 := vnGradDot(p.permute(xy11+z+1), rx-1, ry-1, rz-1)
	return vnLerp3(vnSmoothstep(rx), vnSmoothstep(origRY), vnSmoothstep(rz), d000, d100, d010, d110, d001, d101, d011, d111)
}

// vtLayer is one NoiseStack layer.
type vtLayer struct {
	noise vtOctave
	freq  float64
	amp   float32
}

// vtStack is NoiseStack: the layers summed in order.
type vtStack struct{ layers []vtLayer }

func (s *vtStack) get(x, y, z float64) float32 {
	var v float32
	for _, l := range s.layers {
		v += l.amp * l.noise.get(x*l.freq, y*l.freq, z*l.freq)
	}
	return v
}

func (s *vtStack) addToVolume(buf []float32, v vtVolume, xzScale, yScale float64, amp float32) {
	for _, l := range s.layers {
		l.noise.addToVolume(buf, v, xzScale*l.freq, yScale*l.freq, amp*l.amp)
	}
}

// vtNoiseDef is a worldgen/noise file.
type vtNoiseDef struct {
	FirstOctave int       `json:"firstOctave"`
	Amplitudes  []float64 `json:"amplitudes"`
}

// vtNoiseParams reads a worldgen/noise entry ("minecraft:cave_cheese") from
// the embedded data as NormalNoise.Parameters.
func vtNoiseParams(name string) (vnNoiseParams, error) {
	key := "noise/" + vtStripNS(name)
	raw, ok := vanillaWorldgenData[key]
	if !ok {
		return vnNoiseParams{}, fmt.Errorf("vanilla noise %q not in the data", name)
	}
	var d struct {
		BaseOctave  int             `json:"base_octave"`
		BaseAmp     *float64        `json:"base_amplitude"`
		OctaveCount *int            `json:"octave_count"`
		Mods        []float64       `json:"amplitude_modifiers"`
		Normalize   json.RawMessage `json:"normalize"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return vnNoiseParams{}, fmt.Errorf("vanilla noise %q: %w", name, err)
	}
	if len(d.Normalize) > 0 && string(d.Normalize) != "true" {
		return vnNoiseParams{}, fmt.Errorf("vanilla noise %q: normalize %s is not supported", name, d.Normalize)
	}
	np := vnNoiseParams{baseOctave: d.BaseOctave, baseAmp: 1, octaves: 1, mods: d.Mods}
	if d.BaseAmp != nil {
		np.baseAmp = *d.BaseAmp
	}
	if d.OctaveCount != nil {
		np.octaves = *d.OctaveCount
	}
	return np, nil
}

func vtStripNS(name string) string {
	if len(name) > 10 && name[:10] == "minecraft:" {
		return name[10:]
	}
	return name
}

// vtNormalNoise is NormalNoise.create(random): the octaves of the
// parameters, each a pair of PerlinNoise from two positional forks, the
// second at 1.0181268882175227 times the frequency.
func vtNormalNoise(r vtRandom, np vnNoiseParams) *vtStack {
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
	first := r.forkPositional()
	second := r.forkPositional()
	out := &vtStack{}
	for _, o := range octs {
		seed := "octave_" + strconv.Itoa(o.index)
		f := newVNPerlin(first.fromHashOf(seed))
		s := newVNPerlin(second.fromHashOf(seed))
		vf := float32(norm * o.ampl)
		out.layers = append(out.layers, vtLayer{f, o.freq, vf}, vtLayer{s, o.freq * 1.0181268882175227, vf})
	}
	return out
}

// vtBlendedFbm is BlendedNoise.createFbm: octaves drawn in turn from one
// random source, the coarsest first.
func vtBlendedFbm(r vtRandom, firstOctave int, smearY, valueFactor float64) *vtStack {
	octaves := -firstOctave + 1
	factor := 1.0
	valueFactor /= math.Pow(2, float64(octaves)) - 1
	s := &vtStack{}
	for i := octaves - 1; i >= 0; i-- {
		s.layers = append(s.layers, vtLayer{&vtSmeared{newVNPerlin(r), smearY * factor}, factor, float32(valueFactor)})
		factor /= 2
		valueFactor *= 2
	}
	return s
}
