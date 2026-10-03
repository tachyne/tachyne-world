package worldgen

import (
	"math"
)

// Vanilla's aquifers (Aquifer.NoiseBasedAquifer, 26.3) for a vanilla-caves
// world: what fills a cell the caves open. The world is cut into aquifer
// cells 16×12×16 blocks, each with a centre placed at random inside it
// (the "minecraft:aquifer" positional random) and a fluid status — a fluid
// level and a type — worked out at that centre from the terrain's
// preliminary surface around it, the floodedness and spread noises, and
// the lava noise; a cell takes the status of the nearest centre, and where
// two centres with different statuses are about equally near, the barrier
// noise and a pressure term decide whether a wall of rock stands between
// them. Below y=-54 the global fluid rule makes every open cell lava; near
// and under the sea the statuses take the sea's level, so caves under the
// ocean flood.
//
// The aquifer reads two things vanilla takes from its own terrain noise,
// which the engine's terrain (a per-column height) does not have; they come
// from the engine's column instead (vanillaCaves.surfaceInputs):
//
//   - surface_level, overworld/preliminary_surface_level: vanilla searches
//     down from an upper bound in steps of eight for the first y where the
//     terrain's depth term, without the 3-D noise, is positive
//     (FindTopSurfaceFunction). It is computed the same way here over the
//     same depth model the caves use (vanillacaves.go: depth (h−y)/128 at
//     the engine's surface h, times the terrain factor) — which works out at
//     the highest multiple of eight at least 35/factor blocks under h (six
//     blocks at the factor's 6.3), as vanilla's is under its own surface;
//   - exclusion, the deep dark region (erosion below -0.225 and depth over
//     0.9): no water or lava fills a cell whose centre lies in the engine's
//     own deep_dark (caveBiomeAt), which is where the engine puts that biome.
//
// What the aquifer does not do here: vanilla marks a fluid cell whose
// neighbourhood is unsettled (shouldScheduleFluidUpdate) for a fluid tick
// once the chunk is loaded, so it can flow; the engine has no such pass for
// generated fluids, so every aquifer fluid is a still source.

// vaqStatus is Aquifer.FluidStatus: below level, the fluid (water or lava);
// at and above, air.
type vaqStatus struct {
	level int32
	lava  bool
}

// What a cell holds: computeSubstance's answer.
const (
	vaqSolid int8 = -1 // null: the noise's stone stays
	vaqAir   int8 = 0
	vaqWater int8 = 1
	vaqLava  int8 = 2
)

func (s vaqStatus) at(y int) int8 {
	if y < int(s.level) {
		if s.lava {
			return vaqLava
		}
		return vaqWater
	}
	return vaqAir
}

// vaqWayBelow is DimensionType.WAY_BELOW_MIN_Y: a status with no fluid.
const vaqWayBelow = -32512

// vaqGlobal is the overworld's global fluid picker
// (NoiseBasedChunkGenerator.createFluidPicker): lava up to y=-54, the sea's
// water up to sea level above it.
func vaqGlobal(y int) vaqStatus {
	if y < min(vanillaLavaLevel, SeaLevel) {
		return vaqStatus{vanillaLavaLevel, true}
	}
	return vaqStatus{SeaLevel, false}
}

func vaqSimilarity(d1, d2 int) float64 {
	return 1.0 - float64(d2-d1)/25.0
}

// vaqSurfaceOffsets is SURFACE_SAMPLING_OFFSETS_IN_CHUNKS.
var vaqSurfaceOffsets = [13][2]int{
	{0, 0}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-3, 0}, {-2, 0}, {-1, 0}, {1, 0}, {-2, 1}, {-1, 1}, {0, 1}, {1, 1},
}

var vaqNoiseParams = map[string]vnNoiseParams{
	"aquifer_barrier":                 {baseOctave: -3, baseAmp: 0.955388882960065},
	"aquifer_fluid_level_floodedness": {baseOctave: -7, baseAmp: 0.955388882960065},
	"aquifer_fluid_level_spread":      {baseOctave: -5, baseAmp: 0.955388882960065},
	"aquifer_lava":                    {baseOctave: -1, baseAmp: 0.955388882960065},
}

// vanillaAquifer is a world's aquifer noises and positional random.
type vanillaAquifer struct {
	random                       vnPositional // RandomState's "minecraft:aquifer" factory
	barrier, flood, spread, lava *vnNormal
}

