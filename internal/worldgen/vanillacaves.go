package worldgen

import (
	"math"
	"sync/atomic"
)

// Vanilla caves — the overworld's cave generator as 26.3 has it, chosen per
// world (CaveMode) in place of the engine's own tunnel field (carve in
// generator.go):
//
//   - the noise caves of overworld/final_density: cheese caves (cave_cheese
//     with cave_layer), the entrances (cave_entrance and the 3-D spaghetti
//     with its rarity-scaled samplers, thickness and roughness), the 2-D
//     spaghetti (with its elevation and thickness modulator), the pillars
//     left standing in big caves, and the noodle caves — sampled at the
//     corners of vanilla's 4×8×4 cells and interpolated across them as
//     vanilla interpolates, with the bottom and top slides;
//   - the cave and cave_extra_underground carvers (CaveWorldCarver,
//     vanillacarvers.go), seeded per start chunk as vanilla seeds them;
//   - the aquifer (NoiseBasedAquifer, vanillaaquifer.go) that decides what
//     each opened cell holds.
//
// Every noise is seeded from the world seed and its name exactly as 26.3's
// RandomState does (vanillanoise.go), so a seed samples vanilla's values.
//
// What differs from vanilla, and why. Vanilla's caves are terms in one 3-D
// density whose other terms are the terrain itself (sloped_cheese: depth
// times the terrain factor, plus 3-D base noise); the engine's terrain is a
// per-column height. So the terrain term is modelled from the column:
// sloped_cheese = 4·quarter_negative(factor·(h−y)/128), the depth gradient's
// slope of 1/128 a block taken from the engine's surface h (vanilla's depth
// is zero at its own surface), with no base_3d_noise (the engine's terrain
// has no overhangs to carry) and no jaggedness. The factor is vanilla's
// own (vanillafactor.go: the overworld/factor spline over vanilla's
// climate noises for the seed, bit for bit), with only the side of the
// coast taken from the engine's column, so the engine's sea gets the
// ocean's 3.95 and its land vanilla's land values, 0.625 to 6.3. The
// factor decides how deep under the ground the full cave set begins (where
// sloped_cheese reaches 1.5625: 50/factor blocks, eight at 6.3, eighty at
// 0.625; above it only the entrances cut) and where the cheese caves stop
// being held back (sloped_cheese 2.34); deeper, the caves are vanilla's
// density alone. What fills an opened cell is vanilla's aquifer
// (vanillaaquifer.go): air, local lakes of water at their own levels, lava
// pockets deep down and every cave below y=-54, flooded caves under the
// sea, with the barrier rock between differing levels — its surface input
// computed over the same model. The cave carvers fill their cells through
// the same aquifer at density zero and lay grass on the dirt under a run
// they cut down from a grass surface, as applyCarvingMask does. Two engine
// rules stay: a two-block floor crust over bedrock, and, since the engine's
// sea water stands above the floor regardless (vanilla's is the aquifer's
// own), a sea or river floor block the aquifer would leave as air stays
// solid. The beardifier is the engine's terrain adaptation.

// CaveMode picks a world's cave generator. It is chosen when a world is
// made and never changes after (it shapes every cave).
type CaveMode uint8

const (
	CavesNative  CaveMode = iota // the engine's own caves (every world made before the choice existed)
	CavesVanilla                 // 26.3's noise caves and cave carvers
)

func (m CaveMode) String() string {
	if m == CavesVanilla {
		return "vanilla"
	}
	return "native"
}

// ParseCaveMode reads "native" or "vanilla".
func ParseCaveMode(s string) (CaveMode, bool) {
	switch s {
	case "native":
		return CavesNative, true
	case "vanilla":
		return CavesVanilla, true
	}
	return CavesNative, false
}

// SetCaveMode picks the overworld's cave generator. Must be called at boot,
// before any chunk is generated. The Nether and the End have no such
// choice; earth mode carves no caves either way.
func (g *Generator) SetCaveMode(m CaveMode) {
	if g.nether || g.end || g.void {
		return
	}
	g.caveMode = m
	g.vcaves = nil
	if m == CavesVanilla {
		g.vcaves = newVanillaCaves(g.seed)
	}
}

// CaveMode is the generator's cave generator.
func (g *Generator) CaveMode() CaveMode { return g.caveMode }

