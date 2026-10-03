package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// What the vanilla-mode Nether and End share (vanillanether.go,
// vanillaend.go): the terrain as vanilla's NoiseBasedChunkGenerator fills it
// — the final density sampled at the corners of the router's cells and
// interpolated across them (InterpolatedFunction), squeezed, and a cell the
// density leaves open filled by the global fluid picker — on the true
// vertical range of the dimension's noise settings (both y 0..127). The
// engine's canvas is the overworld's -64..320 in every dimension (one chunk
// pipeline, and the dimension types the gateways declare); in vanilla mode
// the Nether and End sit at their own heights on it, with void below y=0.

// vdH is the noise height of both dimensions (noise.min_y 0, height 128).
const vdH = 128

// vdBlock codes: what one terrain cell holds, before features. The order is
// the oracle's (the bit-exact tests hash these codes).
const (
	vdAir uint8 = iota
	vdNetherrack
	vdLava
	vdBedrock
	vdSoulSand
	vdSoulSoil
	vdBasalt
	vdBlackstone
	vdGravel
	vdWarpedNylium
	vdCrimsonNylium
	vdWarpedWart
	vdNetherWart
	vdEndStone
	vdCodes
)

// vdStates maps a code to its block state (each block's default state, as
// a rule's result_state names it).
var vdStates = [vdCodes]uint32{
	vdAir:           Air,
	vdNetherrack:    blockID("netherrack"),
	vdLava:          blockID("lava"),
	vdBedrock:       blockID("bedrock"),
	vdSoulSand:      blockID("soul_sand"),
	vdSoulSoil:      blockID("soul_soil"),
	vdBasalt:        blockID("basalt"),
	vdBlackstone:    blockID("blackstone"),
	vdGravel:        blockID("gravel"),
	vdWarpedNylium:  blockID("warped_nylium"),
	vdCrimsonNylium: blockID("crimson_nylium"),
	vdWarpedWart:    blockID("warped_wart_block"),
	vdNetherWart:    blockID("nether_wart_block"),
	vdEndStone:      blockID("end_stone"),
}

// vdSlots is the terrain cache's size (a power of two).
const vdSlots = 512

// vdTerrain is one chunk's terrain before features: codes[y*256+lz*16+lx]
// for y 0..127, and the chunk's noise biomes.
type vdTerrain struct {
	cx, cz int32
	codes  [vdH * 256]uint8
	// biome is the noise biome of each quart column (qz*4+qx); both
	// dimensions' biomes are the same at every height.
	biome [16]uint8
}

func (t *vdTerrain) at(lx, y, lz int) uint8 {
	if y < 0 || y >= vdH {
		return vdAir
	}
	return t.codes[y*256+lz*16+lx]
}

// vdCellGrid is the final density's interpolated input sampled at a
// chunk's cell corners: nx×ny×nz values, (i*ny+j)*nz+k.
type vdCellGrid struct {
	nx, ny, nz int
	v          []float32
}

func (c *vdCellGrid) get(i, j, k int) float32 { return c.v[(i*c.ny+j)*c.nz+k] }

// vdInterpolate is InterpolatedFunction.sampleWithBlockStep over a chunk's
// volume (16 × vdH × 16 from (bx, 0, bz)): corner(x, y, z) at every cell
// corner, then each cell filled as fillCell fills it — z, then x, then
// stepping up y by a constant increment. out[y*256+lz*16+lx].
func vdInterpolate(bx, bz, cxz, cy int, corner func(x, y, z int) float32, out *[vdH * 256]float32) {
	g := vdCellGrid{nx: 16/cxz + 1, ny: vdH/cy + 1, nz: 16/cxz + 1}
	g.v = make([]float32, g.nx*g.ny*g.nz)
	for i := 0; i < g.nx; i++ {
		for j := 0; j < g.ny; j++ {
			for k := 0; k < g.nz; k++ {
				g.v[(i*g.ny+j)*g.nz+k] = corner(bx+i*cxz, j*cy, bz+k*cxz)
			}
		}
	}
	invXZ := 1 / float32(cxz)
	invY := 1 / float32(cy)
	for ck := 0; ck < g.nz-1; ck++ {
		for ci := 0; ci < g.nx-1; ci++ {
			for cj := 0; cj < g.ny-1; cj++ {
				v000, v100 := g.get(ci, cj, ck), g.get(ci+1, cj, ck)
				v010, v110 := g.get(ci, cj+1, ck), g.get(ci+1, cj+1, ck)
				v001, v101 := g.get(ci, cj, ck+1), g.get(ci+1, cj, ck+1)
				v011, v111 := g.get(ci, cj+1, ck+1), g.get(ci+1, cj+1, ck+1)
				for z := 0; z < cxz; z++ {
					az := float32(z) * invXZ
					v00 := vnLerp(az, v000, v001)
					v01 := vnLerp(az, v010, v011)
					v10 := vnLerp(az, v100, v101)
					v11 := vnLerp(az, v110, v111)
					lz := ck*cxz + z
					for x := 0; x < cxz; x++ {
						ax := float32(x) * invXZ
						lo := vnLerp(ax, v00, v10)
						hi := vnLerp(ax, v01, v11)
						step := (hi - lo) * invY
						val := lo
						lx := ci*cxz + x
						for y := 0; y < cy; y++ {
							out[(cj*cy+y)*256+lz*16+lx] = val
							val += step
						}
					}
				}
			}
		}
	}
}