func newVanillaAquifer(seed int64) *vanillaAquifer {
	w := vnWorldPositional(seed)
	n := func(name string) *vnNormal { return newVNNormal(w, "minecraft:"+name, vaqNoiseParams[name]) }
	return &vanillaAquifer{
		random:  w.fromHashOf("minecraft:aquifer").forkPositional(),
		barrier: n("aquifer_barrier"),
		flood:   n("aquifer_fluid_level_floodedness"),
		spread:  n("aquifer_fluid_level_spread"),
		lava:    n("aquifer_lava"),
	}
}

// at is XoroshiroPositionalRandomFactory.at.
func (p vnPositional) at(x, y, z int32) *vnXoroshiro {
	return newVNXoroshiro(uint64(mthGetSeed(x, y, z))^p.lo, p.hi)
}

// mthGetSeed is Mth.getSeed(x, y, z).
func mthGetSeed(x, y, z int32) int64 {
	s := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	s = s*s*42317861 + s*11
	return s >> 16
}

func vaqGridX(b int) int        { return b >> 4 }
func vaqGridY(b int) int        { return floorDiv(b, 12) }
func vaqFromGridX(g, o int) int { return g<<4 + o }
func vaqFromGridY(g, o int) int { return g*12 + o }

// vaqChunk is one chunk's aquifer (vanilla makes one per NoiseChunk): the
// grid it covers, its caches, and the inputs it reads.
type vaqChunk struct {
	a *vanillaAquifer
	// surface is preliminary_surface_level at a quart-aligned column, floored.
	surface func(x, z int) int
	// excluded is the exclusion function's "> 0" at an aquifer centre.
	excluded func(x, y, z int) bool

	minGX, minGY, minGZ int
	sizeX, sizeZ        int
	skipAbove           int

	loc     [][3]int32
	locSet  []bool
	status  []vaqStatus
	statSet []bool
	surf    map[[2]int]int
}

// newVaqChunk is NoiseBasedAquifer's constructor over the block box
// [minX..maxX]×[minY..maxY]×[minZ..maxZ].
func newVaqChunk(a *vanillaAquifer, minX, minY, minZ, maxX, maxY, maxZ int,
	surface func(x, z int) int, excluded func(x, y, z int) bool) *vaqChunk {
	c := &vaqChunk{a: a, surface: surface, excluded: excluded, surf: map[[2]int]int{}}
	c.minGX = vaqGridX(minX-5) + 0
	maxGX := vaqGridX(maxX-5) + 1
	c.sizeX = maxGX - c.minGX + 1
	c.minGY = vaqGridY(minY+1) - 1
	maxGY := vaqGridY(maxY+1) + 1
	sizeY := maxGY - c.minGY + 1
	c.minGZ = vaqGridX(minZ-5) + 0
	maxGZ := vaqGridX(maxZ-5) + 1
	c.sizeZ = maxGZ - c.minGZ + 1
	n := c.sizeX * sizeY * c.sizeZ
	c.loc = make([][3]int32, n)
	c.locSet = make([]bool, n)
	c.status = make([]vaqStatus, n)
	c.statSet = make([]bool, n)
	maxAdj := c.maxSurfaceLevel(vaqFromGridX(c.minGX, 0), vaqFromGridX(c.minGZ, 0), vaqFromGridX(maxGX, 9), vaqFromGridX(maxGZ, 9)) + 8
	skipGY := vaqGridY(maxAdj+12) - -1
	c.skipAbove = vaqFromGridY(skipGY, 11) - 1
	return c
}

// surfaceLevel is the preliminary surface at the column's quart.
func (c *vaqChunk) surfaceLevel(x, z int) int {
	k := [2]int{x >> 2 << 2, z >> 2 << 2}
	s, ok := c.surf[k]
	if !ok {
		s = c.surface(k[0], k[1])
		c.surf[k] = s
	}
	return s
}

func (c *vaqChunk) maxSurfaceLevel(minX, minZ, maxX, maxZ int) int {
	m := math.MinInt32
	for qz := minZ >> 2; qz <= maxZ>>2; qz++ {
		for qx := minX >> 2; qx <= maxX>>2; qx++ {
			m = max(m, c.surfaceLevel(qx<<2, qz<<2))
		}
	}
	return m
}

