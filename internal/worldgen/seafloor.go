package worldgen

import "math"

// Sea-floor vegetation: vanilla's per-biome placed features for water
// columns — seagrass (26.3's eight placements, each a patch of attempts
// about one column of the chunk), kelp forests where a low-frequency noise
// says so, and sea pickle clusters in warm oceans. Each feature runs as
// vanilla runs it: N attempts per chunk, pickles scattering ±7 from theirs, kelp
// growing 1–10 tall and never breaking the surface. Attempts are drawn per
// ORIGIN chunk from a hashed stream, so the 3×3 neighbourhood's spill into
// this chunk is reproduced without any chunk depending on another's output.

var (
	Seagrass          = blockBase("seagrass")
	TallSeagrassUpper = blockBase("tall_seagrass") // half: upper is state 0
	TallSeagrassLower = TallSeagrassUpper + 1
	Kelp              = blockBase("kelp") // age 0..25: the growing head
	KelpPlant         = blockBase("kelp_plant")
	SeaPickle         = blockBase("sea_pickle") // pickles 1..4 × waterlogged
	MagmaBlock        = blockBase("magma_block")
)

// seafloorRule is one biome's feature set: the seagrass draws the old
// folded placement made (kept as dry runs so kelp and pickles, later on the
// same stream, do not move), kelp's noise_to_count_ratio (0 = none), and
// whether sea pickles grow.
type seafloorRule struct {
	seagrass int
	kelp     int
	pickles  bool
	coral    bool // warm_ocean_vegetation reefs
}

var seafloorRules = map[string]seafloorRule{
	"minecraft:ocean":               {seagrass: 48, kelp: 120},
	"minecraft:deep_ocean":          {seagrass: 48, kelp: 120},
	"minecraft:cold_ocean":          {seagrass: 32, kelp: 120},
	"minecraft:deep_cold_ocean":     {seagrass: 40, kelp: 120},
	"minecraft:lukewarm_ocean":      {seagrass: 80, kelp: 80},
	"minecraft:deep_lukewarm_ocean": {seagrass: 80, kelp: 80},
	"minecraft:warm_ocean":          {seagrass: 80, pickles: true, coral: true},
	"minecraft:river":               {seagrass: 48},
	"minecraft:swamp":               {seagrass: 64},
	"minecraft:mangrove_swamp":      {seagrass: 64},
}

// seagrassPlacement is one of vanilla's seagrass placed features: count
// attempts about ONE random column of the chunk (in_square, then count,
// then a ±7 trapezoid offset), each a weighted pick between tall seagrass
// (tall of 100) and short.
type seagrassPlacement struct {
	name        string
	count, tall int
	salt        int64
}

// seagrassPlacements in a fixed run order; the tall weights are the
// four selectors' (seagrass_short 30, _slightly_less_short 40, _mid 60,
// _tall 80).
var seagrassPlacements = []seagrassPlacement{
	{"seagrass_warm", 80, 30, 0x5EA6_0001},
	{"seagrass_normal", 48, 30, 0x5EA6_0002},
	{"seagrass_cold", 32, 30, 0x5EA6_0003},
	{"seagrass_river", 48, 40, 0x5EA6_0004},
	{"seagrass_swamp", 64, 60, 0x5EA6_0005},
	{"seagrass_deep_warm", 80, 80, 0x5EA6_0006},
	{"seagrass_deep", 48, 80, 0x5EA6_0007},
	{"seagrass_deep_cold", 40, 80, 0x5EA6_0008},
}

// seagrassByBiome is the seagrass placement each biome lists. The biome
// filter at an attempt's final cell asks for the SAME placement, so an
// attempt spilling from a deep ocean into a shallow one is dropped rather
// than grown at the deep ocean's odds.
var seagrassByBiome = map[string]string{
	"minecraft:ocean":               "seagrass_normal",
	"minecraft:deep_ocean":          "seagrass_deep",
	"minecraft:cold_ocean":          "seagrass_cold",
	"minecraft:deep_cold_ocean":     "seagrass_deep_cold",
	"minecraft:lukewarm_ocean":      "seagrass_warm",
	"minecraft:deep_lukewarm_ocean": "seagrass_deep_warm",
	"minecraft:warm_ocean":          "seagrass_warm",
	"minecraft:river":               "seagrass_river",
	"minecraft:swamp":               "seagrass_swamp",
	"minecraft:mangrove_swamp":      "seagrass_swamp",
}

