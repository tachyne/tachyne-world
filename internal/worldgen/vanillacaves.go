package worldgen

import "sync/atomic"

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
//     vanillacarvers.go), seeded per start chunk as vanilla seeds them.
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
// has no overhangs to carry) and no jaggedness. The factor is a constant —
// 6.3 on land, the spline's value wherever ridges are at or below -0.2 and
// so its most common one, and 3.95 under the sea (continents at or below
// -0.19) — where vanilla's spline varies it from 0.625 to 6.3 with
// continentalness, erosion and ridges the engine does not have. The factor
// only decides how deep under the ground the full cave set begins (where
// sloped_cheese reaches 1.5625: about eight blocks down on land; above it
// only the entrances cut) and where the cheese caves stop being held back
// (sloped_cheese 2.34, about twelve); deeper, the caves are vanilla's
// density alone. Aquifers are not modelled: a cave is air, lava below
// y=-54 as vanilla's global fluid rule has it, and under the sea the five
// blocks below the floor stay solid (the engine's rule) rather than
// flooding through. The beardifier is the engine's terrain adaptation.

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
	if g.nether || g.end {
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

// The vanilla terrain factor the sloped_cheese model uses (see above).
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

// vanillaCaves holds one world's cave noises and its per-chunk cave masks.
type vanillaCaves struct {
	seed int64

	caveLayer, cheese, entrance                     *vnNormal
	s3d1, s3d2, s3dRarity, s3dThick                 *vnNormal
	rough, roughMod                                 *vnNormal
	s2d, s2dMod, s2dElev, s2dThick                  *vnNormal
	pillar, pillarRare, pillarThick                 *vnNormal
	noodle, noodleThick, noodleRidgeA, noodleRidgeB *vnNormal

	// slots caches chunks' masks, direct-mapped by chunk position: a slot
	// holds the last chunk that hashed to it (generation runs on many
	// goroutines; a mask is immutable once stored).
	slots [vcSlots]atomic.Pointer[vcChunk]
}

// vcChunk is one chunk's caves: a bit per cell, (y-MinY)*256 + lz*16 + lx,
// set where vanilla's density or a cave carver opens the cell.
type vcChunk struct {
	cx, cz int32
	mask   []uint64
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
	return min(v, hi)
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
	return min(open, rough+vcClamp(max(s1, s2)+thick, -1, 1))
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
	return vcClamp(max(vcAbs(sel)+tm*0.083, vcCube(vcAbs(elev+vcGradient(y, -64, 320, 8, -40))+tm)), -1, 1)
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
	d := factor * float32(h-y) / 128
	if d <= 0 {
		d *= 0.25 // quarter_negative
	}
	sloped := 4 * d
	rough := v.roughness(x, y, z)
	ent := v.entrances(x, y, z, rough)
	var inner float32
	if sloped >= -1000000 && sloped < 1.5625 {
		inner = min(sloped, ent*5)
	} else {
		layer := vcSample(v.caveLayer, x, y, z, 1, 8)
		cheese := layer*layer*4 + (vcClamp(vcSample(v.cheese, x, y, z, 1, 0.6666666666666666)+0.27, -1, 1) +
			vcClamp(sloped*-0.64+1.5, 0, 0.5))
		p := v.pillars(x, y, z)
		if p >= -1000000 && p < 0.03 {
			p = -1000000
		}
		inner = max(min(min(cheese, ent), v.spaghetti2D(x, y, z)+rough), p)
	}
	// The top slide (y 240 → 256) toward -0.078125, then the bottom slide
	// (y -64 → -40) toward 0.1171875.
	if a := vcGradient(y, 240, 256, 1, 0); a == 0 {
		inner = -0.078125
	} else if a != 1 {
		inner = vnLerp(a, -0.078125, inner)
	}
	if a := vcGradient(y, -64, -40, 0, 1); a == 0 {
		inner = 0.1171875
	} else if a != 1 {
		inner = vnLerp(a, 0.1171875, inner)
	}
	return inner * 0.64
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

// open reports whether vanilla's caves open (x, y, z).
func (v *vanillaCaves) open(g *Generator, x, y, z int) bool {
	cx, cz := int32(floorDiv16(x)), int32(floorDiv16(z))
	c := v.chunk(g, cx, cz)
	i := (y-MinY)*256 + (z-int(cz)*16)*16 + (x - int(cx)*16)
	if i < 0 || i >= len(c.mask)*64 {
		return false
	}
	return c.mask[i>>6]&(1<<(i&63)) != 0
}

// chunk is chunk (cx, cz)'s cave mask, from the cache or built.
func (v *vanillaCaves) chunk(g *Generator, cx, cz int32) *vcChunk {
	slot := &v.slots[(uint32(cx)*0x9E3779B1^uint32(cz)*0x85EBCA77)&(vcSlots-1)]
	if c := slot.Load(); c != nil && c.cx == cx && c.cz == cz {
		return c
	}
	c := v.build(g, cx, cz)
	slot.Store(c)
	return c
}

// build works out one chunk's caves: the density at the 5×5 columns of
// cell corners, interpolated over every cell, then the cave carvers.
func (v *vanillaCaves) build(g *Generator, cx, cz int32) *vcChunk {
	bx, bz := int(cx)*16, int(cz)*16
	cells := g.sections * 16
	ny := cells/8 + 1 // corner rows
	main := make([]float32, 25*ny)
	nd := make([]float32, 25*ny)
	nt := make([]float32, 25*ny)
	na := make([]float32, 25*ny)
	nb := make([]float32, 25*ny)
	for ix := 0; ix < 5; ix++ {
		for iz := 0; iz < 5; iz++ {
			x, z := bx+ix*4, bz+iz*4
			h := g.Height(x, z)
			factor := vcFactorLand
			if h < SeaLevel {
				factor = vcFactorOcean
			}
			for iy := 0; iy < ny; iy++ {
				y := MinY + iy*8
				k := (ix*5+iz)*ny + iy
				main[k] = v.corner(x, y, z, h, factor)
				nd[k], nt[k], na[k], nb[k] = v.noodleCorner(x, y, z)
			}
		}
	}
	c := &vcChunk{cx: cx, cz: cz, mask: make([]uint64, (cells*256+63)/64)}
	var fm, fd, ft, fa, fb [128]float32
	fill := func(q []float32, ix, iz, iy int, out *[128]float32) {
		k := func(dx, dz, dy int) float32 { return q[((ix+dx)*5+iz+dz)*ny+iy+dy] }
		vcFillCell(k(0, 0, 0), k(1, 0, 0), k(0, 0, 1), k(1, 0, 1), k(0, 1, 0), k(1, 1, 0), k(0, 1, 1), k(1, 1, 1), out)
	}
	for ix := 0; ix < 4; ix++ {
		for iz := 0; iz < 4; iz++ {
			for iy := 0; iy < ny-1; iy++ {
				fill(main, ix, iz, iy, &fm)
				fill(nd, ix, iz, iy, &fd)
				fill(nt, ix, iz, iy, &ft)
				fill(na, ix, iz, iy, &fa)
				fill(nb, ix, iz, iy, &fb)
				for lx := 0; lx < 4; lx++ {
					for lz := 0; lz < 4; lz++ {
						for ly := 0; ly < 8; ly++ {
							j := (lx*4+lz)*8 + ly
							noodle := float32(64)
							if !(fd[j] >= -1000000 && fd[j] < 0) {
								noodle = ft[j] + 1.5*max(vcAbs(fa[j]), vcAbs(fb[j]))
							}
							// final_density = min(squeeze(interpolated), noodle) +
							// beardifier: squeeze keeps the sign, so a cell is
							// open where either term is at or below zero.
							if fm[j] > 0 && noodle > 0 {
								continue
							}
							i := (iy*8+ly)*256 + (iz*4+lz)*16 + ix*4 + lx
							c.mask[i>>6] |= 1 << (i & 63)
						}
					}
				}
			}
		}
	}
	v.carveCaves(g, cx, cz, c.mask)
	return c
}

// carveVanilla is carve for a vanilla-caves world: the cell opens where
// vanilla's caves do, as air (lava below y=-54), with the engine's floor
// crust over bedrock and its solid layer under the sea floor kept.
func (g *Generator) carveVanilla(b uint32, wx, wy, wz, colH int) uint32 {
	if wy < caveMinY || !carveable(b) {
		return b
	}
	ceil := colH
	if colH <= SeaLevel+1 {
		ceil = colH - caveSeaFloorGap
	}
	if wy >= ceil || !g.vcaves.open(g, wx, wy, wz) {
		return b
	}
	if wy < vanillaLavaLevel {
		return Lava
	}
	return Air
}