// vdLerpConstFirst is LerpFunction.ConstFirstSampler: the constant at
// alpha 0, the input at alpha 1, else Mth.lerp.
func vdLerpConstFirst(alpha, first float32, second func() float32) float32 {
	switch alpha {
	case 0:
		return first
	case 1:
		return second()
	}
	return vnLerp(alpha, first, second())
}

// vdZoomSeed is BiomeManager.obfuscateSeed: the first eight bytes, little
// endian, of the SHA-256 of the seed's eight little-endian bytes.
func vdZoomSeed(seed int64) int64 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h := sha256.Sum256(b[:])
	return int64(binary.LittleEndian.Uint64(h[:8]))
}

func vdLCG(r, c int64) int64 {
	r *= r*6364136223846793005 + 1442695040888963407
	return r + c
}

func vdFiddle(r int64) float64 {
	u := float64(((r>>24)%1024+1024)%1024) / 1024
	return (u - 0.5) * 0.9
}

// vdZoom is BiomeManager.getBiome's corner choice: which of the eight
// quarts around (x, y, z) the block takes its biome from.
func vdZoom(seed int64, x, y, z int) (qx, qy, qz int) {
	ax, ay, az := x-2, y-2, z-2
	px, py, pz := ax>>2, ay>>2, az>>2
	fx, fy, fz := float64(ax&3)/4, float64(ay&3)/4, float64(az&3)/4
	best, bestD := 0, math.Inf(1)
	for i := 0; i < 8; i++ {
		cx, cy, cz := px, py, pz
		dx, dy, dz := fx, fy, fz
		if i&4 != 0 {
			cx, dx = px+1, fx-1
		}
		if i&2 != 0 {
			cy, dy = py+1, fy-1
		}
		if i&1 != 0 {
			cz, dz = pz+1, fz-1
		}
		r := vdLCG(seed, int64(cx))
		r = vdLCG(r, int64(cy))
		r = vdLCG(r, int64(cz))
		r = vdLCG(r, int64(cx))
		r = vdLCG(r, int64(cy))
		r = vdLCG(r, int64(cz))
		fdx := vdFiddle(r)
		r = vdLCG(r, seed)
		fdy := vdFiddle(r)
		r = vdLCG(r, seed)
		fdz := vdFiddle(r)
		d := (dz+fdz)*(dz+fdz) + (dy+fdy)*(dy+fdy) + (dx+fdx)*(dx+fdx)
		if bestD > d {
			best, bestD = i, d
		}
	}
	qx, qy, qz = px, py, pz
	if best&4 != 0 {
		qx++
	}
	if best&2 != 0 {
		qy++
	}
	if best&1 != 0 {
		qz++
	}
	return
}

// vdFill writes a terrain chunk into a canvas chunk at its true heights.
func vdFill(ch *Chunk, t *vdTerrain) {
	for y := 0; y < vdH; y++ {
		s := (y - MinY) >> 4
		if s >= len(ch.Sections) {
			break
		}
		ly := (y - MinY) & 15
		for i := 0; i < 256; i++ {
			if c := t.codes[y*256+i]; c != vdAir {
				ch.Sections[s][ly*256+i] = vdStates[c]
			}
		}
	}
}
