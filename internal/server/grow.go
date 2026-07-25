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
)

// Sugar cane and cactus state ranges, looked up by NAME.
//
// These were hard-coded 1.21.5 ids (cane 5978-5993, cactus 5960-5975). At the
// canonical 1.21.11 those numbers belong to acacia and cherry HANGING SIGNS, so
// cane and cactus never grew, and hanging signs were fed to the stack-plant
// grower — which can stack a copy of the sign above itself. Never hard-code a
// state id: ids shift every version, names do not.
var (
	caneMin, caneMax     = worldgen.BlockRange("sugar_cane")
	cactusMin, cactusMax = worldgen.BlockRange("cactus")
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

// saplingSpecies pairs each sapling's state range with the tree it grows.
// Every species vanilla has a sapling for is listed: previously only oak,
// spruce and birch were matched at all, and all three grew an OAK tree
// because the grow path hard-coded oak logs and leaves.
type saplingSpec struct {
	rng   [2]uint32
	shape worldgen.TreeShape
}

var saplingSpecies = func() []saplingSpec {
	names := []string{"oak_sapling", "spruce_sapling", "birch_sapling",
		"jungle_sapling", "acacia_sapling", "cherry_sapling",
		"dark_oak_sapling", "pale_oak_sapling"}
	out := make([]saplingSpec, 0, len(names))
	for _, n := range names {
		shape, ok := worldgen.TreeShapeForSapling(n)
		if !ok {
			continue
		}
		lo, hi := worldgen.BlockRange(n)
		out = append(out, saplingSpec{rng: [2]uint32{lo, hi}, shape: shape})
	}
	return out
}()

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

// runRandomTicks ticks the loaded chunks around each player, in that player's
// OWN dimension.
//
// This used to ignore the dimension entirely: every player's x/z drove ticks
// against the overworld, so nothing in the Nether or the End ever ticked, and
// standing in the Nether grew an overworld farm at the same coordinates.
func (h *hub) runRandomTicks(players map[int32]*tracked) {
	if len(players) == 0 {
		return
	}
	// seen is keyed by dimension too: the same chunk coordinates in two worlds
	// are two different chunks.
	seen := map[[3]int]bool{}
	for _, t := range players {
		pcx, pcz := chunkFloor(t.x), chunkFloor(t.z)
		for dx := -simRadius; dx <= simRadius; dx++ {
			for dz := -simRadius; dz <= simRadius; dz++ {
				c := [3]int{t.dim, pcx + dx, pcz + dz}
				if seen[c] {
					continue
				}
				seen[c] = true
				h.randomTickChunk(players, t.dim, c[1], c[2])
			}
		}
	}
}

func (h *hub) randomTickChunk(players map[int32]*tracked, dim, cx, cz int) {
	if h.rng.Intn(16) == 0 { // vanilla tickPrecipitation: ~1 column/chunk sampled
		h.precipTick(players, dim, cx, cz)
	}
	for s := 0; s < h.worldFor(dim).Sections(); s++ {
		baseY := worldgen.MinY + s*16
		speed := randomTickSpeed
		if h.rules.RandomTicks >= 0 {
			speed = h.rules.RandomTicks // gamerule randomTickSpeed (0 = growth off)
		}
		for i := 0; i < speed; i++ {
			x := cx*16 + h.rng.Intn(16)
			y := baseY + h.rng.Intn(16)
			z := cz*16 + h.rng.Intn(16)
			h.randomTickBlock(players, dim, x, y, z)
		}
	}
}

func (h *hub) randomTickBlock(players map[int32]*tracked, dim, x, y, z int) {
	state := h.worldFor(dim).At(x, y, z)
	if worldgen.IsLava(state) {
		h.lavaIgnite(players, dim, x, y, z)
		return
	}
	if h.tickStem(players, dim, x, y, z, state) {
		return
	}
	if h.farmlandRandomTick(players, dim, x, y, z, state) {
		return
	}
	if h.tickTorchflower(players, dim, x, y, z, state) {
		return
	}
	if h.tickPitcher(players, dim, x, y, z, state) {
		return
	}
	if h.tickCocoa(players, dim, x, y, z, state) {
		return
	}
	if h.tickBerry(players, dim, x, y, z, state) {
		return
	}
	if h.tickCopper(players, dim, x, y, z, state) {
		return
	}
	if h.tickWart(players, dim, x, y, z, state) {
		return
	}
	if h.tickThaw(players, dim, x, y, z, state) {
		return
	}
	if h.tickAmethyst(players, dim, x, y, z, state) {
		return
	}
	if h.tickRedstoneOre(players, dim, x, y, z, state) {
		return
	}
	if h.tickNylium(players, dim, x, y, z, state) {
		return
	}
	if h.tickGrowingPlant(players, dim, x, y, z, state) {
		return
	}
	if h.tickMushroom(players, dim, x, y, z, state) {
		return
	}
	if h.tickBamboo(players, dim, x, y, z, state) {
		return
	}
	if h.tickBambooSapling(players, dim, x, y, z, state) {
		return
	}
	if h.tickChorus(players, dim, x, y, z, state) {
		return
	}
	switch {
	case inRange(state, [2]uint32{caneMin, caneMax}):
		h.tickStackPlant(players, dim, x, y, z, state, caneMin)
	case inRange(state, [2]uint32{cactusMin, cactusMax}):
		h.tickStackPlant(players, dim, x, y, z, state, cactusMin)
	default:
		if h.tickSpread(players, dim, x, y, z, state) {
			return
		}
		if h.tickDriedGhast(players, dim, x, y, z, state) {
			return
		}
		if h.tickCrop(players, dim, x, y, z, state) {
			return
		}
		if h.tickSapling(players, dim, x, y, z, state) {
			return
		}
		h.tickLeaf(players, dim, x, y, z, state)
	}
}

// tickStackPlant grows sugar cane / cactus: the top stalk ages each tick, and at
// age 15 it spawns a new stalk above (up to 3 tall), resetting its own age.
func (h *hub) tickStackPlant(players map[int32]*tracked, dim, x, y, z int, state uint32, min uint32) {
	if h.worldFor(dim).At(x, y+1, z) != worldgen.Air {
		return // only the top stalk (open above) grows
	}
	height := 1
	for k := 1; k < 3; k++ {
		if s := h.worldFor(dim).At(x, y-k, z); s >= min && s <= min+15 {
			height++
		} else {
			break
		}
	}
	if height >= 3 {
		return
	}
	if age := state - min; age >= 15 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, min)        // reset this stalk
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, min)    // new stalk above
		h.scheduleAroundIn(dim, blockPos{x, y + 1, z}, fallDelay) // support/neighbour recheck
	} else {
		h.setBlockAt(players, dim, blockPos{x, y, z}, min+age+1)
	}
}

