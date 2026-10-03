package worldgen

import (
	"math"
	"sync/atomic"
)

// The Nether in vanilla mode: 26.3's noise_settings/nether.json on its own
// y 0..127 — the old blended noise (nether/base_3d_noise) slid to 2.5 at the
// floor and 0.9375 at the roof, interpolated over 4×8×4 cells and squeezed,
// netherrack where it is positive and the lava sea below y=32 where it is
// not; the multi-noise biomes from the nether parameter list over the
// legacy temperature and vegetation noises; the surface rules of
// material_rule/nether.json (the bedrock floor and roof gradients, each
// biome's floor and ceiling dressing, the lava holes and the soul sand and
// gravel bands about the lava level) applied as MaterialSystem walks each
// column, with BiomeManager's fuzzed zoom choosing a block's biome; and the
// nether_cave carver. Everything is seeded as RandomState seeds a legacy
// world, so a seed's terrain is vanilla's, block for block.

// vdmNParams are the 26.3 noise files the Nether reads.
var vdmNParams = map[string]vnNoiseParams{
	"nether/temperature":    {baseOctave: -7, baseAmp: 0.9494731054427981, octaves: 2},
	"nether/vegetation":     {baseOctave: -7, baseAmp: 0.9494731054427981, octaves: 2},
	"surface":               {baseOctave: -6, baseAmp: 0.9381732587751008, octaves: 3},
	"patch":                 {baseOctave: -5, baseAmp: 1.637127519350388, octaves: 6, mods: []float64{1, 0, 0, 0, 0, 0.013333333333333334}},
	"nether_state_selector": {baseOctave: -4, baseAmp: 0.955388882960065},
	"netherrack":            {baseOctave: -3, baseAmp: 1.4659491761370222, octaves: 4, mods: []float64{1, 0, 0, 0.35}},
	"nether_wart":           {baseOctave: -3, baseAmp: 1.3827102115748344, octaves: 4, mods: []float64{1, 0, 0, 0.9}},
	"soul_sand_layer":       {baseOctave: -8, baseAmp: 1.0569606747151457, octaves: 9, mods: []float64{1, 1, 1, 1, 0, 0, 0, 0, 0.013333333333333334}},
	"gravel_layer":          {baseOctave: -8, baseAmp: 1.0569606747151457, octaves: 9, mods: []float64{1, 1, 1, 1, 0, 0, 0, 0, 0.013333333333333334}},
}

// The Nether's biomes, in the parameter list's order.
const (
	vdmNWastes uint8 = iota
	vdmNSoulSand
	vdmNCrimson
	vdmNWarped
	vdmNDeltas
)

var vdmNNames = [...]string{
	"minecraft:nether_wastes", "minecraft:soul_sand_valley", "minecraft:crimson_forest",
	"minecraft:warped_forest", "minecraft:basalt_deltas",
}

// vdmNPoints is MultiNoiseBiomeSourceParameterList.Preset.NETHER: each
// biome's temperature, humidity and offset (the other parameters are 0),
// quantized.
var vdmNPoints = [5][3]int64{
	{0, 0, 0},
	{0, QuantizeClimate(-0.5), 0},
	{QuantizeClimate(0.4), 0, 0},
	{0, QuantizeClimate(0.5), QuantizeClimate(0.375)},
	{QuantizeClimate(-0.5), 0, QuantizeClimate(0.175)},
}

// vdmNSeaLevel is nether.json's sea_level: the lava sea fills open
// cells below it.
const vdmNSeaLevel = 32

// vanillaNether is one seed's vanilla Nether.
type vanillaNether struct {
	seed int64
	g    *Generator

	base                           *vdmBlended
	temp, veg                      *vnNormal
	surface, patch, selector, rack *vnNormal
	wart, soulLayer, gravelLayer   *vnNormal
	world, floorRand, roofRand     vdmLegacyPos
	zoom                           int64
	slots                          [vdmSlots]atomic.Pointer[vdmTerrain]
}

