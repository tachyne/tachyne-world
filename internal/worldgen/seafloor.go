package worldgen

import "math"

// Sea-floor vegetation: vanilla's per-biome placed features for water
// columns — seagrass (short/tall by the biome's probability), kelp forests
// where a low-frequency noise says so, and sea pickle clusters in warm
// oceans. Each feature runs as vanilla runs it: N attempts per chunk at
// random columns, seagrass and pickles scattering ±7 from the attempt, kelp
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

// seafloorRule is one biome's feature set: seagrass attempts per chunk and
// the tall-seagrass probability, kelp's noise_to_count_ratio (0 = none),
// and whether sea pickles grow.
type seafloorRule struct {
	seagrass int
	tall     float64
	kelp     int
	pickles  bool
	coral    bool // warm_ocean_vegetation reefs
}

var seafloorRules = map[string]seafloorRule{
	"minecraft:ocean":               {seagrass: 48, tall: 0.3, kelp: 120},
	"minecraft:deep_ocean":          {seagrass: 48, tall: 0.8, kelp: 120},
	"minecraft:cold_ocean":          {seagrass: 32, tall: 0.3, kelp: 120},
	"minecraft:deep_cold_ocean":     {seagrass: 40, tall: 0.8, kelp: 120},
	"minecraft:lukewarm_ocean":      {seagrass: 80, tall: 0.3, kelp: 80},
	"minecraft:deep_lukewarm_ocean": {seagrass: 80, tall: 0.8, kelp: 80},
	"minecraft:warm_ocean":          {seagrass: 80, tall: 0.3, pickles: true, coral: true},
	"minecraft:river":               {seagrass: 48, tall: 0.4},
	"minecraft:swamp":               {seagrass: 64, tall: 0.6},
	"minecraft:mangrove_swamp":      {seagrass: 64, tall: 0.6}, // seagrass_swamp there too
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
	col := g.columnAt(wx, wz)
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
	for ncx := cx - 1; ncx <= cx+1; ncx++ {
		for ncz := cz - 1; ncz <= cz+1; ncz++ {
			ox, oz := int(ncx)*16, int(ncz)*16
			rule, ok := seafloorRules[g.resolveBiome(ox+8, oz+8).Name]
			if !ok {
				continue
			}
			r := newTreeRNG(g.seed^seafloorSalt, ox, oz)
			// Reefs come first (warm_ocean's feature order), so the seagrass and
			// pickles below skip the cells they took.
			if rule.coral {
				g.decorateCoral(r, ox, oz, baseX, baseZ, at, put)
			}
			// Seagrass: N attempts, each scattered ±7 from its column.
			for i := 0; i < rule.seagrass; i++ {
				x := ox + r.Intn(16) + r.Intn(8) - r.Intn(8)
				z := oz + r.Intn(16) + r.Intn(8) - r.Intn(8)
				tall := r.Float64() < rule.tall
				if !inChunk(x, z) {
					continue
				}
				pr, ok := seafloorRules[g.resolveBiome(x, z).Name]
				if !ok || pr.seagrass == 0 {
					continue // the biome filter runs at the final position
				}
				y, ok := g.seafloorCol(x, z)
				if !ok || !water(x, y, z) {
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
					y, ok := g.seafloorCol(x, z)
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
					y, ok := g.seafloorCol(x, z)
					if !ok || !water(x, y, z) {
						continue
					}
					put(x, y, z, SeaPickle+uint32(pickles-1)*2) // waterlogged=true is the first of each pair
				}
			}
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
