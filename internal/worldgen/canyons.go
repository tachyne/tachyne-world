package worldgen

import "math"

// Ravines: vanilla's canyon carver (configured carver "canyon", in every
// overworld biome). One chunk in a hundred starts a ravine: a long, narrow
// cut that wanders up to seven chunks from its start, 10–67 at its spine,
// three times as tall as it is wide, its walls stepped by a per-height
// width factor. It cuts through the ground to the sky where it runs high.
//
// A ravine spans many chunks, so every chunk pass looks at the start chunks
// within the carver's reach (±8 chunks), draws each ravine whole from its
// start chunk's seed, and carves only its own cells. The draw never reads
// the terrain, so every pass sees the same ravine.
//
// Carving follows the carving mask's rules: bedrock is never cut and a
// fluid is left alone. What fills a cut cell is vanilla's fluid rule
// without its aquifers: lava at the lava level (y ≤ -56), sea water under
// the sea (a ravine crossing an ocean or a river is flooded), air
// elsewhere — except that a cell which would open air beside the sea, or
// flood beside a cave, stays rock, so no ravine leaves a wall of water
// standing against air. Where the cut takes a grass block the dirt under it
// becomes the surface block again.
//
// Build guard: a ravine whose whole envelope (buildguard.go) holds a
// player's build is not carved at all.

const (
	canyonProbability  = 0.01
	canyonRangeChunks  = 8 // start chunks are looked for this far out
	canyonMaxDistance  = (4*2 - 1) * 16
	canyonLavaLevel    = MinY + 8 // lava_level: above_bottom 8
	canyonTopProtected = 7        // cells under the ceiling a carver leaves
	canyonGuardMargin  = 3
	canyonSalt         = 0xCA11_0E5
)

// canyonStep is one ellipsoid the ravine carves.
type canyonStep struct {
	x, y, z, hr, vr float64
}

// canyon is one ravine, drawn whole: the ellipsoids it carves, its
// per-height width factors, and the envelope around them.
type canyon struct {
	steps                  []canyonStep
	width                  []float32 // per y - MinY: the wall's width factor, squared
	x0, y0, z0, x1, y1, z1 int
}

// canyonRNG is the ravine's draw source: a splitmix64 seeded from its start
// chunk, with vanilla's float draws on top.
type canyonRNG struct{ hashRNG }

func (r *canyonRNG) float32() float32 { return float32(r.next()>>40) / (1 << 24) }

// canyonAt draws the ravine started in chunk (scx, scz), or nil.
func (g *Generator) canyonAt(scx, scz int32) *canyon {
	r := &canyonRNG{hashRNG{seed: uint64(g.seed^canyonSalt) ^ uint64(int64(scx))*0x9E3779B97F4A7C15 ^ uint64(int64(scz))*0xC2B2AE3D27D4EB4F}}
	if r.float32() > canyonProbability {
		return nil
	}
	x := float64(int(scx)*16 + r.Intn(16))
	y := float64(10 + r.Intn(67-10+1))
	z := float64(int(scz)*16 + r.Intn(16))
	yaw := r.float32() * math.Pi * 2
	pitch := -0.125 + r.float32()*0.25
	yScale := 3.0
	thickness := r.float32()*4 + r.float32()*2 // trapezoid 0..6, plateau 2
	distance := int(float32(canyonMaxDistance) * (0.75 + r.float32()*0.25))

	c := &canyon{width: make([]float32, g.sections*16)}
	wf := float32(1)
	for i := range c.width {
		if i == 0 || r.Intn(3) == 0 { // width_smoothness 3
			wf = 1 + r.float32()*r.float32()
		}
		c.width[i] = wf * wf
	}
	var yawV, pitchV float32
	sin := func(v float32) float32 { return float32(math.Sin(float64(v))) }
	cos := func(v float32) float32 { return float32(math.Cos(float64(v))) }
	for step := 0; step < distance; step++ {
		hr := 1.5 + float64(sin(float32(step)*math.Pi/float32(distance))*thickness)
		vr := hr * yScale
		hr *= float64(0.75 + r.float32()*0.25) // horizontal_radius_factor
		vr *= float64(0.75 + r.float32()*0.25) // vertical default 1, centre 0
		xc, xs := cos(pitch), sin(pitch)
		x += float64(cos(yaw) * xc)
		y += float64(xs)
		z += float64(sin(yaw) * xc)
		pitch *= 0.7
		pitch += pitchV * 0.05
		yaw += yawV * 0.05
		pitchV *= 0.8
		yawV *= 0.5
		pitchV += (r.float32() - r.float32()) * r.float32() * 2
		yawV += (r.float32() - r.float32()) * r.float32() * 4
		if r.Intn(4) != 0 {
			c.steps = append(c.steps, canyonStep{x, y, z, hr, vr})
		}
	}
	c.x0, c.y0, c.z0 = math.MaxInt32, math.MaxInt32, math.MaxInt32
	c.x1, c.y1, c.z1 = math.MinInt32, math.MinInt32, math.MinInt32
	for _, s := range c.steps {
		c.x0 = min(c.x0, int(math.Floor(s.x-s.hr))-1)
		c.x1 = max(c.x1, int(math.Floor(s.x+s.hr))+1)
		c.y0 = min(c.y0, int(math.Floor(s.y-s.vr))-2) // the dirt under a cut grass block
		c.y1 = max(c.y1, int(math.Floor(s.y+s.vr))+1)
		c.z0 = min(c.z0, int(math.Floor(s.z-s.hr))-1)
		c.z1 = max(c.z1, int(math.Floor(s.z+s.hr))+1)
	}
	return c
}

