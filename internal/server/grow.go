package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Random-tick growth — the "living world" simulation. Vanilla picks a few random
// blocks per chunk-section per tick and ticks whatever is there; growable blocks
// (crops, cane, cactus, saplings, grass, leaves) advance. Runs on the hub
// goroutine, so world reads/writes and broadcasts need no extra locking. Reads go
// through world.At (cache-backed) since the ticker touches thousands of blocks.

const (
	randomTickSpeed = 3 // blocks ticked per chunk-section per tick (vanilla default)
	simRadius       = 4 // chunks around each player that random-tick

	// Block-state ID ranges (1.21.5, from minecraft-data blocks.json).
	caneMin, caneMax     = 5978, 5993 // sugar_cane, age 0..15
	cactusMin, cactusMax = 5960, 5975 // cactus, age 0..15
)

var (
	farmlandMin = worldgen.BlockBase("farmland") // (moisture 0..7) — crops sit on it
	dirtPath    = worldgen.BlockBase("dirt_path")
)

// cropRanges are the [min,max] state ranges of the staged crops; the max state is
// fully grown. Each random tick on an immature, lit crop advances one stage.
// blockRange looks up each named block's [min,max] state range (version-independent).
func blockRange(names ...string) [][2]uint32 {
	r := make([][2]uint32, len(names))
	for i, n := range names {
		lo, hi := worldgen.BlockRange(n)
		r[i] = [2]uint32{lo, hi}
	}
	return r
}

var cropRanges = blockRange("wheat", "carrots", "potatoes", "beetroots")

// saplingRanges: at stage 1 the sapling grows a tree.
var saplingRanges = blockRange("oak_sapling", "spruce_sapling", "birch_sapling")

// leafRanges are the leaf families we generate; the persistent property means
// player-placed leaves never decay. All species are listed so decay + drops
// behave consistently — paired with logNearby knowing every log type, a canopy
// near its own trunk never wrongly rots.
var leafRanges = blockRange("oak_leaves", "spruce_leaves", "birch_leaves",
	"jungle_leaves", "acacia_leaves", "cherry_leaves", "dark_oak_leaves",
	"pale_oak_leaves", "mangrove_leaves")

func inRange(s uint32, r [2]uint32) bool { return s >= r[0] && s <= r[1] }

// isAnyLeaf reports whether a state is one of the leaf families we model.
func isAnyLeaf(s uint32) bool {
	for _, r := range leafRanges {
		if inRange(s, r) {
			return true
		}
	}
	return false
}

// runRandomTicks ticks the loaded chunks around each player.
func (h *hub) runRandomTicks(players map[int32]*tracked) {
	if len(players) == 0 {
		return
	}
	seen := map[[2]int]bool{}
	for _, t := range players {
		pcx, pcz := chunkFloor(t.x), chunkFloor(t.z)
		for dx := -simRadius; dx <= simRadius; dx++ {
			for dz := -simRadius; dz <= simRadius; dz++ {
				c := [2]int{pcx + dx, pcz + dz}
				if seen[c] {
					continue
				}
				seen[c] = true
				h.randomTickChunk(players, c[0], c[1])
			}
		}
	}
}

func (h *hub) randomTickChunk(players map[int32]*tracked, cx, cz int) {
	if h.rng.Intn(16) == 0 { // vanilla tickPrecipitation: ~1 column/chunk sampled
		h.precipTick(players, cx, cz)
	}
	for s := 0; s < h.world.Sections(); s++ {
		baseY := worldgen.MinY + s*16
		speed := randomTickSpeed
		if h.rules.RandomTicks >= 0 {
			speed = h.rules.RandomTicks // gamerule randomTickSpeed (0 = growth off)
		}
		for i := 0; i < speed; i++ {
			x := cx*16 + h.rng.Intn(16)
			y := baseY + h.rng.Intn(16)
			z := cz*16 + h.rng.Intn(16)
			h.randomTickBlock(players, x, y, z)
		}
	}
}

func (h *hub) randomTickBlock(players map[int32]*tracked, x, y, z int) {
	state := h.world.At(x, y, z)
	if worldgen.IsLava(state) {
		h.lavaIgnite(players, x, y, z)
		return
	}
	if h.tickStem(players, x, y, z, state) {
		return
	}
	if h.farmlandRandomTick(players, x, y, z, state) {
		return
	}
	if h.tickCocoa(players, x, y, z, state) {
		return
	}
	if h.tickBerry(players, x, y, z, state) {
		return
	}
	if h.tickCopper(players, x, y, z, state) {
		return
	}
	switch {
	case inRange(state, [2]uint32{caneMin, caneMax}):
		h.tickStackPlant(players, x, y, z, state, caneMin)
	case inRange(state, [2]uint32{cactusMin, cactusMax}):
		h.tickStackPlant(players, x, y, z, state, cactusMin)
	case state == worldgen.GrassBlock:
		h.tickGrass(players, x, y, z)
	default:
		if h.tickDriedGhast(players, x, y, z, state) {
			return
		}
		if h.tickCrop(players, x, y, z, state) {
			return
		}
		if h.tickSapling(players, x, y, z, state) {
			return
		}
		h.tickLeaf(players, x, y, z, state)
	}
}