// cropGrowthSpeed ports CropBlock.getGrowthSpeed: base 1.0, plus a farmland
// bonus under the crop (dry +1, moist +3; the eight diagonal/orthogonal cells
// weighted ÷4), then halved when the same crop flanks it on both axes (or on a
// diagonal) — the "planted in rows grows faster" rule. `r` is this crop's state
// range so a same-type neighbour of any age counts.
func (h *hub) cropGrowthSpeed(dim, x, y, z int, r [2]uint32) float64 {
	f := 1.0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			g := 0.0
			if s := h.worldFor(dim).At(x+i, y-1, z+j); s >= farmlandMin && s <= farmlandMin+7 {
				g = 1.0
				if s > farmlandMin { // moisture > 0
					g = 3.0
				}
			}
			if i != 0 || j != 0 {
				g /= 4.0
			}
			f += g
		}
	}
	sameNS := inRange(h.worldFor(dim).At(x, y, z-1), r) || inRange(h.worldFor(dim).At(x, y, z+1), r)
	sameEW := inRange(h.worldFor(dim).At(x-1, y, z), r) || inRange(h.worldFor(dim).At(x+1, y, z), r)
	if sameNS && sameEW {
		f /= 2.0
	} else if inRange(h.worldFor(dim).At(x-1, y, z-1), r) || inRange(h.worldFor(dim).At(x+1, y, z-1), r) ||
		inRange(h.worldFor(dim).At(x-1, y, z+1), r) || inRange(h.worldFor(dim).At(x+1, y, z+1), r) {
		f /= 2.0
	}
	return f
}