// carves reports whether ellipsoid s cuts (x, y, z): inside its horizontal
// disc and inside the stepped wall at that height.
func (c *canyon) carves(s canyonStep, x, y, z int) bool {
	xd := (float64(x) + 0.5 - s.x) / s.hr
	zd := (float64(z) + 0.5 - s.z) / s.hr
	if xd*xd+zd*zd >= 1 {
		return false
	}
	yd := (float64(y) - 0.5 - s.y) / s.vr
	i := y - MinY - 1
	if i < 0 || i >= len(c.width) {
		return false
	}
	return (xd*xd+zd*zd)*float64(c.width[i])+yd*yd/6 < 1
}

// markChunk sets the ravine's cells within chunk (cx, cz) in mask
// (index (y-MinY)*256 + lz*16 + lx), between loY and hiY inclusive.
func (c *canyon) markChunk(mask []bool, cx, cz int32, loY, hiY int) {
	bx, bz := int(cx)*16, int(cz)*16
	for _, s := range c.steps {
		x0 := max(int(math.Floor(s.x-s.hr))-1, bx)
		x1 := min(int(math.Floor(s.x+s.hr)), bx+15)
		z0 := max(int(math.Floor(s.z-s.hr))-1, bz)
		z1 := min(int(math.Floor(s.z+s.hr)), bz+15)
		y0 := max(int(math.Floor(s.y-s.vr))-1, loY)
		y1 := min(int(math.Floor(s.y+s.vr))+1, hiY)
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				for y := y1; y > y0; y-- {
					if c.carves(s, x, y, z) {
						mask[(y-MinY)*256+(z-bz)*16+(x-bx)] = true
					}
				}
			}
		}
	}
}

// canyonsTouching lists the ravines whose envelope reaches the box (x0..x1,
// z0..z1) and that the build guard lets through.
func (g *Generator) canyonsTouching(x0, z0, x1, z1 int) []*canyon {
	if g.earth != nil || g.nether || g.end {
		return nil // real terrain is not cut; the other dimensions have no canyon carver
	}
	var out []*canyon
	for scx := int32(floorDiv16(x0)) - canyonRangeChunks; scx <= int32(floorDiv16(x1))+canyonRangeChunks; scx++ {
		for scz := int32(floorDiv16(z0)) - canyonRangeChunks; scz <= int32(floorDiv16(z1))+canyonRangeChunks; scz++ {
			c := g.canyonAt(scx, scz)
			if c == nil || len(c.steps) == 0 || c.x1 < x0 || c.x0 > x1 || c.z1 < z0 || c.z0 > z1 {
				continue
			}
			if g.builtIn(c.x0-canyonGuardMargin, c.y0-canyonGuardMargin, c.z0-canyonGuardMargin,
				c.x1+canyonGuardMargin, c.y1+canyonGuardMargin, c.z1+canyonGuardMargin) {
				continue
			}
			out = append(out, c)
		}
	}
	return out
}