// vanillaLavaLevel is the global fluid rule's lava: a cave cell below y=-54
// is lava (NoiseBasedChunkGenerator.createFluidPicker; the aquifer returns
// it before anything else).
const vanillaLavaLevel = -54

// The land and sea constants the model used for the factor before it took
// vanilla's own (the comparison in vanillafactor.go; constFactor).
const (
	vcFactorLand  = float32(6.3)
	vcFactorOcean = float32(3.95)
)

// vcNoiseParams are 26.3's worldgen/noise files for the cave noises.
var vcNoiseParams = map[string]vnNoiseParams{
	"cave_layer":                    {baseOctave: -8, baseAmp: 0.955388882960065},
	"cave_cheese":                   {baseOctave: -8, baseAmp: 0.8361300524356068, octaves: 9, mods: []float64{0.5, 1, 2, 1, 2, 1, 0, 2, 0}},
	"cave_entrance":                 {baseOctave: -7, baseAmp: 0.8500634887071167, octaves: 3, mods: []float64{0.4, 0.5, 1}},
	"spaghetti_3d_1":                {baseOctave: -7, baseAmp: 0.955388882960065},
	"spaghetti_3d_2":                {baseOctave: -7, baseAmp: 0.955388882960065},
	"spaghetti_3d_rarity":           {baseOctave: -11, baseAmp: 0.955388882960065},
	"spaghetti_3d_thickness":        {baseOctave: -8, baseAmp: 0.955388882960065},
	"spaghetti_roughness":           {baseOctave: -5, baseAmp: 0.955388882960065},
	"spaghetti_roughness_modulator": {baseOctave: -8, baseAmp: 0.955388882960065},
	"spaghetti_2d":                  {baseOctave: -7, baseAmp: 0.955388882960065},
	"spaghetti_2d_modulator":        {baseOctave: -11, baseAmp: 0.955388882960065},
	"spaghetti_2d_elevation":        {baseOctave: -8, baseAmp: 0.955388882960065},
	"spaghetti_2d_thickness":        {baseOctave: -11, baseAmp: 0.955388882960065},
	"pillar":                        {baseOctave: -7, baseAmp: 0.9494731054427981, octaves: 2},
	"pillar_rareness":               {baseOctave: -8, baseAmp: 0.955388882960065},
	"pillar_thickness":              {baseOctave: -8, baseAmp: 0.955388882960065},
	"noodle":                        {baseOctave: -8, baseAmp: 0.955388882960065},
	"noodle_thickness":              {baseOctave: -8, baseAmp: 0.955388882960065},
	"noodle_ridge_a":                {baseOctave: -7, baseAmp: 0.955388882960065},
	"noodle_ridge_b":                {baseOctave: -7, baseAmp: 0.955388882960065},
}

// vcSlots is the per-chunk cave cache's size (a power of two).
const vcSlots = 1024

// vanillaCaves holds one world's cave noises and its per-chunk cave results.
type vanillaCaves struct {
	seed int64

	caveLayer, cheese, entrance                     *vnNormal
	s3d1, s3d2, s3dRarity, s3dThick                 *vnNormal
	rough, roughMod                                 *vnNormal
	s2d, s2dMod, s2dElev, s2dThick                  *vnNormal
	pillar, pillarRare, pillarThick                 *vnNormal
	noodle, noodleThick, noodleRidgeA, noodleRidgeB *vnNormal

	aquifer *vanillaAquifer
	vfactor *vanillaFactor
	// constFactor uses the land and sea constants for the factor in place
	// of vanilla's (the comparison the factor's choice was measured by).
	constFactor bool

	// slots caches chunks' results, direct-mapped by chunk position: a slot
	// holds the last chunk that hashed to it (generation runs on many
	// goroutines; a result is immutable once stored).
	slots [vcSlots]atomic.Pointer[vcChunk]
}

// vcChunk is one chunk's caves: what each cell under the ground became.
// codes holds two bits a cell, (y-MinY)*256 + lz*16 + lx, for the rows up
// to the chunk's highest surface: vcKeep (the terrain's block), or the air,
// water or lava the caves and the aquifer put there. grass holds the few
// cells the carvers' grass fix-up turned to the column's top block.
type vcChunk struct {
	cx, cz int32
	cells  int
	codes  []uint64
	grass  map[int32]uint32
}

const (
	vcKeep uint64 = iota
	vcAir
	vcWater
	vcLava
)