func newVanillaNether(seed int64, g *Generator) *vanillaNether {
	world := newVDMLegacy(seed).forkPositional()
	n := func(name string) *vnNormal { return newVDMNormal(world, name, vdmNParams[name]) }
	return &vanillaNether{
		seed:        seed,
		g:           g,
		base:        newVDMBlended(seed, 0.25, 0.375, 80, 60, 8),
		temp:        newVDMNetherBiomeNoise(newVDMLegacy(seed), vdmNParams["nether/temperature"]),
		veg:         newVDMNetherBiomeNoise(newVDMLegacy(seed+1), vdmNParams["nether/vegetation"]),
		surface:     n("surface"),
		patch:       n("patch"),
		selector:    n("nether_state_selector"),
		rack:        n("netherrack"),
		wart:        n("nether_wart"),
		soulLayer:   n("soul_sand_layer"),
		gravelLayer: n("gravel_layer"),
		world:       world,
		floorRand:   world.fromHashOf("minecraft:bedrock_floor").forkPositional(),
		roofRand:    world.fromHashOf("minecraft:bedrock_roof").forkPositional(),
		zoom:        vdmZoomSeed(seed),
	}
}

// corner is final_density's interpolated input at a cell corner.
func (v *vanillaNether) corner(x, y, z int) float32 {
	inner := func() float32 {
		return vdmLerpConstFirst(vcGradient(y, 104, 128, 1, 0), 0.9375, func() float32 { return v.base.sample(x, y, z) })
	}
	return vdmLerpConstFirst(vcGradient(y, -8, 24, 0, 1), 2.5, inner) * 0.64
}

// Climate samples the Nether's climate at a quart (Climate.Sampler: the
// router's temperature and vegetation; every other parameter is 0).
func (v *vanillaNether) Sample(qx, qy, qz int) ClimatePoint {
	x, z := float64(qx*4)*0.25, float64(qz*4)*0.25
	return ClimatePoint{
		Temperature: QuantizeClimate(v.temp.get(x, 0, z)),
		Humidity:    QuantizeClimate(v.veg.get(x, 0, z)),
	}
}

// noiseBiome is the multi-noise biome at a quart: the parameter point
// nearest the climate (Climate.ParameterPoint.fitness).
func (v *vanillaNether) noiseBiome(qx, qz int) uint8 {
	c := v.Sample(qx, 0, qz)
	best, bestF := uint8(0), int64(math.MaxInt64)
	for i, p := range vdmNPoints {
		dt, dh := c.Temperature-p[0], c.Humidity-p[1]
		f := dt*dt + dh*dh + p[2]*p[2]
		if f < bestF {
			best, bestF = uint8(i), f
		}
	}
	return best
}

// BiomeAt is the noise biome at a quart (VanillaBiomes).
func (v *vanillaNether) BiomeAt(qx, qy, qz int) string { return vdmNNames[v.noiseBiome(qx, qz)] }

// surfaceDepth is MaterialSystem.getSurfaceDepth.
func (v *vanillaNether) surfaceDepth(x, z int) int {
	n := float64(v.surface.get(float64(x), 0, float64(z)))
	return int(n*2.75 + 3.0 + v.world.at(x, 0, z).nextDouble()*0.25)
}

// terrain is chunk (cx, cz)'s terrain, from the cache or built.
func (v *vanillaNether) terrain(cx, cz int32) *vdmTerrain {
	slot := &v.slots[(uint32(cx)*0x9E3779B1^uint32(cz)*0x85EBCA77)&(vdmSlots-1)]
	if t := slot.Load(); t != nil && t.cx == cx && t.cz == cz {
		return t
	}
	t := v.build(cx, cz)
	slot.Store(t)
	return t
}

// build fills, dresses and carves one chunk as buildTerrain does: doFill,
// buildSurface, generateCarvers.
func (v *vanillaNether) build(cx, cz int32) *vdmTerrain { return v.buildStages(cx, cz, 3) }

