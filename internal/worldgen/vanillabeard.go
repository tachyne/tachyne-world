package worldgen

import (
	"math"
	"sync"
)

// The Beardifier (26.3's levelgen.Beardifier) for the vanilla generator:
// the density the terrain gains or loses around the structures that declare
// a terrain adaptation, added to final_density before any block is chosen
// (the "beardifier" term of the noise settings). The pieces and junctions
// come from the placement pass (VanillaBeards); without one the term is 0.

const vbBeardRadius = 12

var (
	vbKernelOnce sync.Once
	vbKernel     []float32
)

// vbBeardKernel is BEARD_KERNEL: exp(-d²/16) over a 24³ box, the y offset
// by half a block, indexed z, x, y.
func vbBeardKernel() []float32 {
	vbKernelOnce.Do(func() {
		vbKernel = make([]float32, 24*24*24)
		for zi := 0; zi < 24; zi++ {
			for xi := 0; xi < 24; xi++ {
				for yi := 0; yi < 24; yi++ {
					dx, dy, dz := float64(xi-12), float64(yi-12)+0.5, float64(zi-12)
					vbKernel[zi*24*24+xi*24+yi] = float32(math.Pow(math.E, -(dx*dx+dy*dy+dz*dz)/16.0))
				}
			}
		}
	})
	return vbKernel
}

// vbFastInvSqrt is Mth.fastInvSqrt.
func vbFastInvSqrt(x float64) float64 {
	half := 0.5 * x
	i := int64(math.Float64bits(x))
	i = 6910469410427058090 - i>>1
	x = math.Float64frombits(uint64(i))
	return x * (1.5 - half*x*x)
}

func vbBury(dx, dy, dz float32) float32 {
	d := dx*dx + dy*dy + dz*dz
	if d >= 36 {
		return 0
	}
	return 1 - float32(math.Sqrt(float64(d)))/6
}

func vbBeard(dx, dy, dz, toGround int) float32 {
	xi, yi, zi := dx+12, dy+12, dz+12
	if xi < 0 || xi >= 24 || yi < 0 || yi >= 24 || zi < 0 || zi >= 24 {
		return 0
	}
	dyo := float32(toGround) + 0.5
	d := float32(dx)*float32(dx) + dyo*dyo + float32(dz)*float32(dz)
	v := -dyo * float32(vbFastInvSqrt(float64(d/2))) / 2
	return v * vbBeardKernel()[zi*24*24+xi*24+yi]
}

// vtBeardifier is a chunk's Beardifier.
type vtBeardifier struct {
	pieces    []VanillaBeardPiece
	junctions []VanillaJunction
	box       [6]int // the affected box: the pieces and junctions, inflated 24
}

// newVTBeardifier is forStructuresInChunk's result, or nil for none.
func newVTBeardifier(pieces []VanillaBeardPiece, junctions []VanillaJunction) *vtBeardifier {
	if len(pieces) == 0 && len(junctions) == 0 {
		return nil
	}
	b := &vtBeardifier{pieces: pieces, junctions: junctions}
	first := true
	add := func(x0, y0, z0, x1, y1, z1 int) {
		if first {
			b.box = [6]int{x0, y0, z0, x1, y1, z1}
			first = false
			return
		}
		b.box = [6]int{min(b.box[0], x0), min(b.box[1], y0), min(b.box[2], z0), max(b.box[3], x1), max(b.box[4], y1), max(b.box[5], z1)}
	}
	for _, p := range pieces {
		add(p.MinX, p.MinY, p.MinZ, p.MaxX, p.MaxY, p.MaxZ)
	}
	for _, j := range junctions {
		add(j.X, j.GroundY, j.Z, j.X, j.GroundY, j.Z)
	}
	for i := 0; i < 3; i++ {
		b.box[i] -= 24
		b.box[i+3] += 24
	}
	return b
}

func (b *vtBeardifier) inside(x, y, z int) bool {
	return x >= b.box[0] && x <= b.box[3] && y >= b.box[1] && y <= b.box[4] && z >= b.box[2] && z <= b.box[5]
}

func (b *vtBeardifier) unchecked(x, y, z int) float32 {
	var v float32
	for _, p := range b.pieces {
		dx := max(0, max(p.MinX-x, x-p.MaxX))
		dz := max(0, max(p.MinZ-z, z-p.MaxZ))
		groundY := p.MinY + p.GroundLevelDelta
		toGround := y - groundY
		var dy int
		switch p.Adjust {
		case AdjustBury, AdjustBeardThin:
			dy = toGround
		case AdjustBeardBox:
			dy = max(0, max(groundY-y, y-p.MaxY))
		case AdjustEncapsulate:
			dy = max(0, max(p.MinY-y, y-p.MaxY))
		}
		switch p.Adjust {
		case AdjustBury:
			v += vbBury(float32(dx), float32(dy)/2, float32(dz))
		case AdjustBeardThin, AdjustBeardBox:
			v += vbBeard(dx, dy, dz, toGround) * 0.8
		case AdjustEncapsulate:
			v += vbBury(float32(dx)/2, float32(dy)/2, float32(dz)/2) * 0.8
		}
	}
	for _, j := range b.junctions {
		dx, dy, dz := x-j.X, y-j.GroundY, z-j.Z
		v += vbBeard(dx, dy, dz, dy) * 0.4
	}
	return v
}

func (b *vtBeardifier) value(_ *vdCtx, x, y, z int) float32 {
	if b.inside(x, y, z) {
		return b.unchecked(x, y, z)
	}
	return 0
}

func (b *vtBeardifier) volume(_ *vdCtx, out []float32, v vtVolume) {
	for i := range out {
		out[i] = 0
	}
	if b.box[3] < v.minX || b.box[0] > v.maxX() || b.box[5] < v.minZ || b.box[2] > v.maxZ() || b.box[4] < v.minY || b.box[1] > v.maxY() {
		return
	}
	minX := floorDiv(max(0, b.box[0]-v.minX), v.stepX)
	minY := floorDiv(max(0, b.box[1]-v.minY), v.stepY)
	minZ := floorDiv(max(0, b.box[2]-v.minZ), v.stepZ)
	maxX := min(v.sx-1, floorDiv(b.box[3]-v.minX, v.stepX))
	maxY := min(v.sy-1, floorDiv(b.box[4]-v.minY, v.stepY))
	maxZ := min(v.sz-1, floorDiv(b.box[5]-v.minZ, v.stepZ))
	for z := minZ; z <= maxZ; z++ {
		bz := v.blockZ(z)
		for x := minX; x <= maxX; x++ {
			bx := v.blockX(x)
			for y := minY; y <= maxY; y++ {
				out[v.index(x, y, z)] = b.unchecked(bx, v.blockY(y), bz)
			}
		}
	}
}