// tickStackPlant grows sugar cane / cactus: the top stalk ages each tick, and at
// age 15 it spawns a new stalk above (up to 3 tall), resetting its own age.
func (h *hub) tickStackPlant(players map[int32]*tracked, x, y, z int, state uint32, min uint32) {
	if h.world.At(x, y+1, z) != worldgen.Air {
		return // only the top stalk (open above) grows
	}
	height := 1
	for k := 1; k < 3; k++ {
		if s := h.world.At(x, y-k, z); s >= min && s <= min+15 {
			height++
		} else {
			break
		}
	}
	if height >= 3 {
		return
	}
	if age := state - min; age >= 15 {
		h.setBlock(players, blockPos{x, y, z}, min)        // reset this stalk
		h.setBlock(players, blockPos{x, y + 1, z}, min)    // new stalk above
		h.scheduleAround(blockPos{x, y + 1, z}, fallDelay) // support/neighbour recheck
	} else {
		h.setBlock(players, blockPos{x, y, z}, min+age+1)
	}
}

// tickCrop advances a staged crop one stage if it's immature and sky-lit.
func (h *hub) tickCrop(players map[int32]*tracked, x, y, z int, state uint32) bool {
	for _, r := range cropRanges {
		if inRange(state, r) {
			if state < r[1] && h.skyLit(x, y, z) {
				h.setBlock(players, blockPos{x, y, z}, state+1)
			}
			return true
		}
	}
	return false
}

// tickSapling advances a sapling's hidden stage, then grows a tree.
func (h *hub) tickSapling(players map[int32]*tracked, x, y, z int, state uint32) bool {
	for _, r := range saplingRanges {
		if inRange(state, r) {
			if !h.skyLit(x, y, z) {
				return true
			}
			if state == r[0] {
				h.setBlock(players, blockPos{x, y, z}, state+1) // stage 0 → 1
			} else {
				h.growTree(players, x, y, z)
			}
			return true
		}
	}
	return false
}

// tickLeaf decays a non-persistent leaf with no log nearby, rolling its drops.
func (h *hub) tickLeaf(players map[int32]*tracked, x, y, z int, state uint32) {
	for _, r := range leafRanges {
		if !inRange(state, r) {
			continue
		}
		persistent := ((state-r[0])/2)%2 == 0 // middle property; idx 0 == "true"
		if persistent || h.logNearby(x, y, z) {
			return
		}
		h.setBlock(players, blockPos{x, y, z}, worldgen.Air)
		h.scheduleAround(blockPos{x, y, z}, fallDelay)
		h.rollLeafDrops(players, x, y, z)
		return
	}
}

// tickGrass spreads grass to a nearby dirt block, or dies back to dirt if covered.
func (h *hub) tickGrass(players map[int32]*tracked, x, y, z int) {
	if h.opaqueAbove(x, y, z) {
		h.setBlock(players, blockPos{x, y, z}, worldgen.Dirt) // smothered → dirt
		return
	}
	tx := x + h.rng.Intn(3) - 1
	ty := y + h.rng.Intn(3) - 1
	tz := z + h.rng.Intn(3) - 1
	if h.world.At(tx, ty, tz) == worldgen.Dirt && !h.opaqueAbove(tx, ty, tz) {
		h.setBlock(players, blockPos{tx, ty, tz}, worldgen.GrassBlock)
	}
}

// growTree replaces a sapling with a small oak: a 4–6 log trunk and a leaf canopy.
func (h *hub) growTree(players map[int32]*tracked, x, y, z int) {
	height := 4 + h.rng.Intn(3)
	top := y + height
	for ty := y; ty < top; ty++ {
		h.setBlock(players, blockPos{x, ty, z}, worldgen.OakLog)
	}
	leaf := func(ly, r int) {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if r == 2 && abs(dx) == 2 && abs(dz) == 2 {
					continue // trim canopy corners
				}
				px, pz := x+dx, z+dz
				if h.world.At(px, ly, pz) == worldgen.Air {
					h.setBlock(players, blockPos{px, ly, pz}, worldgen.OakLeaves)
				}
			}
		}
	}
	leaf(top-2, 2)
	leaf(top-1, 2)
	leaf(top, 1)
	leaf(top+1, 1)
}

