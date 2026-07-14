package worldgen

import "math"

// Amethyst geodes: buried hollow spheres with a smooth-basalt shell, a calcite
// layer, an amethyst-block lining studded with budding_amethyst, and amethyst
// clusters growing into the hollow. A faithful-appearance approximation of the
// vanilla amethyst_geode feature (single centre + per-cell hash jitter rather
// than its multi-point distance field). Rare (≈1/24 chunks), y -58..30.
//
// Cross-chunk consistency: a geode can straddle chunk borders, so each chunk
// stamps the portion of every geode rooted in its 3×3 neighbourhood that falls
// inside it. All per-cell decisions are pure functions of the geode's origin
// seed + cell coords, so neighbouring chunks agree without communication.

var (
	smoothBasalt        = blockBase("smooth_basalt")
	calcite             = blockBase("calcite")
	amethystBlock       = blockBase("amethyst_block")
	buddingAmethyst     = blockBase("budding_amethyst")
	amethystClusterBase = blockBase("amethyst_cluster")
	smallBudBase        = blockBase("small_amethyst_bud")
	mediumBudBase       = blockBase("medium_amethyst_bud")
	largeBudBase        = blockBase("large_amethyst_bud")
)

// geode layer radii (blocks from centre): <air hollow, then amethyst, calcite,
// basalt shell. A per-cell jitter (~±0.4) roughens the surface.
const (
	geodeAirR      = 3.0
	geodeAmethystR = 3.9
	geodeCalciteR  = 4.6
	geodeBasaltR   = 5.3
	geodeReach     = 6          // bounding half-size to scan
	geodeHollow    = 0xFFFFFFFF // sentinel: carve to air (Air==0 collides with "outside")
)

// budFacing maps an outward step to the bud/cluster facing index
// (north,east,south,west,up,down) in the state table.
var budFacing = map[[3]int]int{
	{0, 0, -1}: 0, {1, 0, 0}: 1, {0, 0, 1}: 2, {-1, 0, 0}: 3, {0, 1, 0}: 4, {0, -1, 0}: 5,
}

func budState(base uint32, dir [3]int) uint32 {
	return base + uint32(budFacing[dir])*2 + 1 // +1 selects waterlogged=false
}

// cellHash mixes a geode-origin seed with a cell coord into a stable value.
func cellHash(seed uint64, x, y, z int) uint64 {
	h := seed
	for _, v := range [3]int{x, y, z} {
		h ^= uint64(uint32(v)) + 0x9e3779b97f4a7c15 + (h << 6) + (h >> 2)
		h *= 0xbf58476d1ce4e5b9
		h ^= h >> 27
	}
	return h
}

func geodeNoise(seed uint64, x, y, z int) float64 {
	return float64(cellHash(seed, x, y, z)>>11) / float64(1<<53)
}

// geodeAt deterministically decides whether a chunk roots a geode and where.
func (g *Generator) geodeAt(ncx, ncz int32) (originSeed uint64, cx, cy, cz int, ok bool) {
	s := uint64(oreSeed(g.seed^0x6a0de, ncx, ncz))
	if s%24 != 0 { // rarity_filter chance 24
		return 0, 0, 0, 0, false
	}
	cx = int(ncx)*16 + int((s>>8)%16)
	cz = int(ncz)*16 + int((s>>16)%16)
	cy = -58 + int((s>>24)%89) // uniform -58..30
	return s, cx, cy, cz, true
}

// placeGeodes stamps into this chunk every geode rooted nearby.
func (g *Generator) placeGeodes(ch *Chunk, cx, cz int32) {
	topY := MinY + len(ch.Sections)*16 - 1
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			seed, gx, gy, gz, ok := g.geodeAt(cx+dcx, cz+dcz)
			if !ok {
				continue
			}
			g.stampGeode(ch, int(cx), int(cz), topY, seed, gx, gy, gz)
		}
	}
}