const (
	seafloorSalt   = 0x5EAF100D
	kelpNoiseScale = 80.0 // noise_factor
	pickleRarity   = 16   // rarity_filter chance
	pickleCount    = 20   // sea_pickle count
)

// seafloorCol is a water column's floor: the first water cell and whether the
// block under it can hold a plant.
func (g *Generator) seafloorCol(wx, wz int) (floorY int, ok bool) {
	return g.columnAt(wx, wz).seafloor()
}

// seafloor is seafloorCol on a column already in hand.
func (col column) seafloor() (floorY int, ok bool) {
	if col.h > SeaLevel-1 {
		return 0, false // dry land
	}
	top := col.topBlock()
	if !IsSturdyTop(top) || top == MagmaBlock {
		return 0, false
	}
	return col.h, true
}

// decorateSeafloor stamps this chunk's water plants.
func (g *Generator) decorateSeafloor(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	inChunk := func(x, z int) bool { return x >= baseX && x < baseX+16 && z >= baseZ && z < baseZ+16 }
	at := func(x, y, z int) uint32 { return sectionBlockAt(ch, x-baseX, y, z-baseZ) }
	water := func(x, y, z int) bool { return at(x, y, z) == Water }
	put := func(x, y, z int, s uint32) { setSectionBlock(ch, x-baseX, y, z-baseZ, s, true) }
	bg := g.newBuildGuard(cx, cz)
	// floorAt is seafloorCol on the chunk as carved: a ravine under the sea
	// drops the floor to its bottom, which must hold a plant too.
	floorAt := func(x, z int) (int, bool) {
		y, ok := g.seafloorCol(x, z)
		if !ok {
			return 0, false
		}
		for y > MinY+1 && water(x, y-1, z) {
			y--
		}
		if b := at(x, y-1, z); !IsSturdyTop(b) || b == MagmaBlock {
			return 0, false
		}
		return y, true
	}
	for ncx := cx - 1; ncx <= cx+1; ncx++ {
		for ncz := cz - 1; ncz <= cz+1; ncz++ {
			ox, oz := int(ncx)*16, int(ncz)*16
			rule, ok := seafloorRules[g.resolveBiome(ox+8, oz+8).Name]
			r := newTreeRNG(g.seed^seafloorSalt, ox, oz)
			// Reefs come first (warm_ocean's feature order), so the seagrass and
			// pickles below skip the cells they took.
			if ok && rule.coral {
				g.decorateCoral(r, ox, oz, baseX, baseZ, at, put)
			}
			// Seagrass runs for every biome in the chunk, not just its centre's.
			g.decorateSeagrass(ox, oz, inChunk, floorAt, water, put, bg)
			if !ok {
				continue
			}
			// The old folded seagrass draws, spent unplaced: kelp and pickles
			// follow on this stream and keep their places.
			for i := 0; i < rule.seagrass; i++ {
				r.Intn(16)
				r.Intn(8)
				r.Intn(8)
				r.Intn(16)
				r.Intn(8)
				r.Intn(8)
				r.Float64()
			}
			// Kelp: a noise-scaled count of stalks, 1–10 tall, stopping short of
			// the surface.
			if rule.kelp > 0 {
				n := int(math.Ceil(g.humid.Noise2(float64(ox)/kelpNoiseScale, float64(oz)/kelpNoiseScale) * float64(rule.kelp)))
				for i := 0; i < n; i++ {
					x, z := ox+r.Intn(16), oz+r.Intn(16)
					height := 1 + r.Intn(10)
					ages := make([]int, height+1)
					for j := range ages {
						ages[j] = 20 + r.Intn(4)
					}
					if !inChunk(x, z) {
						continue
					}
					if kr, ok := seafloorRules[g.resolveBiome(x, z).Name]; !ok || kr.kelp == 0 {
						continue
					}
					y, ok := floorAt(x, z)
					if !ok {
						continue
					}
					stampKelp(x, y, z, height, ages, at, put)
				}
			}
			// Sea pickles: one chunk in sixteen grows a cluster of up to twenty.
			if rule.pickles && r.Intn(pickleRarity) == 0 {
				cxo, czo := ox+r.Intn(16), oz+r.Intn(16)
				for i := 0; i < pickleCount; i++ {
					x := cxo + r.Intn(8) - r.Intn(8)
					z := czo + r.Intn(8) - r.Intn(8)
					pickles := 1 + r.Intn(4)
					if !inChunk(x, z) {
						continue
					}
					if pr, ok := seafloorRules[g.resolveBiome(x, z).Name]; !ok || !pr.pickles {
						continue
					}
					y, ok := floorAt(x, z)
					if !ok || !water(x, y, z) {
						continue
					}
					put(x, y, z, SeaPickle+uint32(pickles-1)*2) // waterlogged=true is the first of each pair
				}
			}
		}
	}
}