// buildStages is build stopped after the fill (1), the surface (2) or the
// carvers (3).
func (v *vanillaNether) buildStages(cx, cz int32, stages int) *vdmTerrain {
	t := &vdmTerrain{cx: cx, cz: cz}
	bx, bz := int(cx)*16, int(cz)*16
	var dens [vdmH * 256]float32
	vdmInterpolate(bx, bz, 4, 8, v.corner, &dens)
	for y := 0; y < vdmH; y++ {
		for i := 0; i < 256; i++ {
			d := vcSqueeze(dens[y*256+i])
			switch {
			case d > 0:
				t.codes[y*256+i] = vdmNetherrack
			case y < vdmNSeaLevel:
				t.codes[y*256+i] = vdmLava
			}
		}
	}
	// The noise biomes over the chunk and a quart around it (the zoom
	// reaches one quart past either side).
	var quarts [6][6]uint8
	qx0, qz0 := bx>>2-1, bz>>2-1
	for i := 0; i < 6; i++ {
		for k := 0; k < 6; k++ {
			quarts[i][k] = v.noiseBiome(qx0+i, qz0+k)
		}
	}
	for i := 0; i < 4; i++ {
		for k := 0; k < 4; k++ {
			t.biome[k*4+i] = quarts[i+1][k+1]
		}
	}
	if stages < 2 {
		return t
	}
	biomeAt := func(x, y, z int) uint8 {
		qx, _, qz := vdmZoom(v.zoom, x, y, z)
		return quarts[qx-qx0][qz-qz0]
	}
	for lz := 0; lz < 16; lz++ {
		for lx := 0; lx < 16; lx++ {
			v.surfaceColumn(t, bx+lx, bz+lz, lx, lz, biomeAt)
		}
	}
	if stages >= 3 {
		v.carve(t)
	}
	return t
}

// vdmNRuleCtx is MaterialRuleContext for one column and cell.
type vdmNRuleCtx struct {
	v                      *vanillaNether
	x, y, z                int
	stoneAbove, stoneBelow int
	surfaceDepth           int
	biomeAt                func(x, y, z int) uint8
	biome                  int // -1 until asked
	noise2                 map[*vnNormal]float64
}

func (c *vdmNRuleCtx) getBiome() uint8 {
	if c.biome < 0 {
		c.biome = int(c.biomeAt(c.x, c.y, c.z))
	}
	return uint8(c.biome)
}

// above is a noise_threshold condition (2-D, the maximum Double.MAX_VALUE).
func (c *vdmNRuleCtx) above(n *vnNormal, min float64) bool {
	val, ok := c.noise2[n]
	if !ok {
		val = float64(n.get(float64(c.x), 0, float64(c.z)))
		c.noise2[n] = val
	}
	return val >= min
}

// stoneDepth is a stone_depth condition with offset 0 and no secondary
// range: the depth from the floor (or ceiling) at most one plus the surface
// depth when it is added.
func (c *vdmNRuleCtx) floorDepth(add bool) bool {
	sd := 0
	if add {
		sd = c.surfaceDepth
	}
	return c.stoneAbove <= 1+sd
}

func (c *vdmNRuleCtx) ceilingDepth(add bool) bool {
	sd := 0
	if add {
		sd = c.surfaceDepth
	}
	return c.stoneBelow <= 1+sd
}

// yAbove is a y_above condition (surface_depth_multiplier 0).
func (c *vdmNRuleCtx) yAbove(anchor int, addStone bool) bool {
	y := c.y
	if addStone {
		y += c.stoneAbove
	}
	return y >= anchor
}

func (c *vdmNRuleCtx) hole() bool { return c.surfaceDepth <= 0 }

// gradient is a vertical_gradient condition.
func (c *vdmNRuleCtx) gradient(r vdmLegacyPos, trueAtAndBelow, falseAtAndAbove int) bool {
	if c.y <= trueAtAndBelow {
		return true
	}
	if c.y >= falseAtAndAbove {
		return false
	}
	p := float64(trueAtAndBelow)
	p = 1 + (float64(c.y)-p)/(float64(falseAtAndAbove)-p)*(0-1)
	return float64(r.at(c.x, c.y, c.z).nextFloat()) < p
}

// The anchors of the Nether's rules: its gen range is y 0..127.
const (
	vdmNBottom   = 0
	vdmNBelowTop = vdmH - 1 // below_top 0
)