func (c *vaqChunk) index(gx, gy, gz int) int {
	return ((gy-c.minGY)*c.sizeZ+(gz-c.minGZ))*c.sizeX + gx - c.minGX
}

// substance is computeSubstance: what the cell holds given its final
// density (vaqSolid where the noise's stone stays).
func (c *vaqChunk) substance(x, y, z int, density float64) int8 {
	if density > 0 {
		return vaqSolid
	}
	global := vaqGlobal(y)
	if y > c.skipAbove {
		return global.at(y)
	}
	if global.at(y) == vaqLava {
		return vaqLava
	}
	xA, yA, zA := vaqGridX(x-5), vaqGridY(y+1), vaqGridX(z-5)
	// The three nearest centres (vanilla keeps a fourth only to decide the
	// fluid-tick flag, which the engine has no use for).
	d1, d2, d3 := math.MaxInt32, math.MaxInt32, math.MaxInt32
	i1, i2, i3 := 0, 0, 0
	for x1 := 0; x1 <= 1; x1++ {
		for y1 := -1; y1 <= 1; y1++ {
			for z1 := 0; z1 <= 1; z1++ {
				gx, gy, gz := xA+x1, yA+y1, zA+z1
				i := c.index(gx, gy, gz)
				if !c.locSet[i] {
					r := c.a.random.at(int32(gx), int32(gy), int32(gz))
					lx := vaqFromGridX(gx, int(r.nextInt(10)))
					ly := vaqFromGridY(gy, int(r.nextInt(9)))
					lz := vaqFromGridX(gz, int(r.nextInt(10)))
					c.loc[i] = [3]int32{int32(lx), int32(ly), int32(lz)}
					c.locSet[i] = true
				}
				l := c.loc[i]
				dx, dy, dz := int(l[0])-x, int(l[1])-y, int(l[2])-z
				d := dx*dx + dy*dy + dz*dz
				switch {
				case d1 >= d:
					i3, i2, i1 = i2, i1, i
					d3, d2, d1 = d2, d1, d
				case d2 >= d:
					i3, i2 = i2, i
					d3, d2 = d2, d
				case d3 >= d:
					i3, d3 = i, d
				}
			}
		}
	}
	s1 := c.statusAt(i1)
	sim12 := vaqSimilarity(d1, d2)
	fluid := s1.at(y)
	if sim12 <= 0 {
		return fluid
	}
	if fluid == vaqWater && vaqGlobal(y-1).at(y-1) == vaqLava {
		return fluid
	}
	barrier := math.NaN()
	s2 := c.statusAt(i2)
	// (The products are converted before the sum so no platform fuses them
	// into a multiply-add vanilla does not do.)
	if density+float64(sim12*c.pressure(x, y, z, &barrier, s1, s2)) > 0 {
		return vaqSolid
	}
	s3 := c.statusAt(i3)
	if sim13 := vaqSimilarity(d1, d3); sim13 > 0 {
		if density+float64(sim12*sim13*c.pressure(x, y, z, &barrier, s1, s3)) > 0 {
			return vaqSolid
		}
	}
	if sim23 := vaqSimilarity(d2, d3); sim23 > 0 {
		if density+float64(sim12*sim23*c.pressure(x, y, z, &barrier, s2, s3)) > 0 {
			return vaqSolid
		}
	}
	return fluid
}

// pressure is calculatePressure: the barrier between two statuses at y.
func (c *vaqChunk) pressure(x, y, z int, barrier *float64, s1, s2 vaqStatus) float64 {
	t1, t2 := s1.at(y), s2.at(y)
	if (t1 == vaqLava && t2 == vaqWater) || (t1 == vaqWater && t2 == vaqLava) {
		return 2.0
	}
	diff := s1.level - s2.level
	if diff < 0 {
		diff = -diff
	}
	if diff == 0 {
		return 0.0
	}
	avg := 0.5 * float64(s1.level+s2.level)
	above := float64(y) + 0.5 - avg
	base := float64(diff) / 2.0
	toMiddle := base - math.Abs(above)
	var gradient float64
	if above > 0.0 {
		if centre := 0.0 + toMiddle; centre > 0.0 {
			gradient = centre / 1.5
		} else {
			gradient = centre / 2.5
		}
	} else {
		if centre := 3.0 + toMiddle; centre > 0.0 {
			gradient = centre / 3.0
		} else {
			gradient = centre / 10.0
		}
	}
	noise := 0.0
	if !(gradient < -2.0) && !(gradient > 2.0) {
		if math.IsNaN(*barrier) {
			*barrier = float64(vcSample(c.a.barrier, x, y, z, 1, 0.5))
		}
		noise = *barrier
	}
	return 2.0 * (noise + gradient)
}

