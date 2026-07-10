package worldgen

// The End: a floating end-stone island around the origin with the obsidian
// pillar ring, void everywhere else. Same -64..384 canvas as every dimension
// (one chunk pipeline), no sky light. The dragon and crystals are entities —
// the generator only builds the stage.

const (
	EndIslandR    = 95 // main island radius
	EndSurfaceY   = 60
	EndPillars    = 10
	EndPillarRing = 42.0
)

var (
	EndStone       = blockBase("end_stone")
	EndPortalBlock = blockBase("end_portal")
	DragonEgg      = blockBase("dragon_egg")
)

// NewEndGenerator builds a Generator in End mode.
func NewEndGenerator(seed int64) *Generator {
	g := NewGenerator(seed ^ 0xE4D)
	g.end = true
	return g
}

// endPillar returns the pillar index at (x,z), or -1. Pillars sit on a ring,
// radius 3 each, heights varying with the index.
func endPillarAt(x, z int) int {
	for i := 0; i < EndPillars; i++ {
		px := int(EndPillarRing * cos01(float64(i)/EndPillars))
		pz := int(EndPillarRing * sin01(float64(i)/EndPillars))
		dx, dz := x-px, z-pz
		if dx*dx+dz*dz <= 9 {
			return i
		}
	}
	return -1
}

// EndPillarTop is the crystal height for a pillar (varies per pillar).
func EndPillarTop(i int) int { return 76 + (i*7)%28 }

// endBlock assembles one End cell.
func (g *Generator) endBlock(x, y, z int) uint32 {
	if p := endPillarAt(x, z); p >= 0 && y >= EndSurfaceY-8 && y < EndPillarTop(p) {
		return Obsidian
	}
	r := float64(x*x + z*z)
	if r > EndIslandR*EndIslandR {
		return Air
	}
	// Island: a lens — thick in the middle, tapering to the rim, with a
	// noise wobble so the underside isn't a perfect dish.
	rr := sqrtApprox(r)
	depth := (float64(EndIslandR) - rr) * 0.55
	if depth > 26 {
		depth = 26
	}
	wob := g.hills.Noise2(float64(x)/40, float64(z)/40) * 4
	top := float64(EndSurfaceY) + g.detail.Noise2(float64(x)/30, float64(z)/30)*2
	if float64(y) <= top && float64(y) >= top-depth+wob {
		return EndStone
	}
	return Air
}

// generateEndChunk fills a chunk in End mode.
func (g *Generator) generateEndChunk(cx, cz int32) *Chunk {
	ch := &Chunk{}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			for s := 0; s < SectionCount; s++ {
				for ly := 0; ly < 16; ly++ {
					wy := MinY + s*16 + ly
					ch.Sections[s][(ly*16+lz)*16+lx] = g.endBlock(wx, wy, wz)
				}
			}
		}
	}
	for s := 0; s < SectionCount; s++ {
		ch.Biomes[s] = "minecraft:the_end"
	}
	ch.computeHeightmap()
	return ch
}

// sqrtApprox: Newton's method is plenty for island shaping.
func sqrtApprox(v float64) float64 {
	if v <= 0 {
		return 0
	}
	x := v
	for i := 0; i < 8; i++ {
		x = (x + v/x) / 2
	}
	return x
}
