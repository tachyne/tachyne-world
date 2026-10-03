package worldgen

import (
	"math"
	"sync/atomic"
)

// The End in vanilla mode: 26.3's noise_settings/end.json on y 0..127 —
// end/sloped_cheese (end/islands, the main island's distance falloff from
// the origin maxed with the outer islands of EndIslandFunction, plus the old
// blended noise) slid to -23.4375 high up and -0.234375 at the floor,
// interpolated over 8×4×8 cells and squeezed, end stone where it is
// positive and nothing else (the End has no fluid and its surface rule is
// end stone throughout); and TheEndBiomeSource's rings, which read the same
// islands density at each chunk's middle. Seeded as a legacy world, so a
// seed's End is vanilla's, block for block.

// The End's biomes.
const (
	vdmEEnd uint8 = iota
	vdmEHighlands
	vdmEMidlands
	vdmEIslands
	vdmEBarrens
)

var vdmENames = [...]string{
	"minecraft:the_end", "minecraft:end_highlands", "minecraft:end_midlands",
	"minecraft:small_end_islands", "minecraft:end_barrens",
}

// vanillaEnd is one seed's vanilla End.
type vanillaEnd struct {
	seed   int64
	g      *Generator
	base   *vdmBlended
	island *vdmSimplex
	place  VanillaPlacement // the placement pass, nil = the engine's own features
	slots  [vdmSlots]atomic.Pointer[vdmTerrain]
}

func newVanillaEnd(seed int64, g *Generator) *vanillaEnd {
	r := newVDMLegacy(seed) // EndIslandFunction: LegacyRandomSource(seed), 17292 draws in
	r.consume(17292)
	return &vanillaEnd{
		seed:   seed,
		g:      g,
		base:   newVDMBlended(seed, 0.25, 0.25, 80, 160, 4),
		island: &vdmSimplex{newVDMPerlin(r, 0)},
	}
}

// outerHeight is EndIslandFunction.getHeightValue: the outer islands' height
// value at an 8-block section, the most any island in the 25×25 chunks
// around reaches there (-100 when none does). Java's truncating division and
// remainder split the section into chunk and half.
func (v *vanillaEnd) outerHeight(sectionX, sectionZ int) float32 {
	chunkX, chunkZ := sectionX/2, sectionZ/2
	subX, subZ := sectionX%2, sectionZ%2
	doffs := float32(-100)
	for xo := -12; xo <= 12; xo++ {
		for zo := -12; zo <= 12; zo++ {
			tx, tz := int64(chunkX+xo), int64(chunkZ+zo)
			if tx*tx+tz*tz <= 4096 || v.island.get2(float64(tx), float64(tz)) >= -0.9 {
				continue
			}
			ax, az := vcAbs(float32(tx)), vcAbs(float32(tz))
			size := vdmFmod32(float32(ax*3439)+float32(az*147), 13) + 9
			xd := float32(subX - xo*2)
			zd := float32(subZ - zo*2)
			d := 100 - vdmSqrt32(float32(xd*xd)+float32(zd*zd))*size
			d = vcClamp(d, -100, 80)
			doffs = vcMax(doffs, d)
		}
	}
	return doffs
}

// vdmFmod32 is Java's float remainder (exact, so the double one rounds back).
func vdmFmod32(a, b float32) float32 { return float32(math.Mod(float64(a), float64(b))) }

// vdmSqrt32 is Mth.sqrt(float): the double root of the float, rounded.
func vdmSqrt32(v float32) float32 { return float32(math.Sqrt(float64(v))) }

