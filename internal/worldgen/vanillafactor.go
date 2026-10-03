package worldgen

// The terrain factor of a vanilla-caves world (vanillacaves.go): vanilla's
// overworld/factor, the TerrainProvider.overworldFactor spline over
// continentalness, erosion, weirdness (the ridge noise) and its folded
// peaks-and-valleys, each sampled from vanilla's own climate noise for the
// seed — shifted by the offset noise as overworld/continents, erosion and
// ridges are — so a column's factor is the one vanilla gives it.
//
// One input is the engine's: which side of the coast a column is on.
// Vanilla's continentalness draws its own coastline, which is not the
// engine's, and the factor's continent points are ocean (3.95 at -0.19 and
// below) against land (from -0.15 up). So the continentalness is held on
// the engine's side of vanilla's coast band (OverworldBiomeBuilder's coast
// is -0.19 to -0.11): at most -0.19 under the engine's sea, at least -0.11
// on its land — vanilla's ocean factor under the engine's ocean, vanilla's
// land factor, with vanilla's erosion and ridges, on its land.
//
// Why not from the engine's own terrain (its slope or roughness): over the
// 26.3 server's seed-1 land (29k columns in ±6000 blocks), the factor is
// independent of both — a mean of 5.31 on flat ground, 5.10–5.43 at every
// slope from 8 to 48+ blocks per 16, 5.2–5.3 at every height, the share
// below 4 between 8 and 16% in every bin — so neither carries anything to
// derive it from; vanilla's own field carries its distribution and its
// patches (a tenth of the land under 4, where the full cave set starts
// twelve to eighty blocks down rather than eight).
//
// Measured against the server: cave air by depth under the top terrain
// block of land columns topping out at y 80–100 (the server's seed-1 land
// at chunks 237..261 × -238..-214, 39k columns; the engine's at two
// 625-chunk regions, TestVanillaCaveProfile). The first eight blocks match
// with either factor (0.6% at one block down to 3% at eight); at nine to
// sixteen, where the factor decides, the server holds flat at 2.5–3.0%,
// and the mean distance from it fell from 0.8 to 0.46 points in one region
// (vanilla's factor: 1.9–2.9%; the constant 6.3: 2.9–4.3%, already rising)
// and from 3.4 to 2.8 in the other.

var vfNoiseParams = map[string]vnNoiseParams{
	"offset":          {baseOctave: -3, baseAmp: 0.9381732587751005, octaves: 4, mods: []float64{1, 1, 1, 0}},
	"continentalness": {baseOctave: -9, baseAmp: 0.8880832896205223, octaves: 9, mods: []float64{1, 1, 2, 2, 2, 1, 1, 1, 1}},
	"erosion":         {baseOctave: -9, baseAmp: 1.063180125160734, octaves: 5, mods: []float64{1, 1, 0, 1, 1}},
	"ridge":           {baseOctave: -7, baseAmp: 0.9147152149950137, octaves: 6, mods: []float64{1, 2, 1, 0, 0, 0}},
}

// vfCoord names a spline coordinate.
const (
	vfContinents = iota
	vfErosion
	vfWeirdness // overworld/ridges
	vfRidges    // overworld/ridges_folded
)

// vfSpline is a CubicSpline: a constant, or a multipoint over a coordinate
// (every point of the factor spline has derivative 0).
type vfSpline struct {
	constant bool
	value    float32
	coord    int
	locs     []float32
	vals     []*vfSpline
}

func vfConst(v float32) *vfSpline { return &vfSpline{constant: true, value: v} }

// vfPoints builds a multipoint from location/value pairs; a value is a
// float32 (a constant) or a *vfSpline.
func vfPoints(coord int, pts ...any) *vfSpline {
	s := &vfSpline{coord: coord}
	for i := 0; i < len(pts); i += 2 {
		s.locs = append(s.locs, float32(pts[i].(float64)))
		switch v := pts[i+1].(type) {
		case float64:
			s.vals = append(s.vals, vfConst(float32(v)))
		case *vfSpline:
			s.vals = append(s.vals, v)
		}
	}
	return s
}