func (c *vcChunk) code(i int) uint64 {
	if i < 0 || i >= c.cells {
		return vcKeep
	}
	return c.codes[i>>5] >> ((i & 31) * 2) & 3
}

func newVanillaCaves(seed int64) *vanillaCaves {
	w := vnWorldPositional(seed)
	n := func(name string) *vnNormal { return newVNNormal(w, "minecraft:"+name, vcNoiseParams[name]) }
	return &vanillaCaves{
		seed:         seed,
		caveLayer:    n("cave_layer"),
		cheese:       n("cave_cheese"),
		entrance:     n("cave_entrance"),
		s3d1:         n("spaghetti_3d_1"),
		s3d2:         n("spaghetti_3d_2"),
		s3dRarity:    n("spaghetti_3d_rarity"),
		s3dThick:     n("spaghetti_3d_thickness"),
		rough:        n("spaghetti_roughness"),
		roughMod:     n("spaghetti_roughness_modulator"),
		s2d:          n("spaghetti_2d"),
		s2dMod:       n("spaghetti_2d_modulator"),
		s2dElev:      n("spaghetti_2d_elevation"),
		s2dThick:     n("spaghetti_2d_thickness"),
		pillar:       n("pillar"),
		pillarRare:   n("pillar_rareness"),
		pillarThick:  n("pillar_thickness"),
		noodle:       n("noodle"),
		noodleThick:  n("noodle_thickness"),
		noodleRidgeA: n("noodle_ridge_a"),
		noodleRidgeB: n("noodle_ridge_b"),
		aquifer:      newVanillaAquifer(seed),
		vfactor:      newVanillaFactor(seed),
	}
}

// sample is a NoiseFunction: the noise at the block scaled by xz and y.
func vcSample(n *vnNormal, x, y, z int, xz, ys float64) float32 {
	return n.get(float64(x)*xz, float64(y)*ys, float64(z)*xz)
}

// vcGradient is a clamped y gradient (GradientFunction.ClampedSampler).
func vcGradient(y, from, to int, fromV, toV float32) float32 {
	f := (toV - fromV) / float32(to-from)
	c := max(min(from, to), min(y, max(from, to)))
	return fromV + float32(c-from)*f
}

func vcClamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	return vcMin(v, hi)
}

// vcMin and vcMax are Java's Math.min/max on floats: NaN wins, and -0 is
// below +0. (The package's own min and max take ints.)
func vcMin(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(b)) {
		return b
	}
	if a <= b {
		return a
	}
	return b
}

func vcMax(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(a)) {
		return b
	}
	if a >= b {
		return a
	}
	return b
}

func vcAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func vcCube(v float32) float32 { return v * v * v }

// roughness is overworld/caves/spaghetti_roughness_function.
func (v *vanillaCaves) roughness(x, y, z int) float32 {
	mod := vcSample(v.roughMod, x, y, z, 1, 1)
	r := vcSample(v.rough, x, y, z, 1, 1)
	return (mod*-0.05 + -0.05) * (vcAbs(r) + -0.4)
}

// spaghetti3D is one interval_select over a spaghetti_3d noise, keyed by
// the rarity noise: the noise at 4/3, 1, 2/3 or 1/2 scale times 0.75, 1,
// 1.5 or 2.
func vcSpaghetti3D(n *vnNormal, rarity float32, x, y, z int) float32 {
	switch {
	case rarity < -0.5:
		return vcSample(n, x, y, z, 1.3333333333333333, 1.3333333333333333) * 0.75
	case rarity < 0:
		return vcSample(n, x, y, z, 1, 1) * 1
	case rarity < 0.5:
		return vcSample(n, x, y, z, 0.6666666666666666, 0.6666666666666666) * 1.5
	}
	return vcSample(n, x, y, z, 0.5, 0.5) * 2
}

// entrances is overworld/caves/entrances.
func (v *vanillaCaves) entrances(x, y, z int, rough float32) float32 {
	open := vcSample(v.entrance, x, y, z, 0.75, 0.5) + 0.37 + vcGradient(y, -10, 30, 0.3, 0)
	rarity := vcSample(v.s3dRarity, x, y, z, 2, 1)
	s1 := vcAbs(vcSpaghetti3D(v.s3d1, rarity, x, y, z))
	s2 := vcAbs(vcSpaghetti3D(v.s3d2, rarity, x, y, z))
	thick := vcSample(v.s3dThick, x, y, z, 1, 1)*-0.011500001 + -0.0765
	return vcMin(open, rough+vcClamp(vcMax(s1, s2)+thick, -1, 1))
}