// islands is end/islands at a column (sliced at y=0): the main island's
// falloff, (clamp(100 - distance, -100, 80) - 8) / 128, maxed with the outer
// islands' height value scaled the same way.
func (v *vanillaEnd) islands(x, z int) float32 {
	dx, dz := float32(0-x), float32(0-z)
	dist := vdmSqrt32(float32(float32(dx*dx)+0) + float32(dz*dz))
	left := vcClamp(100-dist, -100, 80)
	left = (left + -8) * 0.0078125
	if left >= 0.5625 { // MaxSampler: the outer islands' range tops out here
		return left
	}
	right := (v.outerHeight(x/8, z/8) - 8) / 128
	return vcMax(left, right)
}

// corner is final_density's interpolated input at a cell corner.
func (v *vanillaEnd) corner(x, y, z int) float32 { return v.cornerWith(v.islands(x, z), x, y, z) }

// cornerWith is corner given the column's islands density (end/islands is
// cached per column in vanilla; it does not vary with y).
func (v *vanillaEnd) cornerWith(islands float32, x, y, z int) float32 {
	inner := func() float32 {
		return vdmLerpConstFirst(vcGradient(y, 56, 312, 1, 0), -23.4375, func() float32 {
			return islands + v.base.sample(x, y, z)
		})
	}
	return vdmLerpConstFirst(vcGradient(y, 4, 32, 0, 1), -0.234375, inner) * 0.64
}

// chunkBiome is TheEndBiomeSource.getNoiseBiome for a chunk: the_end within
// 64 chunks of the origin, else the ring the islands density at the chunk's
// middle falls in. Every quart of a chunk has the same biome.
func (v *vanillaEnd) chunkBiome(cx, cz int32) uint8 {
	if int64(cx)*int64(cx)+int64(cz)*int64(cz) <= 4096 {
		return vdmEEnd
	}
	h := float64(v.islands((int(cx)*2+1)*8, (int(cz)*2+1)*8))
	switch {
	case h > 0.25:
		return vdmEHighlands
	case h >= -0.0625:
		return vdmEMidlands
	case h < -0.21875:
		return vdmEIslands
	}
	return vdmEBarrens
}

// BiomeAt is the noise biome at a quart (VanillaBiomes).
func (v *vanillaEnd) BiomeAt(qx, qy, qz int) string {
	return vdmENames[v.chunkBiome(int32(qx>>2), int32(qz>>2))]
}

// terrain is chunk (cx, cz)'s terrain, from the cache or built.
func (v *vanillaEnd) terrain(cx, cz int32) *vdmTerrain {
	slot := &v.slots[(uint32(cx)*0x9E3779B1^uint32(cz)*0x85EBCA77)&(vdmSlots-1)]
	if t := slot.Load(); t != nil && t.cx == cx && t.cz == cz {
		return t
	}
	t := v.build(cx, cz)
	slot.Store(t)
	return t
}

// build is doFill for one End chunk (its surface rule and the absent
// carvers change nothing).
func (v *vanillaEnd) build(cx, cz int32) *vdmTerrain {
	t := &vdmTerrain{cx: cx, cz: cz}
	var dens [vdmH * 256]float32
	bx, bz := int(cx)*16, int(cz)*16
	var cols [3][3]float32 // the islands density at the corner columns
	for i := range cols {
		for k := range cols[i] {
			cols[i][k] = v.islands(bx+i*8, bz+k*8)
		}
	}
	vdmInterpolate(bx, bz, 8, 4, func(x, y, z int) float32 {
		return v.cornerWith(cols[(x-bx)/8][(z-bz)/8], x, y, z)
	}, &dens)
	for i, d := range dens {
		if vcSqueeze(d) > 0 {
			t.codes[i] = vdmEndStone
		}
	}
	b := v.chunkBiome(cx, cz)
	for i := range t.biome {
		t.biome[i] = b
	}
	return t
}

// BlockAt is the generated terrain before features.
func (v *vanillaEnd) BlockAt(x, y, z int) uint32 {
	if y < 0 || y >= vdmH {
		return Air
	}
	t := v.terrain(int32(x>>4), int32(z>>4))
	return vdmStates[t.at(x&15, y, z&15)]
}