// cuts reports whether the ravine's mask holds (x, y, z).
func (c *canyon) cuts(x, y, z int) bool {
	for _, s := range c.steps {
		if y > int(math.Floor(s.y-s.vr))-1 && y <= int(math.Floor(s.y+s.vr))+1 && c.carves(s, x, y, z) {
			return true
		}
	}
	return false
}

// ravineCut answers, for the box around a chunk, whether a ravine cuts a
// cell — for surface features rooted on the terrain model, which must not
// stand over a ravine's open top (the answer is the same in every pass).
func (g *Generator) ravineCut(x0, z0, x1, z1 int) func(x, y, z int) bool {
	cs := g.canyonsTouching(x0, z0, x1, z1)
	return func(x, y, z int) bool {
		for _, c := range cs {
			if x >= c.x0 && x <= c.x1 && z >= c.z0 && z <= c.z1 && y >= c.y0 && y <= c.y1 && c.cuts(x, y, z) {
				return true
			}
		}
		return false
	}
}

// carveCanyons cuts every ravine that reaches this chunk.
func (g *Generator) carveCanyons(ch *Chunk, cx, cz int32) {
	bx, bz := int(cx)*16, int(cz)*16
	cs := g.canyonsTouching(bx, bz, bx+15, bz+15)
	if len(cs) == 0 {
		return
	}
	loY, hiY := MinY+1, MinY+len(ch.Sections)*16-1-canyonTopProtected
	mask := make([]bool, len(ch.Sections)*16*256)
	for _, c := range cs {
		c.markChunk(mask, cx, cz, loY, hiY)
	}
	g.applyCanyonMask(ch, cx, cz, mask, loY, hiY)
}

// applyCanyonMask fills the marked cells, column by column from the top.
func (g *Generator) applyCanyonMask(ch *Chunk, cx, cz int32, mask []bool, loY, hiY int) {
	bx, bz := int(cx)*16, int(cz)*16
	var heights [18 * 18]int
	for i := range heights {
		heights[i] = math.MinInt32
	}
	height := func(x, z int) int { // the terrain model's surface, this chunk and a ring
		i := (x-bx+1)*18 + (z - bz + 1)
		if heights[i] == math.MinInt32 {
			heights[i] = g.Height(x, z)
		}
		return heights[i]
	}
	caveAir := func(x, y, z int) bool { // a noise cave in the terrain model
		h := height(x, z)
		return y < h && g.carve(Stone, x, y, z, h) == Air
	}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			x, z := bx+lx, bz+lz
			grass := false
			var top uint32
			for y := hiY; y >= loY; y-- {
				if !mask[(y-MinY)*256+lz*16+lx] {
					continue
				}
				s := sectionBlockAt(ch, lx, y, lz)
				if s == Bedrock || s == Air || IsFluid(s) {
					continue
				}
				if s == GrassBlock || s == Mycelium {
					grass, top = true, s
				}
				fill := Air
				switch {
				case y <= canyonLavaLevel:
					fill = Lava
				case y < SeaLevel && height(x, z) < SeaLevel:
					// Under the sea: flooded, unless it would open onto a cave.
					fill = Water
					if caveAir(x, y-1, z) || caveAir(x+1, y, z) || caveAir(x-1, y, z) || caveAir(x, y, z+1) || caveAir(x, y, z-1) {
						continue
					}
				case y < SeaLevel:
					// Dry land below sea level: rock stays beside the sea.
					if height(x+1, z) < SeaLevel || height(x-1, z) < SeaLevel || height(x, z+1) < SeaLevel || height(x, z-1) < SeaLevel {
						continue
					}
				}
				setSectionBlock(ch, lx, y, lz, fill, true)
				if grass && fill == Air && y-1 >= MinY && sectionBlockAt(ch, lx, y-1, lz) == Dirt {
					setSectionBlock(ch, lx, y-1, lz, top, true)
				}
			}
		}
	}
}