// spaghetti2D is overworld/caves/spaghetti_2d.
func (v *vanillaCaves) spaghetti2D(x, y, z int) float32 {
	tm := vcSample(v.s2dThick, x, y, z, 2, 1)*-0.34999996 + -0.95 // spaghetti_2d_thickness_modulator
	mod := vcSample(v.s2dMod, x, y, z, 2, 1)
	var sel float32
	switch {
	case mod < -0.75:
		sel = vcSample(v.s2d, x, y, z, 2, 2) * 0.5
	case mod < -0.5:
		sel = vcSample(v.s2d, x, y, z, 1.3333333333333333, 1.3333333333333333) * 0.75
	case mod < 0.5:
		sel = vcSample(v.s2d, x, y, z, 1, 1) * 1
	case mod < 0.75:
		sel = vcSample(v.s2d, x, y, z, 0.5, 0.5) * 2
	default:
		sel = vcSample(v.s2d, x, y, z, 0.3333333333333333, 0.3333333333333333) * 3
	}
	elev := vcSample(v.s2dElev, x, y, z, 1, 0) * 8
	return vcClamp(vcMax(vcAbs(sel)+tm*0.083, vcCube(vcAbs(elev+vcGradient(y, -64, 320, 8, -40))+tm)), -1, 1)
}

// pillars is overworld/caves/pillars.
func (v *vanillaCaves) pillars(x, y, z int) float32 {
	p := vcSample(v.pillar, x, y, z, 25, 0.3)*2 + (vcSample(v.pillarRare, x, y, z, 1, 1)*-1 + -1)
	return p * vcCube(vcSample(v.pillarThick, x, y, z, 1, 1)*0.55+0.55)
}

// corner is the input of final_density's interpolated term at a cell
// corner: the slides over the terrain-or-caves choice, times 0.64. h is the
// column's surface, factor the terrain factor (see the file comment).
func (v *vanillaCaves) corner(x, y, z, h int, factor float32) float32 {
	sloped := 4 * vcDepthFactor(y, h, factor)
	rough := v.roughness(x, y, z)
	ent := v.entrances(x, y, z, rough)
	var inner float32
	if sloped >= -1000000 && sloped < 1.5625 {
		inner = vcMin(sloped, ent*5)
	} else {
		layer := vcSample(v.caveLayer, x, y, z, 1, 8)
		cheese := layer*layer*4 + (vcClamp(vcSample(v.cheese, x, y, z, 1, 0.6666666666666666)+0.27, -1, 1) +
			vcClamp(sloped*-0.64+1.5, 0, 0.5))
		p := v.pillars(x, y, z)
		if p >= -1000000 && p < 0.03 {
			p = -1000000
		}
		inner = vcMax(vcMin(vcMin(cheese, ent), v.spaghetti2D(x, y, z)+rough), p)
	}
	return vcSlide(y, inner) * 0.64
}

// vcSlide is slideOverworld: the top slide (y 240 → 256) toward -0.078125,
// then the bottom slide (y -64 → -40) toward 0.1171875.
func vcSlide(y int, v float32) float32 {
	if a := vcGradient(y, 240, 256, 1, 0); a == 0 {
		v = -0.078125
	} else if a != 1 {
		v = vnLerp(a, -0.078125, v)
	}
	if a := vcGradient(y, -64, -40, 0, 1); a == 0 {
		v = 0.1171875
	} else if a != 1 {
		v = vnLerp(a, 0.1171875, v)
	}
	return v
}

// vcDepthFactor is the depth term the model puts in vanilla's place:
// factor·depth with depth (h−y)/128, quarter_negative (see the file
// comment).
func vcDepthFactor(y, h int, factor float32) float32 {
	d := factor * float32(h-y) / 128
	if d <= 0 {
		d *= 0.25 // quarter_negative
	}
	return d
}