// plantBrightness is vanilla getRawBrightness(pos, darken) at a block: `darken`
// 0 is the true raw value (what CropBlock/StemBlock/SweetBerryBushBlock use),
// -1 applies the time-and-weather skyDarken (getMaxLocalRawBrightness, which is
// what SaplingBlock uses — so saplings, unlike crops, stall at night).
//
// This replaced an approximation that required an unobstructed column to the
// sky, which silently made artificial light useless for growing: a torch-lit
// indoor or underground farm grows in vanilla and did not here.
func (h *hub) plantBrightness(dim, x, y, z, darken int) int {
	sky, block := h.worldFor(dim).LightAt(x, y, z)
	return h.rawBrightness(sky, block, darken)
}

// blockLight is getBrightness(LightLayer.BLOCK, pos) — the artificial light
// alone, ignoring sky. Melting and freezing both use it rather than the
// combined brightness, which is why daylight never melts ice but a torch does.
func (h *hub) blockLight(dim, x, y, z int) int {
	_, block := h.worldFor(dim).LightAt(x, y, z)
	return int(block)
}

// tickThaw ports IceBlock.randomTick and SnowLayerBlock.randomTick: both melt
// when the BLOCK light exceeds 11. Ice becomes water, except where water
// evaporates (the Nether), where it simply goes.
func (h *hub) tickThaw(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	isIce := state == iceBlock
	isSnow := state >= snowLayer1 && state <= snowLayer1+7
	if !isIce && !isSnow {
		return false
	}
	if h.blockLight(dim, x, y, z) <= 11 {
		return true
	}
	if isSnow {
		// dropResources: one snowball per layer, then the block goes.
		h.spawnBlockDrop(players, itemByName["snowball"], int(state-snowLayer1)+1, x, y, z)
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
		return true
	}
	melted := worldgen.WaterBase
	if dim != 0 { // water evaporates in the Nether; the End has no water either
		melted = worldgen.Air
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, melted)
	h.scheduleAroundIn(dim, blockPos{x, y, z}, waterDelay)
	return true
}

// cropGrows rolls the vanilla growth gate: random.nextInt((int)(25/speed)+1)==0.
func (h *hub) cropGrows(dim, x, y, z int, r [2]uint32) bool {
	return h.rng.Intn(int(25.0/h.cropGrowthSpeed(dim, x, y, z, r))+1) == 0
}

// tickCrop advances a staged crop one stage if it's immature and sky-lit, gated
// by the vanilla growth-speed probability (was: advanced every random tick).
func (h *hub) tickCrop(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	for _, r := range cropRanges {
		if inRange(state, r) {
			if state < r[1] && h.plantBrightness(dim, x, y, z, 0) >= 9 && h.cropGrows(dim, x, y, z, r) {
				h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
			}
			return true
		}
	}
	return false
}

// tickSapling advances a sapling's hidden stage, then grows its species' tree.
func (h *hub) tickSapling(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	for _, sp := range saplingSpecies {
		if !inRange(state, sp.rng) {
			continue
		}
		// getMaxLocalRawBrightness(pos.above()) >= 9 && nextInt(7) == 0.
		// The 1-in-7 roll was missing, so saplings advanced on every random
		// tick that passed the light check — about seven times too fast.
		if h.plantBrightness(dim, x, y+1, z, -1) < 9 || h.rng.Intn(7) != 0 {
			return true
		}
		if state == sp.rng[0] {
			h.setBlockAt(players, dim, blockPos{x, y, z}, state+1) // stage 0 → 1
			return true
		}
		h.growSapling(players, dim, x, y, z, state, sp)
		return true
	}
	return false
}

// tickLeaf decays a non-persistent leaf with no log nearby, rolling its drops.
func (h *hub) tickLeaf(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	for _, r := range leafRanges {
		if !inRange(state, r) {
			continue
		}
		persistent := ((state-r[0])/2)%2 == 0 // middle property; idx 0 == "true"
		if persistent || h.logNearby(dim, x, y, z) {
			return
		}
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
		h.scheduleAroundIn(dim, blockPos{x, y, z}, fallDelay)
		h.rollLeafDrops(players, dim, x, y, z)
		return
	}
}

