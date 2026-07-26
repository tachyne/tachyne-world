package worldgen

import "math"

// The outer End islands — everything beyond the 1000-block void ring around
// the main island. This is where the End actually opens up: chorus forests,
// end cities, and the only way anyone gets elytra.
//
// The PLACEMENT is vanilla's, transcribed from EndIslandDensityFunction: a
// 25x25 search of nearby chunk cells, each qualifying cell seeding a circular
// plate whose size comes from an integer hash of its coordinates. That search
// is what gives the End its characteristic scatter of separate discs rather
// than a continuous landscape, so it is the part worth getting exactly right.
//
// The vertical PROFILE is not vanilla's. Vanilla derives thickness from a full
// noise router (sloped cheese, squeeze, y-clamped gradients) that this
// generator has no equivalent of; here the plate's height comes from the same
// island value, which gives the same silhouette — thick in the middle, thin at
// the rim — without pretending to reproduce the router.

const (
	// EndOuterStart is how far out the islands begin. Vanilla's threshold is
	// on the CHUNK grid: totalChunkX^2 + totalChunkZ^2 > 4096, i.e. 64 chunks.
	EndOuterChunkR2 = 4096
	endIslandFloor  = 50 // the plates sit around here…
	endIslandTop    = 70 // …and never poke above this
	endIslandSearch = 12 // the 25x25 cell search vanilla does

	// Vanilla's ISLAND_THRESHOLD is -0.9 on ITS simplex noise. tachyne has no
	// simplex — its Perlin only spans about ±0.78 — so -0.9 selects NOTHING and
	// the outer End came out empty void.
	//
	// The replacement is calibrated on the thing that actually matters, which
	// is how much of the outer End is land rather than what fraction of CELLS
	// qualify: each qualifying cell seeds a plate tens of blocks across, and
	// the 25x25 search lets neighbours merge, so a cell fraction that sounds
	// small produces continuous ground. Matching vanilla's ~1-in-70 cells gave
	// 62% land; this gives ~19%, which is the scattered-discs End you can
	// actually glide between.
	endIslandThreshold = -0.70
)

// endIslandHeight is vanilla's getHeightValue: the island "height value" for a
// section (an 8-block cell), in [-100, 80]. Above 0 means solid.
func (g *Generator) endIslandHeight(sectionX, sectionZ int) float64 {
	chunkX, chunkZ := sectionX/2, sectionZ/2
	subX, subZ := sectionX%2, sectionZ%2

	// The main island's own falloff, which is what keeps the middle clear.
	h := 100.0 - math.Sqrt(float64(sectionX*sectionX+sectionZ*sectionZ))*8.0
	h = clampF(h, -100, 80)

	for xo := -endIslandSearch; xo <= endIslandSearch; xo++ {
		for zo := -endIslandSearch; zo <= endIslandSearch; zo++ {
			cx, cz := int64(chunkX+xo), int64(chunkZ+zo)
			if cx*cx+cz*cz <= EndOuterChunkR2 {
				continue // inside the void ring: no island seeds here
			}
			if g.endIslandNoise(cx, cz) >= endIslandThreshold {
				continue
			}
			// An integer hash of the cell decides how big its plate is: the
			// same formula vanilla uses, so the size distribution matches.
			size := math.Mod(math.Abs(float64(cx))*3439.0+math.Abs(float64(cz))*147.0, 13.0) + 9.0
			xd := float64(subX - xo*2)
			zd := float64(subZ - zo*2)
			v := clampF(100.0-math.Sqrt(xd*xd+zd*zd)*size, -100, 80)
			if v > h {
				h = v
			}
		}
	}
	return h
}

// endIslandNoise is the per-cell noise vanilla samples with a dedicated
// SimplexNoise. tachyne has no simplex and no LegacyRandomSource, so the
// islands land in DIFFERENT PLACES than a vanilla world of the same seed —
// their sizes, spacing and shapes follow vanilla's rules, their positions do
// not. Matching those would mean porting Java's legacy RNG, which buys
// nothing here: this End already diverges from vanilla's at the seed.
func (g *Generator) endIslandNoise(cx, cz int64) float64 {
	return g.detail.Noise2(float64(cx)*0.5+0.31, float64(cz)*0.5+0.17)
}

// endOuterColumn returns the top and bottom of the island plate at a column,
// or ok=false where there is only void.
func (g *Generator) endOuterColumn(x, z int) (top, bottom int, ok bool) {
	h := g.endIslandHeight(floorDiv(x, 8), floorDiv(z, 8))
	if h <= 0 {
		return 0, 0, false
	}
	// h runs 0..80 across a plate; map it to a lens sitting near the floor.
	thick := int(h/80.0*14.0) + 1
	top = endIslandFloor + int(h/80.0*float64(endIslandTop-endIslandFloor))
	return top, top - thick, true
}

// floorDiv is integer division that rounds toward negative infinity — Go's /
// truncates toward zero, which would mirror the islands across the axes.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// EndOuterColumn is endOuterColumn for callers outside worldgen: the top and
// bottom of the outer-island column at (x, z), and whether there is one.
func (g *Generator) EndOuterColumn(x, z int) (top, bottom int, ok bool) {
	return g.endOuterColumn(x, z)
}