// vcPrelimSurface is overworld/preliminary_surface_level over the same
// model: FindTopSurfaceFunction stepping down by eight from the upper bound
// to the first y where the terrain's density without its 3-D noise is
// positive. Vanilla's upper bound, remap(0.2734375/factor − offset, 1.5,
// -1.5, -64, 320) with the model's offset h/128 − 1, is h − 35/factor.
func vcPrelimSurface(h int, factor float32) int {
	upper := vcClamp(float32(h)-35/factor, -40, 320)
	top := int(math.Floor(float64(upper/8))) * 8
	if top <= MinY {
		return MinY
	}
	for y := top; y >= MinY; y -= 8 {
		d := vcClamp(4*vcDepthFactor(y, h, factor)+-0.703125, -64, 64)
		if vcSlide(y, d)+-0.390625 > 0 {
			return y
		}
	}
	return MinY
}

// vcSqueeze is the squeeze postProcess applies to the interpolated term.
func vcSqueeze(v float32) float32 {
	c := vcClamp(v, -1, 1)
	return c/2 - vcCube(c)/24
}

// noodleCorner is overworld/caves/noodle's four interpolated inputs at a
// cell corner: the noodle switch, thickness, and the two ridges (each only
// from y=-60 up to 320).
func (v *vanillaCaves) noodleCorner(x, y, z int) (n, thick, ra, rb float32) {
	if y < -60 || y >= 321 {
		return -1, 0, 0, 0
	}
	return vcSample(v.noodle, x, y, z, 1, 1),
		vcSample(v.noodleThick, x, y, z, 1, 1)*-0.025 + -0.075,
		vcSample(v.noodleRidgeA, x, y, z, 2.6666666666666665, 2.6666666666666665),
		vcSample(v.noodleRidgeB, x, y, z, 2.6666666666666665, 2.6666666666666665)
}

// vcFillCell interpolates one quantity across a 4×8×4 cell from its eight
// corners as InterpolatedFunction.fillCell does — z, then x, then stepping
// up y — into out[(x*4+z)*8+y].
func vcFillCell(v000, v100, v010, v110, v001, v101, v011, v111 float32, out *[128]float32) {
	for z := 0; z < 4; z++ {
		az := float32(z) * 0.25
		v00 := vnLerp(az, v000, v001)
		v01 := vnLerp(az, v010, v011)
		v10 := vnLerp(az, v100, v101)
		v11 := vnLerp(az, v110, v111)
		for x := 0; x < 4; x++ {
			ax := float32(x) * 0.25
			lo := vnLerp(ax, v00, v10)
			hi := vnLerp(ax, v01, v11)
			step := (hi - lo) * 0.125
			val := lo
			for y := 0; y < 8; y++ {
				out[(x*4+z)*8+y] = val
				val += step
			}
		}
	}
}

// cell is what vanilla's caves made of (x, y, z): the block, and whether
// they changed it at all (false: the terrain's own block stays).
func (v *vanillaCaves) cell(g *Generator, x, y, z int) (uint32, bool) {
	cx, cz := int32(floorDiv16(x)), int32(floorDiv16(z))
	c := v.chunk(g, cx, cz)
	i := (y-MinY)*256 + (z-int(cz)*16)*16 + (x - int(cx)*16)
	switch c.code(i) {
	case vcAir:
		return Air, true
	case vcWater:
		return Water, true
	case vcLava:
		return Lava, true
	}
	if b, ok := c.grass[int32(i)]; ok {
		return b, true
	}
	return 0, false
}

// open reports whether vanilla's caves open (x, y, z): air, water or lava.
func (v *vanillaCaves) open(g *Generator, x, y, z int) bool {
	b, ok := v.cell(g, x, y, z)
	return ok && (b == Air || b == Water || b == Lava)
}

// chunk is chunk (cx, cz)'s caves, from the cache or built.
func (v *vanillaCaves) chunk(g *Generator, cx, cz int32) *vcChunk {
	slot := &v.slots[(uint32(cx)*0x9E3779B1^uint32(cz)*0x85EBCA77)&(vcSlots-1)]
	if c := slot.Load(); c != nil && c.cx == cx && c.cz == cz {
		return c
	}
	c := v.build(g, cx, cz)
	slot.Store(c)
	return c
}

// factor is the terrain factor the model gives column (x, z) of surface h:
// vanilla's own factor there, on the engine's side of the coast
// (vanillafactor.go), or with constFactor the land/sea constants.
func (v *vanillaCaves) factor(x, z, h int) float32 {
	sea := h < SeaLevel
	if v.constFactor {
		if sea {
			return vcFactorOcean
		}
		return vcFactorLand
	}
	return v.vfactor.at(x, z, &sea)
}