func (g *Generator) stampGeode(ch *Chunk, cx, cz, topY int, seed uint64, gx, gy, gz int) {
	base := cx * 16
	baseZ := cz * 16
	// Pass 1: shell + hollow.
	for bx := gx - geodeReach; bx <= gx+geodeReach; bx++ {
		lx := bx - base
		if lx < 0 || lx > 15 {
			continue
		}
		for bz := gz - geodeReach; bz <= gz+geodeReach; bz++ {
			lz := bz - baseZ
			if lz < 0 || lz > 15 {
				continue
			}
			for by := gy - geodeReach; by <= gy+geodeReach; by++ {
				if by <= MinY || by >= topY {
					continue
				}
				d := math.Sqrt(float64((bx-gx)*(bx-gx)+(by-gy)*(by-gy)+(bz-gz)*(bz-gz))) +
					(geodeNoise(seed, bx, by, bz)-0.5)*0.8
				setGeodeCell(ch, lx, by, lz, geodeShell(seed, bx, by, bz, d))
			}
		}
	}
	// Pass 2: clusters in hollow cells adjacent to budding amethyst.
	for bx := gx - geodeReach; bx <= gx+geodeReach; bx++ {
		lx := bx - base
		if lx < 0 || lx > 15 {
			continue
		}
		for bz := gz - geodeReach; bz <= gz+geodeReach; bz++ {
			lz := bz - baseZ
			if lz < 0 || lz > 15 {
				continue
			}
			for by := gy - geodeReach + 1; by < gy+geodeReach; by++ {
				if by <= MinY || by >= topY {
					continue
				}
				if !isHollow(seed, gx, gy, gz, bx, by, bz) {
					continue
				}
				if c := geodeCluster(seed, gx, gy, gz, bx, by, bz); c != 0 {
					yi := by - MinY
					sec, idx := yi/16, ((yi%16)*16+lz)*16+lx
					if ch.Sections[sec][idx] == Air {
						ch.Sections[sec][idx] = c
					}
				}
			}
		}
	}
}

// geodeShell returns the block for a cell at distance d, or 0 if outside.
func geodeShell(seed uint64, bx, by, bz int, d float64) uint32 {
	switch {
	case d <= geodeAirR:
		return geodeHollow // carve to air (distinct from 0 = outside geode)
	case d <= geodeAmethystR:
		if cellHash(seed, bx, by, bz)%7 == 0 {
			return buddingAmethyst
		}
		return amethystBlock
	case d <= geodeCalciteR:
		return calcite
	case d <= geodeBasaltR:
		return smoothBasalt
	}
	return 0
}

// isHollow reports whether a cell falls in the geode's air pocket.
func isHollow(seed uint64, gx, gy, gz, bx, by, bz int) bool {
	d := math.Sqrt(float64((bx-gx)*(bx-gx)+(by-gy)*(by-gy)+(bz-gz)*(bz-gz))) +
		(geodeNoise(seed, bx, by, bz)-0.5)*0.8
	return d <= geodeAirR
}

// geodeCluster returns an amethyst cluster/bud state to place in this hollow
// cell if a budding_amethyst sits against it, else 0.
func geodeCluster(seed uint64, gx, gy, gz, bx, by, bz int) uint32 {
	for _, dir := range [6][3]int{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}} {
		nx, ny, nz := bx+dir[0], by+dir[1], bz+dir[2]
		d := math.Sqrt(float64((nx-gx)*(nx-gx)+(ny-gy)*(ny-gy)+(nz-gz)*(nz-gz))) +
			(geodeNoise(seed, nx, ny, nz)-0.5)*0.8
		if d > geodeAirR && d <= geodeAmethystR && cellHash(seed, nx, ny, nz)%7 == 0 {
			// A budding block is on the `dir` side; the cluster grows back the
			// other way, facing from the budding block toward this hollow cell.
			out := [3]int{-dir[0], -dir[1], -dir[2]}
			r := cellHash(seed, bx, by, bz) % 100
			switch { // ~35% grows something; larger stages rarer
			case r < 10:
				return budState(amethystClusterBase, out)
			case r < 18:
				return budState(largeBudBase, out)
			case r < 26:
				return budState(mediumBudBase, out)
			case r < 35:
				return budState(smallBudBase, out)
			}
			return 0
		}
	}
	return 0
}

// setGeodeCell writes a shell block, replacing only stone/deepslate (so the
// geode doesn't overwrite bedrock, fluids, or carve odd holes in cave walls),
// while the hollow carves stone/deepslate to air.
func setGeodeCell(ch *Chunk, lx, y, lz int, block uint32) {
	if block == 0 {
		return
	}
	yi := y - MinY
	sec, idx := yi/16, ((yi%16)*16+lz)*16+lx
	cur := ch.Sections[sec][idx]
	if block == geodeHollow {
		if cur == Stone || cur == Deepslate {
			ch.Sections[sec][idx] = Air
		}
		return
	}
	if cur == Stone || cur == Deepslate {
		ch.Sections[sec][idx] = block
	}
}