// spreaders are the SpreadingSnowyBlock family: a block that creeps over
// nearby dirt and reverts to dirt when smothered. Mycelium shares the class
// with grass in vanilla but was never dispatched here, so it did neither.
var spreaders = map[uint32]uint32{} // spreading state → the block it reverts to

func init() {
	spreaders[worldgen.GrassBlock] = worldgen.Dirt
	lo, hi := worldgen.BlockRange("mycelium")
	for st := lo; st <= hi; st++ { // mycelium carries `snowy`, like grass
		spreaders[st] = worldgen.Dirt
	}
	glo, ghi := worldgen.BlockRange("grass_block")
	for st := glo; st <= ghi; st++ {
		spreaders[st] = worldgen.Dirt
	}
}

// tickSpread ports SpreadingSnowyBlock.randomTick.
//
// Three things the grass-only version got wrong, all fixed here: vanilla makes
// FOUR spread attempts per tick (not one), the vertical offset is nextInt(5)-3
// (-3..+1, so it creeps down slopes) rather than -1..+1, and spreading is
// gated on brightness >= 9 at the block above.
func (h *hub) tickSpread(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	base, ok := spreaders[state]
	if !ok {
		return false
	}
	if h.opaqueAbove(dim, x, y, z) {
		h.setBlockAt(players, dim, blockPos{x, y, z}, base) // smothered → dirt
		return true
	}
	if h.plantBrightness(dim, x, y+1, z, -1) < 9 {
		return true // alive, but too dark to spread
	}
	for i := 0; i < 4; i++ {
		tx := x + h.rng.Intn(3) - 1
		ty := y + h.rng.Intn(5) - 3
		tz := z + h.rng.Intn(3) - 1
		if h.worldFor(dim).At(tx, ty, tz) == base && !h.opaqueAbove(dim, tx, ty, tz) {
			h.setBlockAt(players, dim, blockPos{tx, ty, tz}, state)
		}
	}
	return true
}