// aquiferFor is the chunk's aquifer, over the engine's stand-ins for
// vanilla's surface level and deep-dark exclusion (vanillaaquifer.go).
func (v *vanillaCaves) aquiferFor(g *Generator, bx, bz int) *vaqChunk {
	surface := func(x, z int) int {
		h := g.Height(x, z)
		return vcPrelimSurface(h, v.factor(x, z, h))
	}
	excluded := func(x, y, z int) bool { return g.caveBiomeAt(x, y, z) == "minecraft:deep_dark" }
	return newVaqChunk(v.aquifer, bx, MinY, bz, bx+15, g.Ceiling()-1, bz+15, surface, excluded)
}

// vcGrassy is grass_block or mycelium, any state (the carvers' hasGrass).
func vcGrassy(b uint32) bool {
	return b == GrassBlock || b == GrassBlock-1 || b == Mycelium || b == Mycelium-1
}

// build works out one chunk's caves as vanilla fills and carves it: the
// density at the 5×5 columns of cell corners, interpolated over every cell,
// each cell the density opens given what the aquifer puts there; then the
// cave carvers (applyCarvingMask), whose cells take the aquifer's answer at
// density zero, with the fix-up that turns the dirt under a carved run
// that began in grass into the column's top block.
func (v *vanillaCaves) build(g *Generator, cx, cz int32) *vcChunk {
	bx, bz := int(cx)*16, int(cz)*16
	rows := g.sections * 16
	ny := rows/8 + 1 // corner rows
	var cols [256]column
	maxH := MinY
	for lz := 0; lz < 16; lz++ {
		for lx := 0; lx < 16; lx++ {
			cols[lz*16+lx] = g.columnAt(bx+lx, bz+lz)
			maxH = max(maxH, cols[lz*16+lx].h)
		}
	}
	maxH = min(maxH, g.Ceiling())
	cells := (maxH - MinY) * 256
	// open marks a cell the caves may change: under the column's surface,
	// above the floor crust, and a block caves cut (carveable).
	open := func(lx, y, lz int) bool {
		c := &cols[lz*16+lx]
		return y >= caveMinY && y < c.h && carveable(c.block(y))
	}

	main := make([]float32, 25*ny)
	nd := make([]float32, 25*ny)
	nt := make([]float32, 25*ny)
	na := make([]float32, 25*ny)
	nb := make([]float32, 25*ny)
	for ix := 0; ix < 5; ix++ {
		for iz := 0; iz < 5; iz++ {
			x, z := bx+ix*4, bz+iz*4
			h := g.Height(x, z)
			factor := v.factor(x, z, h)
			for iy := 0; iy < ny; iy++ {
				y := MinY + iy*8
				k := (ix*5+iz)*ny + iy
				main[k] = v.corner(x, y, z, h, factor)
				nd[k], nt[k], na[k], nb[k] = v.noodleCorner(x, y, z)
			}
		}
	}
	aq := v.aquiferFor(g, bx, bz)
	state := make([]uint8, cells) // vcKeep/vcAir/vcWater/vcLava a cell
	var fm, fd, ft, fa, fb [128]float32
	fill := func(q []float32, ix, iz, iy int, out *[128]float32) {
		k := func(dx, dz, dy int) float32 { return q[((ix+dx)*5+iz+dz)*ny+iy+dy] }
		vcFillCell(k(0, 0, 0), k(1, 0, 0), k(0, 0, 1), k(1, 0, 1), k(0, 1, 0), k(1, 1, 0), k(0, 1, 1), k(1, 1, 1), out)
	}
	for ix := 0; ix < 4; ix++ {
		for iz := 0; iz < 4; iz++ {
			for iy := 0; iy < ny-1 && MinY+iy*8 < maxH; iy++ {
				fill(main, ix, iz, iy, &fm)
				fill(nd, ix, iz, iy, &fd)
				fill(nt, ix, iz, iy, &ft)
				fill(na, ix, iz, iy, &fa)
				fill(nb, ix, iz, iy, &fb)
				for lx := 0; lx < 4; lx++ {
					for lz := 0; lz < 4; lz++ {
						for ly := 0; ly < 8; ly++ {
							x, y, z := ix*4+lx, MinY+iy*8+ly, iz*4+lz
							if !open(x, y, z) {
								continue
							}
							j := (lx*4+lz)*8 + ly
							noodle := float32(64)
							if !(fd[j] >= -1000000 && fd[j] < 0) {
								noodle = ft[j] + 1.5*vcMax(vcAbs(fa[j]), vcAbs(fb[j]))
							}
							// final_density = min(squeeze(interpolated), noodle)
							// + beardifier (the engine's own terrain adaptation).
							d := vcMin(vcSqueeze(fm[j]), noodle)
							if d > 0 {
								continue
							}
							if s := aq.substance(bx+x, y, bz+z, float64(d)); s != vaqSolid {
								state[(y-MinY)*256+z*16+x] = uint8(s) + 1
							}
						}
					}
				}
			}
		}
	}

	// The carvers' cells, then applyCarvingMask over each column's runs.
	mask := make([]uint64, (rows*256+63)/64)
	v.carveCaves(g, cx, cz, mask)
	var grass map[int32]uint32
	cur := make([]uint32, maxH-MinY)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			c := &cols[lz*16+lx]
			loaded := false
			hasGrass := false
			for y := min(c.h, maxH) - 1; y >= caveMinY; y-- {
				i := (y-MinY)*256 + lz*16 + lx
				if mask[i>>6]&(1<<(i&63)) == 0 {
					hasGrass = false // a run ends: the next one starts afresh
					continue
				}
				if !open(lx, y, lz) {
					continue // the engine's uncarvable (vanilla's is bedrock alone)
				}
				if !loaded { // the column as the noise left it
					for yy := MinY; yy < c.h && yy < maxH; yy++ {
						cur[yy-MinY] = c.block(yy)
						switch state[(yy-MinY)*256+lz*16+lx] {
						case uint8(vcAir):
							cur[yy-MinY] = Air
						case uint8(vcWater):
							cur[yy-MinY] = Water
						case uint8(vcLava):
							cur[yy-MinY] = Lava
						}
					}
					loaded = true
				}
				if vcGrassy(cur[y-MinY]) {
					hasGrass = true
				}
				s := aq.substance(bx+lx, y, bz+lz, 0)
				if s == vaqSolid {
					continue
				}
				cur[y-MinY] = [3]uint32{Air, Water, Lava}[s]
				state[i] = uint8(s) + 1
				if hasGrass && y-1 >= MinY && cur[y-1-MinY] == Dirt && s == vaqAir {
					// topMaterial at the dirt: the column's top (under a fluid,
					// vanilla's rules give dirt, which it already is).
					cur[y-1-MinY] = c.topBlock()
					if grass == nil {
						grass = map[int32]uint32{}
					}
					grass[int32(i-256)] = c.topBlock()
				}
			}
			if loaded {
				// A grass fix-up the run later carved through is carved.
				for k := range grass {
					if int(k)&255 == lz*16+lx && state[k] != uint8(vcKeep) {
						delete(grass, k)
					}
				}
			}
		}
	}

	// The engine's sea and river water stands on the column whatever the
	// aquifer says (vanilla's water above the floor is the aquifer's own
	// decision, so it never sits on a cave the aquifer left dry): a floor
	// cell the aquifer would leave as air under that water stays solid.
	for i := range cols {
		c := &cols[i]
		if y := c.h - 1; c.h <= SeaLevel && y >= caveMinY && y < maxH && c.block(c.h) == Water {
			if k := (y-MinY)*256 + i; state[k] == uint8(vcAir) {
				state[k] = uint8(vcKeep)
			}
		}
	}

	out := &vcChunk{cx: cx, cz: cz, cells: cells, codes: make([]uint64, (cells+31)/32), grass: grass}
	for i, s := range state {
		if s != uint8(vcKeep) {
			out.codes[i>>5] |= uint64(s) << ((i & 31) * 2)
		}
	}
	return out
}

// carveVanilla is carve for a vanilla-caves world: the cell becomes what
// vanilla's caves and aquifer made of it (air, water or lava, or the grass
// the carvers' fix-up lays), with the engine's floor crust over bedrock
// kept. Under the sea the caves reach the floor, flooded as the aquifer
// floods them.
func (g *Generator) carveVanilla(b uint32, wx, wy, wz, colH int) uint32 {
	if wy < caveMinY || wy >= colH || !carveable(b) {
		return b
	}
	if s, ok := g.vcaves.cell(g, wx, wy, wz); ok {
		return s
	}
	return b
}