// rollLeafDrops spawns a decaying leaf's loot (5% sapling / 2% sticks / 0.5% apple).
func (h *hub) rollLeafDrops(players map[int32]*tracked, x, y, z int) {
	for _, d := range h.leafDrops() {
		h.spawnBlockDrop(players, d.item, d.count, x, y, z)
	}
}

// logNearby reports whether an oak log sits within 4 blocks (so leaves near a
// tree survive). Bounded so leaf ticks stay cheap.
func (h *hub) logNearby(x, y, z int) bool {
	const r = 4
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if s := h.world.At(x+dx, y+dy, z+dz); s >= worldgen.BlockBase("oak_log") && s <= worldgen.BlockBase("mangrove_log")+2 { // any log: oak..mangrove (axis x/y/z each)
					return true
				}
			}
		}
	}
	return false
}

// skyLit reports whether a column is open to the sky above (a daylight proxy for
// the light≥9 growth requirement — ignores torches and night).
func (h *hub) skyLit(x, y, z int) bool {
	for ay := y + 1; h.inWorldY(ay); ay++ {
		if worldgen.SkyOpacity(h.world.At(x, ay, z)) >= worldgen.Opaque {
			return false
		}
	}
	return true
}

// opaqueAbove reports whether the block directly above blocks light (smothers grass).
func (h *hub) opaqueAbove(x, y, z int) bool {
	return worldgen.SkyOpacity(h.world.At(x, y+1, z)) >= worldgen.Opaque
}

// lavaIgnite is the vanilla LavaFluid.randomTick fire-starter: an overworld
// lava block randomly sets fire to a nearby flammable block (using the
// flammability table as the ignitedByLava proxy). Gated by doFireTick.
func (h *hub) lavaIgnite(players map[int32]*tracked, x, y, z int) {
	if !h.rules.DoFireTick {
		return
	}
	flammableNear := func(px, py, pz int) bool {
		for _, d := range allNeighbors {
			if ig, _ := worldgen.Flammability(h.world.At(px+d.x, py+d.y, pz+d.z)); ig > 0 {
				return true
			}
		}
		return false
	}
	if passes := h.rng.Intn(3); passes > 0 {
		cx, cy, cz := x, y, z
		for i := 0; i < passes; i++ {
			cx += h.rng.Intn(3) - 1
			cy++
			cz += h.rng.Intn(3) - 1
			if !h.inWorldY(cy) {
				return
			}
			s := h.world.At(cx, cy, cz)
			if s == worldgen.Air && flammableNear(cx, cy, cz) {
				h.igniteFire(players, blockPos{cx, cy, cz}, 0)
				return
			}
			if worldgen.IsSolidFull(s) {
				return
			}
		}
		return
	}
	// passes == 0: ignite the air directly above a flammable block nearby.
	for i := 0; i < 3; i++ {
		ax, az := x+h.rng.Intn(3)-1, z+h.rng.Intn(3)-1
		if h.inWorldY(y+1) && h.world.At(ax, y, az) != worldgen.Air {
			if ig, _ := worldgen.Flammability(h.world.At(ax, y, az)); ig > 0 && h.world.At(ax, y+1, az) == worldgen.Air {
				h.igniteFire(players, blockPos{ax, y + 1, az}, 0)
			}
		}
	}
}

var (
	snowLayer1 = worldgen.BlockBase("snow") // 1-layer snow (base state = 1 layer)
	iceBlock   = worldgen.BlockBase("ice")
)

// precipTick freezes exposed water to ice and accumulates snow layers in cold
// biomes, a port of ServerLevel.tickPrecipitation restricted to one sampled
// column. Ice forms whenever a snowy column's surface water is exposed at an
// edge; snow only while it is actually snowing (raining in a cold biome).
func (h *hub) precipTick(players map[int32]*tracked, cx, cz int) {
	x := cx*16 + h.rng.Intn(16)
	z := cz*16 + h.rng.Intn(16)
	// Find the topmost non-air near the surface (water sits above the terrain).
	start := worldgen.SeaLevel + 4
	if g := h.world.GroundY(x, z); g > start {
		start = g
	}
	topY, top := 0, uint32(0)
	for y := start; y >= h.world.GroundY(x, z)-1 && h.inWorldY(y); y-- {
		if s := h.world.At(x, y, z); s != worldgen.Air {
			topY, top = y, s
			break
		}
	}
	if top == 0 {
		return
	}
	if worldgen.PrecipitationAt(h.world.BiomeAt(x, z), topY) != worldgen.PrecipSnow {
		return // not cold enough to snow/freeze here
	}
	if !h.skyExposedColumn(x, z) {
		return // sheltered columns don't freeze or accumulate
	}
	// Freeze: an exposed water SOURCE with a non-water edge neighbour becomes
	// ice (vanilla freezes edges first, so open water stays liquid).
	if top == worldgen.WaterBase {
		edge := false
		for _, d := range horizNeighbors {
			if !worldgen.IsWater(h.world.At(x+d.x, topY, z+d.z)) {
				edge = true
				break
			}
		}
		if edge {
			h.setBlock(players, blockPos{x, topY, z}, iceBlock)
		}
		return
	}
	// Snow: while snowing, lay a snow layer on a solid, snow-free surface.
	if h.raining && worldgen.IsSolidFull(top) && top != iceBlock &&
		h.world.At(x, topY+1, z) == worldgen.Air {
		h.setBlock(players, blockPos{x, topY + 1, z}, snowLayer1)
	}
}