func (c *vaqChunk) statusAt(i int) vaqStatus {
	if !c.statSet[i] {
		l := c.loc[i]
		c.status[i] = c.computeFluid(int(l[0]), int(l[1]), int(l[2]))
		c.statSet[i] = true
	}
	return c.status[i]
}

// computeFluid is the status at an aquifer centre.
func (c *vaqChunk) computeFluid(x, y, z int) vaqStatus {
	global := vaqGlobal(y)
	lowest := math.MaxInt32
	top, bottom := y+12, y-12
	underGlobal := false
	for _, o := range vaqSurfaceOffsets {
		sx, sz := x+o[0]*16, z+o[1]*16
		surface := c.surfaceLevel(sx, sz)
		adjusted := surface + 8
		start := o[0] == 0 && o[1] == 0
		if start && bottom > adjusted {
			return global
		}
		pokes := top > adjusted
		if pokes || start {
			if atSurface := vaqGlobal(adjusted); atSurface.at(adjusted) != vaqAir {
				if start {
					underGlobal = true
				}
				if pokes {
					return atSurface
				}
			}
		}
		lowest = min(lowest, surface)
	}
	level := c.computeSurfaceLevel(x, y, z, global, lowest, underGlobal)
	return vaqStatus{int32(level), c.computeFluidType(x, y, z, global, level)}
}

func (c *vaqChunk) computeSurfaceLevel(x, y, z int, global vaqStatus, lowest int, underGlobal bool) int {
	var partially, fully float64
	if c.excluded(x, y, z) {
		partially, fully = -1.0, -1.0
	} else {
		below := lowest + 8 - y
		factor := 0.0
		if underGlobal {
			factor = mthClampedMap(float64(below), 0.0, 64.0, 1.0, 0.0)
		}
		noise := mthClamp(float64(vcSample(c.a.flood, x, y, z, 1, 0.67)), -1.0, 1.0)
		fullyThreshold := mthMap(factor, 1.0, 0.0, -0.3, 0.8)
		partiallyThreshold := mthMap(factor, 1.0, 0.0, -0.8, 0.4)
		partially = noise - partiallyThreshold
		fully = noise - fullyThreshold
	}
	switch {
	case fully > 0.0:
		return int(global.level)
	case partially > 0.0:
		return c.randomizedLevel(x, y, z, lowest)
	}
	return vaqWayBelow
}

// randomizedLevel is computeRandomizedFluidSurfaceLevel.
func (c *vaqChunk) randomizedLevel(x, y, z, lowest int) int {
	cx, cy, cz := floorDiv(x, 16), floorDiv(y, 40), floorDiv(z, 16)
	middle := cy*40 + 20
	spread := float64(vcSample(c.a.spread, cx, cy, cz, 1, 0.7142857142857143) * 10.0)
	quantized := int(math.Floor(spread/3)) * 3
	return min(lowest, middle+quantized)
}

// computeFluidType: deep enough, the lava noise makes a level lava.
func (c *vaqChunk) computeFluidType(x, y, z int, global vaqStatus, level int) bool {
	if level <= -10 && level != vaqWayBelow && !global.lava {
		v := vcSample(c.a.lava, floorDiv(x, 64), floorDiv(y, 40), floorDiv(z, 64), 1, 1)
		if math.Abs(float64(v)) > 0.3 {
			return true
		}
	}
	return global.lava
}

// Mth's double helpers, as written.
func mthClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	return math.Min(v, hi)
}

func mthLerp(a, p0, p1 float64) float64 { return p0 + a*(p1-p0) }

func mthMap(v, fromMin, fromMax, toMin, toMax float64) float64 {
	return mthLerp((v-fromMin)/(fromMax-fromMin), toMin, toMax)
}

func mthClampedMap(v, fromMin, fromMax, toMin, toMax float64) float64 {
	f := (v - fromMin) / (fromMax - fromMin)
	if f < 0.0 {
		return toMin
	}
	if f > 1.0 {
		return toMax
	}
	return mthLerp(f, toMin, toMax)
}