// sample is CubicSpline.Multipoint.sample with all derivatives 0.
func (s *vfSpline) sample(in *[4]float32) float32 {
	if s.constant {
		return s.value
	}
	input := in[s.coord]
	start := len(s.locs) // Mth.binarySearch: the first location above input
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
		return s.vals[0].sample(in)
	case start == last:
		return s.vals[last].sample(in)
	}
	const d1, d2 = float32(0), float32(0)
	x1, x2 := s.locs[start], s.locs[start+1]
	t := (input - x1) / (x2 - x1)
	y1 := s.vals[start].sample(in)
	y2 := s.vals[start+1].sample(in)
	a := d1*(x2-x1) - (y2 - y1)
	b := -d2*(x2-x1) + (y2 - y1)
	return vnLerp(t, y1, y2) + t*(1-t)*vnLerp(t, a, b)
}

// vfErosionFactor is TerrainProvider.getErosionFactor (no transform).
func vfErosionFactor(base float64, shattered bool) *vfSpline {
	baseSpline := vfPoints(vfWeirdness, -0.2, 6.3, 0.2, base)
	pts := []any{
		-0.6, baseSpline,
		-0.5, vfPoints(vfWeirdness, -0.05, 6.3, 0.05, 2.67),
		-0.35, baseSpline,
		-0.25, baseSpline,
		-0.1, vfPoints(vfWeirdness, -0.05, 2.67, 0.05, 6.3),
		0.03, baseSpline,
	}
	if shattered {
		weirdShattered := vfPoints(vfWeirdness, 0.0, base, 0.1, 0.625)
		ridgesShattered := vfPoints(vfRidges, -0.9, base, -0.69, weirdShattered)
		pts = append(pts, 0.35, base, 0.45, ridgesShattered, 0.55, ridgesShattered, 0.62, base)
	} else {
		extremeHills := vfPoints(vfRidges, -0.7, baseSpline, -0.15, 1.37)
		peaksOnly := vfPoints(vfRidges, 0.45, baseSpline, 0.7, 1.56)
		pts = append(pts, 0.05, peaksOnly, 0.4, peaksOnly, 0.45, extremeHills, 0.55, extremeHills, 0.58, base)
	}
	return vfPoints(vfErosion, pts...)
}

// vfOverworldFactor is TerrainProvider.overworldFactor (not amplified).
var vfOverworldFactor = vfPoints(vfContinents,
	-0.19, 3.95,
	-0.15, vfErosionFactor(6.25, true),
	-0.1, vfErosionFactor(5.47, true),
	0.03, vfErosionFactor(5.08, true),
	0.06, vfErosionFactor(4.69, false),
)

// vanillaFactor holds a world's climate noises for the factor.
type vanillaFactor struct {
	offset, continents, erosion, ridge *vnNormal
}

func newVanillaFactor(seed int64) *vanillaFactor {
	w := vnWorldPositional(seed)
	n := func(name string) *vnNormal { return newVNNormal(w, "minecraft:"+name, vfNoiseParams[name]) }
	return &vanillaFactor{offset: n("offset"), continents: n("continentalness"), erosion: n("erosion"), ridge: n("ridge")}
}

// climate is overworld/continents, erosion and ridges at a column: each
// noise at a quarter scale, shifted by shift_x (shift_a) and shift_z
// (shift_b) of the offset noise.
func (f *vanillaFactor) climate(x, z int) (cont, ero, ridges float32) {
	sx := f.offset.get(float64(x)*0.25, 0, float64(z)*0.25) * 4
	sz := f.offset.get(float64(z)*0.25, float64(x)*0.25, 0) * 4
	nx := float64(x)*0.25 + float64(sx)
	nz := float64(z)*0.25 + float64(sz)
	return f.continents.get(nx, 0, nz), f.erosion.get(nx, 0, nz), f.ridge.get(nx, 0, nz)
}

// vfFold is overworld/ridges_folded (NoiseRouterData.peaksAndValleys).
func vfFold(r float32) float32 {
	return (vcAbs(vcAbs(r)+-0.6666667) + -0.33333334) * -3
}

// at is overworld/factor at a column, with continentalness held to the
// engine's side of the coast (see the file comment): sea true under the
// engine's sea, false on its land; vanilla's own value when sea is nil.
func (f *vanillaFactor) at(x, z int, sea *bool) float32 {
	cont, ero, ridges := f.climate(x, z)
	if sea != nil {
		if *sea {
			cont = vcMin(cont, -0.19)
		} else {
			cont = vcMax(cont, -0.11)
		}
	}
	in := [4]float32{cont, ero, ridges, vfFold(ridges)}
	return vfOverworldFactor.sample(&in)
}