var (
	melonStemBase       = worldgen.BlockBase("melon_stem")            // age 0..7
	pumpkinStemBase     = worldgen.BlockBase("pumpkin_stem")          // age 0..7
	attachedMelonBase   = worldgen.BlockBase("attached_melon_stem")   // facing N/S/W/E
	attachedPumpkinBase = worldgen.BlockBase("attached_pumpkin_stem") //
	melonBlock          = worldgen.BlockBase("melon")
	pumpkinBlock        = worldgen.BlockBase("pumpkin")
)

// stemFacing maps a horizontal delta to the attached-stem facing index
// (north=0, south=1, west=2, east=3).
var stemFacing = map[blockPos]uint32{
	{0, 0, -1}: 0, {0, 0, 1}: 1, {-1, 0, 0}: 2, {1, 0, 0}: 3,
}

var (
	cocoaBase = worldgen.BlockBase("cocoa")            // facing×age, age 0..2
	berryBase = worldgen.BlockBase("sweet_berry_bush") // age 0..3
)

// tickCocoa ripens a cocoa pod one age stage (0→2) at 1-in-5 odds, preserving
// its facing (CocoaBlock.randomTick). Returns whether it handled the block.
func (h *hub) tickCocoa(players map[int32]*tracked, x, y, z int, state uint32) bool {
	if state < cocoaBase || state > cocoaBase+11 {
		return false
	}
	if h.rng.Intn(5) != 0 { // vanilla nextInt(5)==0
		return true
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	age := worldgen.GetProperty(info, state, "age")
	if age == "0" || age == "1" { // age<2: advance one stage, facing untouched
		next := "1"
		if age == "1" {
			next = "2"
		}
		h.setBlock(players, blockPos{x, y, z}, worldgen.SetProperty(info, state, "age", next))
	}
	return true
}

// tickBerry ripens a sweet berry bush one age stage (0→3) at 1-in-5 odds when
// the bush is lit (SweetBerryBushBlock.randomTick — brightness≥9 ≈ sky-lit).
// Returns whether it handled the block.
func (h *hub) tickBerry(players map[int32]*tracked, x, y, z int, state uint32) bool {
	if state < berryBase || state > berryBase+3 {
		return false
	}
	if state < berryBase+3 && h.rng.Intn(5) == 0 && h.skyLit(x, y, z) {
		h.setBlock(players, blockPos{x, y, z}, state+1)
	}
	return true
}

// tickStem grows a melon/pumpkin stem: it ages to 7, then spawns its fruit in
// an adjacent free cell over tillable ground and turns into an attached stem
// (StemBlock.randomTick). Returns whether it handled the block.
func (h *hub) tickStem(players map[int32]*tracked, x, y, z int, state uint32) bool {
	var stemBase, attachedBase, fruit uint32
	switch {
	case state >= melonStemBase && state <= melonStemBase+7:
		stemBase, attachedBase, fruit = melonStemBase, attachedMelonBase, melonBlock
	case state >= pumpkinStemBase && state <= pumpkinStemBase+7:
		stemBase, attachedBase, fruit = pumpkinStemBase, attachedPumpkinBase, pumpkinBlock
	default:
		return false
	}
	if !h.skyLit(x, y, z) {
		return true
	}
	age := int(state - stemBase)
	if age < 7 {
		h.setBlock(players, blockPos{x, y, z}, stemBase+uint32(age+1))
		return true
	}
	// Mature: try to fruit in a random horizontal neighbour.
	d := horizNeighbors[h.rng.Intn(4)]
	fx, fz := x+d.x, z+d.z
	if h.world.At(fx, y, fz) != worldgen.Air {
		return true // occupied — no room this tick
	}
	below := h.world.At(fx, y-1, fz)
	if below != worldgen.Dirt && below != worldgen.GrassBlock &&
		!(below >= farmlandMin && below <= farmlandMin+7) {
		return true // fruit needs tillable/dirt/grass ground
	}
	h.setBlock(players, blockPos{fx, y, fz}, fruit)
	h.setBlock(players, blockPos{x, y, z}, attachedBase+stemFacing[d]) // attach toward the fruit
	return true
}