// growSapling replaces a mature sapling with its species' tree.
//
// Dark oak and pale oak are TwoByTwo: vanilla's growers give them a mega
// feature and NO single-sapling tree, so a lone one never grows. findSquare
// looks for the 2x2 the way vanilla does — scanning the four offsets that
// could place this sapling in a square — and the whole square is consumed.
func (h *hub) growSapling(players map[int32]*tracked, dim, x, y, z int, state uint32, sp saplingSpec) {
	if sp.shape.TwoByTwo {
		cx, cz, ok := h.findSaplingSquare(dim, x, z, y, sp.rng)
		if !ok {
			return // stays a sapling until a square is completed
		}
		for _, c := range [4][2]int{{cx, cz}, {cx + 1, cz}, {cx, cz + 1}, {cx + 1, cz + 1}} {
			h.setBlockAt(players, dim, blockPos{c[0], y, c[1]}, worldgen.Air)
		}
		h.stampTree(players, dim, cx, y, cz, sp.shape, true)
		return
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	h.stampTree(players, dim, x, y, z, sp.shape, false)
}

// findSaplingSquare reports the lower-left corner of a 2x2 block of matching
// saplings containing (x,z), scanning the same offsets vanilla does.
func (h *hub) findSaplingSquare(dim, x, z, y int, rng [2]uint32) (int, int, bool) {
	matches := func(px, pz int) bool { return inRange(h.worldFor(dim).At(px, y, pz), rng) }
	for dx := 0; dx >= -1; dx-- {
		for dz := 0; dz >= -1; dz-- {
			cx, cz := x+dx, z+dz
			if matches(cx, cz) && matches(cx+1, cz) && matches(cx, cz+1) && matches(cx+1, cz+1) {
				return cx, cz, true
			}
		}
	}
	return 0, 0, false
}

// stampTree writes a trunk and canopy for one species. `wide` grows the 2x2
// trunk and broader canopy the mega species need. Leaves only replace air, so
// a tree never eats a player's build.
func (h *hub) stampTree(players map[int32]*tracked, dim, x, y, z int, s worldgen.TreeShape, wide bool) {
	height := s.MinH + h.rng.Intn(s.ExtraH+1)
	top := y + height
	trunk := [][2]int{{x, z}}
	if wide {
		trunk = [][2]int{{x, z}, {x + 1, z}, {x, z + 1}, {x + 1, z + 1}}
	}
	for ty := y; ty < top; ty++ {
		for _, c := range trunk {
			h.setBlockAt(players, dim, blockPos{c[0], ty, c[1]}, s.Log)
		}
	}

	// Canopy is centred on the trunk; a 2x2 trunk shifts the centre half a
	// block, so widen by one on the far side instead of offsetting.
	leaf := func(ly, r int, trimCorners bool) {
		hi := r
		if wide {
			hi = r + 1
		}
		for dx := -r; dx <= hi; dx++ {
			for dz := -r; dz <= hi; dz++ {
				if trimCorners && abs(dx) == r && abs(dz) == r {
					continue // trim canopy corners
				}
				px, pz := x+dx, z+dz
				if h.worldFor(dim).At(px, ly, pz) == worldgen.Air {
					h.setBlockAt(players, dim, blockPos{px, ly, pz}, s.Leaves)
				}
			}
		}
	}
	if s.Conical { // spruce: stacked rings tapering to a point
		for i, ly := 0, top-4; ly <= top; ly++ {
			r := 2 - i/2
			if r < 0 {
				r = 0
			}
			leaf(ly, r, r == 2)
			i++
		}
		h.setBlockAt(players, dim, blockPos{x, top + 1, z}, s.Leaves)
		return
	}
	leaf(top-2, 2, true)
	leaf(top-1, 2, true)
	leaf(top, 1, false)
	leaf(top+1, 1, true)
}

// rollLeafDrops spawns a decaying leaf's loot (5% sapling / 2% sticks / 0.5% apple).
func (h *hub) rollLeafDrops(players map[int32]*tracked, dim, x, y, z int) {
	for _, d := range h.leafDrops() {
		h.spawnBlockDrop(players, d.item, d.count, x, y, z)
	}
}

// logNearby reports whether an oak log sits within 4 blocks (so leaves near a
// tree survive). Bounded so leaf ticks stay cheap.
func (h *hub) logNearby(dim, x, y, z int) bool {
	const r = 4
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if s := h.worldFor(dim).At(x+dx, y+dy, z+dz); s >= worldgen.BlockBase("oak_log") && s <= worldgen.BlockBase("mangrove_log")+2 { // any log: oak..mangrove (axis x/y/z each)
					return true
				}
			}
		}
	}
	return false
}

// opaqueAbove reports whether the block directly above blocks light (smothers grass).
func (h *hub) opaqueAbove(dim, x, y, z int) bool {
	return worldgen.SkyOpacity(h.worldFor(dim).At(x, y+1, z)) >= worldgen.Opaque
}

