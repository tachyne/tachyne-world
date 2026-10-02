package worldgen

import "math"

// Amethyst geodes: vanilla's amethyst_geode, GeodeFeature — one chunk in
// 24, uniform from six above the bottom to y=30. Three or four points 4–6
// blocks out from the origin (each with an offset of 1–2) make a distance
// field: the sum of 1/sqrt(d²+offset) over the points, plus a little noise,
// sets each cell of the 33-block box to the air filling, the amethyst lining
// (one in twelve budding), calcite or the smooth-basalt crust by vanilla's
// layer thresholds. A crack (95%) of three more points opens the shell on
// one side. A geode two of whose points land in air or an invalid block
// (bedrock, fluid, ice) is not placed. Budding amethyst grows a bud or
// cluster (the four tiers equally likely) into its first open neighbour,
// one in three times.
//
// A geode is planned once on the pre-decoration terrain (the owRegion
// scratch view) from its origin chunk's own stream, and every chunk pass
// writes its part of the plan, so the chunks agree. A geode with a player's
// build or dug cell in its box is left out whole.

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

// geodeSalt seeds the geodes' own streams.
const geodeSalt = 0x6E0DE_0026

// Vanilla's amethyst_geode configuration and GeodeFeature's defaults.
const (
	geodeChance      = 24  // rarity_filter
	geodeFilling     = 1.7 // layers
	geodeInnerLayer  = 2.2
	geodeMiddleLayer = 3.2
	geodeOuterLayer  = 4.2
	geodeCrackChance = 0.95
	geodeCrackBase   = 2.0
	geodeCrackOffset = 2
	geodeAltChance   = 0.083 // use_alternate_layer0_chance: budding amethyst
	geodePlaceChance = 0.35  // use_potential_placements_chance
	geodeWallMin     = 4     // outer_wall_distance 4..6
	geodeWallMax     = 6
	geodeNoiseMul    = 0.05
	geodeGenOffset   = 16 // min/max_gen_offset
)

// geodePlan is one geode's writes.
type geodePlan struct {
	cx, cz     int32 // its origin chunk
	cells      map[[3]int]uint32
	x0, y0, z0 int // bounding box of the writes
	x1, y1, z1 int
}

var geodeCache = map[roomKey]*geodePlan{}

// geodeCacheCap bounds the cached plans (a geode's is a few thousand cells).
const geodeCacheCap = 4096

// geodeIn is origin chunk (cx, cz)'s geode, or nil; cached like the rooms.
func (g *Generator) geodeIn(cx, cz int32) *geodePlan {
	k := roomKey{g: g, cx: cx, cz: cz}
	roomMu.Lock()
	p, ok := geodeCache[k]
	roomMu.Unlock()
	if ok {
		return p
	}
	p = g.planGeode(cx, cz)
	roomMu.Lock()
	if len(geodeCache) >= geodeCacheCap {
		geodeCache = map[roomKey]*geodePlan{}
	}
	geodeCache[k] = p
	roomMu.Unlock()
	return p
}

// planGeode is GeodeFeature.place for origin chunk (cx, cz)'s draw.
func (g *Generator) planGeode(cx, cz int32) *geodePlan {
	ox, oz := int(cx)*16, int(cz)*16
	r := newTreeRNG(g.seed^geodeSalt, ox, oz)
	if r.Float64() >= 1.0/geodeChance {
		return nil
	}
	x0, z0 := ox+r.Intn(16), oz+r.Intn(16)
	y0 := oreUniform(oreAboveBottom(6), oreAbs(30)).sample(r, g.Ceiling())
	return g.geodeAt(r, cx, cz, x0, y0, z0, true, nil)
}