// rule is material_rule/nether.json; ok=false is no result (the cell keeps
// its block).
func (c *vdmNRuleCtx) rule() (uint8, bool) {
	v := c.v
	if c.gradient(v.floorRand, vdmNBottom, vdmNBottom+5) { // bedrock_floor
		return vdmBedrock, true
	}
	if !c.gradient(v.roofRand, vdmNBelowTop-5, vdmNBelowTop) { // bedrock_roof
		return vdmBedrock, true
	}
	if c.yAbove(vdmNBelowTop-5, false) {
		return vdmNetherrack, true
	}
	// The gravel band about the lava level, the same in three biomes.
	gravelBand := func() bool {
		return c.above(v.patch, -0.012) && c.yAbove(30, true) && !c.yAbove(35, true)
	}
	switch c.getBiome() {
	case vdmNDeltas:
		if c.ceilingDepth(true) {
			return vdmBasalt, true
		}
		if c.floorDepth(true) {
			if gravelBand() {
				return vdmGravel, true
			}
			if c.above(v.selector, 0) {
				return vdmBasalt, true
			}
			return vdmBlackstone, true
		}
	case vdmNSoulSand:
		if c.ceilingDepth(true) {
			if c.above(v.selector, 0) {
				return vdmSoulSand, true
			}
			return vdmSoulSoil, true
		}
		if c.floorDepth(true) {
			if gravelBand() {
				return vdmGravel, true
			}
			if c.above(v.selector, 0) {
				return vdmSoulSand, true
			}
			return vdmSoulSoil, true
		}
	}
	if c.floorDepth(false) { // on_floor
		if !c.yAbove(32, false) && c.hole() {
			return vdmLava, true
		}
		switch b := c.getBiome(); b {
		case vdmNWarped, vdmNCrimson:
			if !c.above(v.rack, 0.54) && c.yAbove(31, false) {
				if c.above(v.wart, 1.17) {
					if b == vdmNWarped {
						return vdmWarpedWart, true
					}
					return vdmNetherWart, true
				}
				if b == vdmNWarped {
					return vdmWarpedNylium, true
				}
				return vdmCrimsonNylium, true
			}
		}
	}
	if c.getBiome() == vdmNWastes {
		if c.floorDepth(true) && c.above(v.soulLayer, -0.012) {
			if !c.hole() && c.yAbove(30, true) && !c.yAbove(35, true) {
				return vdmSoulSand, true
			}
			return vdmNetherrack, true
		}
		if c.floorDepth(false) && c.yAbove(31, false) && !c.yAbove(35, true) && c.above(v.gravelLayer, -0.012) {
			if c.yAbove(32, false) || !c.hole() {
				return vdmGravel, true
			}
		}
	}
	return vdmNetherrack, true
}

// vdmIsStone is MaterialSystem.isStone: neither air nor a fluid.
func vdmIsStone(c uint8) bool { return c != vdmAir && c != vdmLava }

// surfaceColumn is MaterialSystem.buildSurface for one column: down from
// the first air over the highest block, counting the stone above (reset by
// air, not by fluid) and below each cell, every stone cell put to the rule.
func (v *vanillaNether) surfaceColumn(t *vdmTerrain, x, z, lx, lz int, biomeAt func(x, y, z int) uint8) {
	height := 0
	for y := vdmH - 1; y >= 0; y-- {
		if t.at(lx, y, lz) != vdmAir {
			height = y + 1
			break
		}
	}
	c := &vdmNRuleCtx{v: v, x: x, z: z, surfaceDepth: v.surfaceDepth(x, z), biomeAt: biomeAt, noise2: map[*vnNormal]float64{}}
	stoneAbove := 0
	nextCeiling := math.MaxInt32
	for y := height; y >= 0; y-- {
		old := t.at(lx, y, lz)
		switch {
		case old == vdmAir:
			stoneAbove = 0
		case old == vdmLava:
		default:
			if nextCeiling >= y {
				nextCeiling = math.MinInt32
				for la := y - 1; la >= -1; la-- {
					if !vdmIsStone(t.at(lx, la, lz)) {
						nextCeiling = la + 1
						break
					}
				}
			}
			stoneAbove++
			c.y, c.stoneAbove, c.stoneBelow, c.biome = y, stoneAbove, y-nextCeiling+1, -1
			if s, ok := c.rule(); ok {
				t.codes[y*256+lz*16+lx] = s
			}
		}
	}
}