// decorateSeagrass runs origin chunk (ox, oz)'s seagrass placements into
// this chunk: each placement listed by a biome among the chunk's corner and
// centre columns, from its own stream. One centre column per placement,
// count attempts offset about it, each on the ocean floor, in water, in a
// biome that lists the same placement, and clear of a player's build; tall
// seagrass needs water above it too.
func (g *Generator) decorateSeagrass(ox, oz int, inChunk func(x, z int) bool, floorAt func(x, z int) (int, bool),
	water func(x, y, z int) bool, put func(x, y, z int, s uint32), bg *buildGuard) {
	present := map[string]bool{}
	for _, d := range [5][2]int{{0, 0}, {15, 0}, {0, 15}, {15, 15}, {8, 8}} {
		if name, ok := seagrassByBiome[g.resolveBiome(ox+d[0], oz+d[1]).Name]; ok {
			present[name] = true
		}
	}
	for _, p := range seagrassPlacements {
		if !present[p.name] {
			continue
		}
		r := newTreeRNG(g.seed^p.salt, ox, oz)
		sx, sz := ox+r.Intn(16), oz+r.Intn(16) // in_square
		for i := 0; i < p.count; i++ {
			x := sx + r.Intn(8) - r.Intn(8) // offset: trapezoid -7..7
			z := sz + r.Intn(8) - r.Intn(8)
			tall := r.Intn(100) < p.tall // the selector's weighted pick
			if !inChunk(x, z) || seagrassByBiome[g.resolveBiome(x, z).Name] != p.name {
				continue
			}
			y, ok := floorAt(x, z)
			if !ok || !water(x, y, z) || bg.decorationBlocked(x, y, z) {
				continue
			}
			if tall {
				if water(x, y+1, z) {
					put(x, y, z, TallSeagrassLower)
					put(x, y+1, z, TallSeagrassUpper)
				}
				continue
			}
			put(x, y, z, Seagrass)
		}
	}
}

// stampKelp is KelpFeature.place: body segments up the column with the head
// on top, the head dropping one cell when the next cell up is not water
// (the surface, or something in the way), never two heads in a row.
func stampKelp(x, y0, z, height int, ages []int, at func(x, y, z int) uint32, put func(x, y, z int, s uint32)) {
	water := func(x, y, z int) bool { return at(x, y, z) == Water }
	if !water(x, y0, z) {
		return
	}
	y := y0
	for i := 0; i <= height; i++ {
		if water(x, y, z) && water(x, y+1, z) {
			if i == height {
				put(x, y, z, Kelp+uint32(ages[i]))
			} else {
				put(x, y, z, KelpPlant)
			}
		} else if i > 0 {
			below := y - 1
			if IsKelpHead(at(x, below-1, z)) {
				return
			}
			put(x, below, z, Kelp+uint32(ages[i]))
			return
		} else {
			return
		}
		y++
	}
}

// IsKelpHead reports a kelp head of any age.
func IsKelpHead(s uint32) bool { return s >= Kelp && s <= Kelp+25 }