// lavaIgnite is the vanilla LavaFluid.randomTick fire-starter: an overworld
// lava block randomly sets fire to a nearby flammable block (using the
// flammability table as the ignitedByLava proxy). Gated by doFireTick.
func (h *hub) lavaIgnite(players map[int32]*tracked, dim, x, y, z int) {
	if !h.rules.DoFireTick {
		return
	}
	flammableNear := func(px, py, pz int) bool {
		for _, d := range allNeighbors {
			if ig, _ := worldgen.Flammability(h.worldFor(dim).At(px+d.x, py+d.y, pz+d.z)); ig > 0 {
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
			s := h.worldFor(dim).At(cx, cy, cz)
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
		if h.inWorldY(y+1) && h.worldFor(dim).At(ax, y, az) != worldgen.Air {
			if ig, _ := worldgen.Flammability(h.worldFor(dim).At(ax, y, az)); ig > 0 && h.worldFor(dim).At(ax, y+1, az) == worldgen.Air {
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
func (h *hub) precipTick(players map[int32]*tracked, dim, cx, cz int) {
	x := cx*16 + h.rng.Intn(16)
	z := cz*16 + h.rng.Intn(16)
	// Find the topmost non-air near the surface (water sits above the terrain).
	start := worldgen.SeaLevel + 4
	if g := h.worldFor(dim).GroundY(x, z); g > start {
		start = g
	}
	topY, top := 0, uint32(0)
	for y := start; y >= h.worldFor(dim).GroundY(x, z)-1 && h.inWorldY(y); y-- {
		if s := h.worldFor(dim).At(x, y, z); s != worldgen.Air {
			topY, top = y, s
			break
		}
	}
	if top == 0 {
		return
	}
	snowing := worldgen.PrecipitationAt(h.worldFor(dim).BiomeAt(x, z), topY) == worldgen.PrecipSnow
	if _, _, isCauldron := cauldronOf(top); isCauldron {
		if h.raining && h.skyExposedColumn(x, z) {
			h.cauldronPrecip(players, blockPos{x, topY, z}, top, snowing)
		}
		return
	}
	if !snowing {
		return // not cold enough to snow/freeze here
	}
	if !h.skyExposedColumn(x, z) {
		return // sheltered columns don't freeze or accumulate
	}
	// Freeze: an exposed water SOURCE with a non-water edge neighbour becomes
	// ice (vanilla freezes edges first, so open water stays liquid). Biome
	// .shouldFreeze also needs block light < 10, so a torch beside a pond keeps
	// it liquid — that gate was missing.
	if top == worldgen.WaterBase {
		if h.blockLight(dim, x, topY, z) >= 10 {
			return
		}
		edge := false
		for _, d := range horizNeighbors {
			if !worldgen.IsWater(h.worldFor(dim).At(x+d.x, topY, z+d.z)) {
				edge = true
				break
			}
		}
		if edge {
			h.setBlockAt(players, dim, blockPos{x, topY, z}, iceBlock)
		}
		return
	}
	// Snow: while snowing, lay a snow layer on a solid, snow-free surface.
	if h.raining && worldgen.IsSolidFull(top) && top != iceBlock &&
		h.worldFor(dim).At(x, topY+1, z) == worldgen.Air {
		h.setBlockAt(players, dim, blockPos{x, topY + 1, z}, snowLayer1)
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
func (h *hub) tickCocoa(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
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
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "age", next))
	}
	return true
}

// tickBerry ripens a sweet berry bush one age stage (0→3) at 1-in-5 odds when
// the bush is lit (SweetBerryBushBlock.randomTick: getRawBrightness(pos.above(), 0) >= 9).
// Returns whether it handled the block.
func (h *hub) tickBerry(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state < berryBase || state > berryBase+3 {
		return false
	}
	if state < berryBase+3 && h.rng.Intn(5) == 0 && h.plantBrightness(dim, x, y+1, z, 0) >= 9 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
	}
	return true
}

// tickStem grows a melon/pumpkin stem: it ages to 7, then spawns its fruit in
// an adjacent free cell over tillable ground and turns into an attached stem
// (StemBlock.randomTick). Returns whether it handled the block.
func (h *hub) tickStem(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	var stemBase, attachedBase, fruit uint32
	switch {
	case state >= melonStemBase && state <= melonStemBase+7:
		stemBase, attachedBase, fruit = melonStemBase, attachedMelonBase, melonBlock
	case state >= pumpkinStemBase && state <= pumpkinStemBase+7:
		stemBase, attachedBase, fruit = pumpkinStemBase, attachedPumpkinBase, pumpkinBlock
	default:
		return false
	}
	if h.plantBrightness(dim, x, y, z, 0) < 9 {
		return true
	}
	// Vanilla StemBlock.randomTick gates both the age-advance and the fruiting on
	// the same growth-speed probability (was: grew every random tick).
	if !h.cropGrows(dim, x, y, z, [2]uint32{stemBase, stemBase + 7}) {
		return true
	}
	age := int(state - stemBase)
	if age < 7 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, stemBase+uint32(age+1))
		return true
	}
	// Mature: try to fruit in a random horizontal neighbour.
	d := horizNeighbors[h.rng.Intn(4)]
	fx, fz := x+d.x, z+d.z
	if h.worldFor(dim).At(fx, y, fz) != worldgen.Air {
		return true // occupied — no room this tick
	}
	below := h.worldFor(dim).At(fx, y-1, fz)
	if below != worldgen.Dirt && below != worldgen.GrassBlock &&
		!(below >= farmlandMin && below <= farmlandMin+7) {
		return true // fruit needs tillable/dirt/grass ground
	}
	h.setBlockAt(players, dim, blockPos{fx, y, fz}, fruit)
	h.setBlockAt(players, dim, blockPos{x, y, z}, attachedBase+stemFacing[d]) // attach toward the fruit
	return true
}

// Torchflower and pitcher plant growth.
//
// Both are staged crops that the generic tickCrop path cannot express: the
// torchflower's last step REPLACES the crop with a different block, and the
// pitcher plant becomes two blocks tall partway through. Ported from
// TorchflowerCropBlock and PitcherCropBlock.

var (
	torchflowerCropMin, torchflowerCropMax = worldgen.BlockRange("torchflower_crop")
	pitcherCropMin, pitcherCropMax         = worldgen.BlockRange("pitcher_crop")
)

// pitcher_crop states run age-major, half-minor with half ordered
// [upper, lower] — verified against the 1.21.11 blocks report, and pinned by
// TestPitcherStateLayout. InfoForState/SetProperty cannot help here: they only
// cover orientable blocks.
func pitcherUpper(age int) uint32 { return pitcherCropMin + uint32(age)*2 }
func pitcherLower(age int) uint32 { return pitcherCropMin + uint32(age)*2 + 1 }

// pitcherAgeHalf splits a pitcher_crop state into its age and whether it is the
// lower half.
func pitcherAgeHalf(state uint32) (age int, lower bool) {
	off := state - pitcherCropMin
	return int(off / 2), off%2 == 1
}

// pitcherIsDouble mirrors PitcherCropBlock.isDouble: from age 3 the plant
// occupies two cells.
func pitcherIsDouble(age int) bool { return age >= 3 }

// tickTorchflower ports TorchflowerCropBlock. Its randomTick calls super only
// when nextInt(3) != 0, so two random ticks in three do anything at all; and
// getMaxAge() is 2 even though the AGE property stops at 1, so the final step
// swaps the crop for the torchflower block.
func (h *hub) tickTorchflower(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state < torchflowerCropMin || state > torchflowerCropMax {
		return false
	}
	if h.rng.Intn(3) == 0 {
		return true // vanilla skips this tick outright
	}
	r := [2]uint32{torchflowerCropMin, torchflowerCropMax}
	if h.plantBrightness(dim, x, y, z, 0) < 9 || !h.cropGrows(dim, x, y, z, r) {
		return true
	}
	if state < torchflowerCropMax {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
	} else {
		// getStateForAge(2) == TORCHFLOWER.defaultBlockState()
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.BlockID("torchflower"))
	}
	return true
}

// tickPitcher ports PitcherCropBlock.randomTick + grow. Only the lower half
// ticks and only while immature (isRandomlyTicking), and once the new age makes
// the plant double the upper half is written above it.
func (h *hub) tickPitcher(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state < pitcherCropMin || state > pitcherCropMax {
		return false
	}
	age, lower := pitcherAgeHalf(state)
	if !lower || age >= 4 { // isRandomlyTicking / isMaxAge
		return true
	}
	if !h.cropGrows(dim, x, y, z, [2]uint32{pitcherCropMin, pitcherCropMax}) {
		return true
	}
	newAge := age + 1

	// canGrow: sufficientLight is CropBlock.hasSufficientLight (>= 8, NOT the
	// >= 9 growth gate), the cell above must be in the world, and once the
	// plant goes double that cell must be air or already pitcher crop.
	if h.plantBrightness(dim, x, y, z, 0) < 8 || !h.inWorldY(y+1) {
		return true
	}
	if pitcherIsDouble(newAge) {
		above := h.worldFor(dim).At(x, y+1, z)
		if above != worldgen.Air && (above < pitcherCropMin || above > pitcherCropMax) {
			return true // canGrowInto: air or pitcher_crop only
		}
	}

	h.setBlockAt(players, dim, blockPos{x, y, z}, pitcherLower(newAge))
	if pitcherIsDouble(newAge) {
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, pitcherUpper(newAge))
	}
	return true
}