// nether_cave: the cave carver configured for the Nether.
const (
	vdmNCarverProbability = 0.2
	vdmNCarverMinY        = 1              // CarvingMask: minGenY + 1
	vdmNCarverMaxY        = vdmH - 1 - 7   // minGenY + genDepth - 1 - 7 protected
	vdmNCarverMaxDistance = (4*2 - 1) * 16 // range 4
	vdmNCarverYTop        = vdmH - 1 - 1   // y: uniform 0 .. below_top 1
)

// vdmNCarverFloor is the constant floor_level -0.7, a float.
var vdmNCarverFloor = float64(float32(-0.7))

// carve runs nether_cave from every start chunk in range and applies the
// mask as applyCarvingMask does: every carved cell but bedrock takes the
// disabled aquifer's answer at density zero — lava under the sea level,
// air above it.
func (v *vanillaNether) carve(t *vdmTerrain) {
	rows := v.g.sections * 16
	mask := make([]uint64, (rows*256+63)/64)
	out := &vcCarve{cx: t.cx, cz: t.cz, mask: mask, minY: vdmNCarverMinY, maxY: vdmNCarverMaxY}
	for dx := int32(-vcCarverRange); dx <= vcCarverRange; dx++ {
		for dz := int32(-vcCarverRange); dz <= vcCarverRange; dz++ {
			sx, sz := t.cx+dx, t.cz+dz
			r := vcLargeFeatureRandom(v.seed, sx, sz)
			if r.nextFloat() <= vdmNCarverProbability {
				vdmNCave(r, sx, sz, out)
			}
		}
	}
	for y := vdmNCarverMinY; y <= vdmNCarverMaxY; y++ {
		for i := 0; i < 256; i++ {
			b := (y-MinY)*256 + i
			if mask[b>>6]&(1<<(b&63)) == 0 || t.codes[y*256+i] == vdmBedrock {
				continue
			}
			if y < vdmNSeaLevel {
				t.codes[y*256+i] = vdmLava
			} else {
				t.codes[y*256+i] = vdmAir
			}
		}
	}
}

// vdmNCave is CaveWorldCarver.carve with nether_cave's configuration:
// up to nine caves biased to few, constant radius multipliers, rooms half
// as tall as wide, tunnels five times as tall to start, thickness a
// trapezoid over 0..6.
func vdmNCave(r *javaRandom, sx, sz int32, out *vcCarve) {
	n := int(r.nextInt(r.nextInt(r.nextInt(9+1)+1) + 1)) // very_biased_to_bottom 0..9
	for cave := 0; cave < n; cave++ {
		x := float64(int(sx)*16 + int(r.nextInt(16)))
		y := float64(r.nextInt(vdmNCarverYTop + 1))
		z := float64(int(sz)*16 + int(r.nextInt(16)))
		tunnels := 1
		if r.nextInt(4) == 0 {
			thick := 1 + r.nextFloat()*6
			hr := 1.5 + float64(mthSin(float64(vcHalfPi))*thick)
			out.ellipsoid(x+1, y, z, hr, hr*0.5, vdmNCarverFloor)
			tunnels += int(r.nextInt(4))
		}
		for i := 0; i < tunnels; i++ {
			hRot := r.nextFloat() * vcTwoPi
			vRot := (r.nextFloat() - 0.5) / 4
			thick := r.nextFloat()*4 + r.nextFloat()*2 // trapezoid 0..6, plateau 2
			dist := vdmNCarverMaxDistance - int(r.nextInt(vdmNCarverMaxDistance/4))
			seed := r.nextLong()
			out.tunnel(seed, x, y, z, 1, 1, thick, hRot, vRot, 0, dist, 5, vdmNCarverFloor)
		}
	}
}

// BlockAt is the generated block before features (VanillaDimension).
func (v *vanillaNether) BlockAt(x, y, z int) uint32 {
	if y < 0 || y >= vdmH {
		return Air
	}
	t := v.terrain(int32(x>>4), int32(z>>4))
	return vdmStates[t.at(x&15, y, z&15)]
}