// geodeAt is GeodeFeature.place at (x0, y0, z0) on r's stream, against the
// terrain: the plan of what it writes, or nil when it does not place. A
// guarded geode (generation's) is also left out where a player built or dug.
// under (nil: the terrain) is what the geode sees beneath its own writes.
func (g *Generator) geodeAt(r TreeRNG, cx, cz int32, x0, y0, z0 int, guarded bool, under func(x, y, z int) uint32) *geodePlan {
	view := &owRegion{g: g, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}, under: under}

	numPoints := 3 + r.Intn(2) // distribution_points 3..4
	adj := float64(numPoints) / geodeWallMax
	innerAir := 1 / math.Sqrt(geodeFilling)
	innermost := 1 / math.Sqrt(geodeInnerLayer+adj)
	innerCrust := 1 / math.Sqrt(geodeMiddleLayer+adj)
	outerCrust := 1 / math.Sqrt(geodeOuterLayer+adj)
	extra := 0.0
	if numPoints > 3 {
		extra = adj
	}
	crackSize := 1 / math.Sqrt(geodeCrackBase+r.Float64()/2+extra)
	crack := r.Float64() < geodeCrackChance
	type point struct{ x, y, z, off int }
	var points []point
	invalid := 0
	for i := 0; i < numPoints; i++ {
		px := x0 + geodeWallMin + r.Intn(geodeWallMax-geodeWallMin+1)
		py := y0 + geodeWallMin + r.Intn(geodeWallMax-geodeWallMin+1)
		pz := z0 + geodeWallMin + r.Intn(geodeWallMax-geodeWallMin+1)
		if s := view.read(px, py, pz); s == Air || geodeInvalid(s) {
			if invalid++; invalid > 1 { // invalid_blocks_threshold
				return nil
			}
		}
		points = append(points, point{px, py, pz, 1 + r.Intn(2)}) // point_offset 1..2
	}
	var cracks [][3]int
	if crack {
		o := numPoints*2 + 1
		var dx, dz int
		switch r.Intn(4) {
		case 0:
			dx, dz = o, 0
		case 1:
			dx, dz = 0, o
		case 2:
			dx, dz = o, o
		default:
			dx, dz = 0, 0
		}
		for _, dy := range [3]int{7, 5, 1} {
			cracks = append(cracks, [3]int{x0 + dx, y0 + dy, z0 + dz})
		}
	}
	noise := NewPerlin(g.seed ^ geodeSalt)
	p := &geodePlan{cx: cx, cz: cz, cells: map[[3]int]uint32{}, x0: math.MaxInt, y0: math.MaxInt, z0: math.MaxInt,
		x1: math.MinInt, y1: math.MinInt, z1: math.MinInt}
	top := g.Ceiling()
	set := func(x, y, z int, s uint32) {
		if y < MinY || y >= top || inAnyRange(view.read(x, y, z), featuresCannotReplace) {
			return
		}
		p.cells[[3]int{x, y, z}] = s
		view.capture[[3]int{x, y, z}] = s
		p.x0, p.y0, p.z0 = min(p.x0, x), min(p.y0, y), min(p.z0, z)
		p.x1, p.y1, p.z1 = max(p.x1, x), max(p.y1, y), max(p.z1, z)
	}
	var budding [][3]int
	// BlockPos.betweenClosed's order: x fastest, then y, then z.
	for z := z0 - geodeGenOffset; z <= z0+geodeGenOffset; z++ {
		for y := y0 - geodeGenOffset; y <= y0+geodeGenOffset; y++ {
			for x := x0 - geodeGenOffset; x <= x0+geodeGenOffset; x++ {
				n := noise.Noise3(float64(x)/16, float64(y)/16, float64(z)/16) * geodeNoiseMul
				sum := 0.0
				for _, q := range points {
					dx, dy, dz := x-q.x, y-q.y, z-q.z
					sum += 1/math.Sqrt(float64(dx*dx+dy*dy+dz*dz+q.off)) + n
				}
				if sum < outerCrust {
					continue
				}
				if sum >= innerAir {
					set(x, y, z, Air)
					continue
				}
				crackSum := 0.0
				for _, c := range cracks {
					dx, dy, dz := x-c[0], y-c[1], z-c[2]
					crackSum += 1/math.Sqrt(float64(dx*dx+dy*dy+dz*dz+geodeCrackOffset)) + n
				}
				switch {
				case crack && crackSum >= crackSize:
					set(x, y, z, Air)
				case sum >= innermost:
					if r.Float64() < geodeAltChance {
						set(x, y, z, buddingAmethyst)
						if r.Float64() < geodePlaceChance {
							budding = append(budding, [3]int{x, y, z})
						}
					} else {
						set(x, y, z, amethystBlock)
					}
				case sum >= innerCrust:
					set(x, y, z, calcite)
				default:
					set(x, y, z, smoothBasalt)
				}
			}
		}
	}
	buds := [4]string{"small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster"}
	for _, b := range budding {
		name := buds[r.Intn(len(buds))]
		for _, d := range geodeDirections {
			x, y, z := b[0]+d.dx, b[1]+d.dy, b[2]+d.dz
			at := view.read(x, y, z)
			if at == Air || at == Water { // canClusterGrowAtState: air or a water source
				wl := "false"
				if at == Water {
					wl = "true"
				}
				set(x, y, z, withProps(name, "facing", d.name, "waterlogged", wl))
				break
			}
		}
	}
	if len(p.cells) == 0 || (guarded && g.touchedIn(p.x0, p.y0, p.z0, p.x1, p.y1, p.z1)) {
		return nil // a player built or dug where it would stand
	}
	return p
}

// geodeDirections is Direction.values(): down, up, north, south, west, east.
var geodeDirections = [6]struct {
	name       string
	dx, dy, dz int
}{{"down", 0, -1, 0}, {"up", 0, 1, 0}, {"north", 0, 0, -1}, {"south", 0, 0, 1}, {"west", -1, 0, 0}, {"east", 1, 0, 0}}

// geodeInvalid is #geode_invalid_blocks: bedrock, water, lava and the ices.
func geodeInvalid(s uint32) bool {
	return s == Bedrock || IsFluid(s) || s == Ice || s == PackedIce || s == BlueIce
}

// placeGeodes stamps into this chunk every geode rooted in its 3×3
// neighbourhood (a geode reaches sixteen blocks from its origin).
func (g *Generator) placeGeodes(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			p := g.geodeIn(cx+dcx, cz+dcz)
			if p == nil || p.x1 < baseX || p.x0 > baseX+15 || p.z1 < baseZ || p.z0 > baseZ+15 {
				continue
			}
			for c, s := range p.cells {
				if lx, lz := c[0]-baseX, c[2]-baseZ; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
					setSectionBlock(ch, lx, c[1], lz, s, true)
				}
			}
		}
	}
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
