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

// saplingSpecies pairs each sapling's state range with its TreeGrower — which
// vanilla features it grows, single and mega, and at what odds. The trees
// themselves come from the same PlaceTree the world generator uses, so a
// planted oak and a generated oak are the same tree by construction.
type saplingSpec struct {
	rng [2]uint32
	// single is the lone-sapling feature ("" = never grows alone: dark and
	// pale oak are mega-only). secondary is grown instead at secondaryChance —
	// the plain oak's occasional large oak.
	single, secondary string
	secondaryChance   float64
	// mega is the 2x2 feature ("" = a square of these grows four singles).
	// megaSecondary at the same odds — the spruce square's mega pine.
	mega, megaSecondary string
	// flowers variants: grown instead when a #flowers block sits within the
	// 5x5x3 box around the sapling (TreeGrower.hasFlowers) — the tree comes
	// up carrying a bee nest.
	flowers, secondaryFlowers string
}

// saplingSpecies is TreeGrower's table: species, features and odds are
// vanilla's, including the flowers-nearby bee variants.
var saplingSpecies = func() []saplingSpec {
	rows := []struct {
		name string
		sp   saplingSpec
	}{
		{"oak_sapling", saplingSpec{single: "oak", secondary: "fancy_oak", secondaryChance: 0.1,
			flowers: "oak_bees_005", secondaryFlowers: "fancy_oak_bees_005"}},
		{"spruce_sapling", saplingSpec{single: "spruce", mega: "mega_spruce", megaSecondary: "mega_pine", secondaryChance: 0.5}},
		{"birch_sapling", saplingSpec{single: "birch", flowers: "birch_bees_005"}},
		{"jungle_sapling", saplingSpec{single: "jungle_tree_no_vine", mega: "mega_jungle_tree"}},
		{"acacia_sapling", saplingSpec{single: "acacia"}},
		{"cherry_sapling", saplingSpec{single: "cherry", flowers: "cherry_bees_005"}},
		{"dark_oak_sapling", saplingSpec{mega: "dark_oak"}},
		// Vanilla's PALE_OAK grower uses the BONEMEAL feature: the bare tree,
		// no moss and never a heart — those belong to the wild ones.
		{"pale_oak_sapling", saplingSpec{mega: "pale_oak_bonemeal"}},
	}
	out := make([]saplingSpec, 0, len(rows))
	for _, r := range rows {
		lo, hi := worldgen.BlockRange(r.name)
		r.sp.rng = [2]uint32{lo, hi}
		out = append(out, r.sp)
	}
	return out
}()

// leafRanges are the leaf families we generate; the persistent property means
// player-placed leaves never decay. All species are listed so the distance
// rule and the drops behave consistently across every canopy.
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
	if h.scratchSeen3 == nil {
		h.scratchSeen3 = map[[3]int]bool{}
	}
	clear(h.scratchSeen3)
	seen := h.scratchSeen3
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
	// One chunk resolution for the whole chunk's reads (see world.ChunkReader):
	// this loop is the single hottest block-read site in the engine.
	rd := h.worldFor(dim).Reader(int32(cx), int32(cz))
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
			h.randomTickBlockState(players, dim, x, y, z, rd.At(x, y, z))
		}
	}
}

func (h *hub) randomTickBlock(players map[int32]*tracked, dim, x, y, z int) {
	h.randomTickBlockState(players, dim, x, y, z, h.worldFor(dim).At(x, y, z))
}

// randomTickBlockState is randomTickBlock with the block already read (the
// chunk loop reads through a pinned ChunkReader).
func (h *hub) randomTickBlockState(players map[int32]*tracked, dim, x, y, z int, state uint32) {
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
	if h.tickDripstone(players, dim, x, y, z, state) {
		return
	}
	if h.tickEyeblossom(players, dim, x, y, z, state) {
		return
	}
	if h.tickNetherPortal(players, dim, x, y, z, state) {
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
	if h.tickChorusPlant(players, dim, x, y, z, state) {
		return
	}
	if h.tickChorus(players, dim, x, y, z, state) {
		return
	}
	if h.tickPropagule(players, dim, x, y, z, state) {
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
		h.spawnBlockDrop(players, dim, itemByName["snowball"], int(state-snowLayer1)+1, x, y, z)
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

// Leaf decay, ported from LeavesBlock. A leaf carries a DISTANCE property,
// 1..7: a trunk block counts as 0, every leaf is min(neighbour)+1, and a
// non-persistent leaf at 7 rots on a random tick. The old rule here scanned a
// radius-4 box for any log, which is wrong in BOTH directions — a leaf on a
// 5-long chain of leaves survives in vanilla but sat outside the box, and a
// leaf floating near a log with no leaf path to it decayed in vanilla but
// passed the box. Cutting a trunk now sends the recompute out as a wave, and
// the canopy rots from the cut outward as the distances rise to 7.

// leafInfo unpacks a leaf state: its family base, distance and persistence.
// The states pack distance x persistent x waterlogged, waterlogged innermost.
func leafInfo(state uint32) (base uint32, distance int, persistent bool, ok bool) {
	for _, r := range leafRanges {
		if inRange(state, r) {
			idx := state - r[0]
			return r[0], int(idx/4) + 1, (idx/2)%2 == 0, true
		}
	}
	return 0, 0, false, false
}

// leafWithDistance rewrites a leaf state's distance, keeping its other bits.
func leafWithDistance(state uint32, base uint32, d int) uint32 {
	return base + uint32((d-1)*4) + (state-base)%4
}

// leafDistanceAt is a block's contribution to its neighbours' distance:
// vanilla getOptionalDistanceAt. Trunk blocks are 0, a leaf is its own
// distance, anything else contributes nothing (7 pre-increment).
func leafDistanceAt(state uint32) int {
	if isTrunkBlock(state) {
		return 0
	}
	if _, d, _, ok := leafInfo(state); ok {
		return d
	}
	return 7
}

// isTrunkBlock is vanilla's prevents_nearby_leaf_decay — the LOGS tag: every
// log, wood, stem and hyphae, stripped or not.
func isTrunkBlock(state uint32) bool {
	for _, r := range trunkRanges {
		if inRange(state, r) {
			return true
		}
	}
	return false
}

var trunkRanges = func() [][2]uint32 {
	names := []string{}
	for _, sp := range []string{"oak", "spruce", "birch", "jungle", "acacia",
		"dark_oak", "pale_oak", "mangrove", "cherry"} {
		names = append(names, sp+"_log", sp+"_wood", "stripped_"+sp+"_log", "stripped_"+sp+"_wood")
	}
	for _, sp := range []string{"crimson", "warped"} {
		names = append(names, sp+"_stem", sp+"_hyphae", "stripped_"+sp+"_stem", "stripped_"+sp+"_hyphae")
	}
	return blockRange(names...)
}()

// updateLeafDistance is the neighbour-change half: recompute this leaf's
// distance from its six neighbours and write it if it moved, which schedules
// the neighbours in turn — the wave.
func (h *hub) updateLeafDistance(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	base, d, _, ok := leafInfo(state)
	if !ok {
		return false
	}
	w := h.worldFor(dim)
	newD := 7
	for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if n := leafDistanceAt(w.At(x+o[0], y+o[1], z+o[2])) + 1; n < newD {
			newD = n
		}
	}
	if newD != d {
		h.setBlockAt(players, dim, blockPos{x, y, z}, leafWithDistance(state, base, newD))
	}
	return true
}

// tickLeaf rots a non-persistent leaf whose distance is 7 — after VERIFYING
// the distance with a bounded search. The verification is not optional
// caution: every canopy generated before this port carries the distance-7
// state its stamper wrote, and trusting it would rot every existing tree in
// the live world on its first random ticks. A stale leaf that turns out to be
// within reach of a trunk is healed with its true distance instead.
func (h *hub) tickLeaf(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	base, d, persistent, ok := leafInfo(state)
	if !ok || persistent {
		return
	}
	if d < 7 {
		return // healthy; decay only ever fires at 7
	}
	if trueD := h.leafTrueDistance(dim, x, y, z); trueD < 7 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, leafWithDistance(state, base, trueD))
		return
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	h.scheduleAroundIn(dim, blockPos{x, y, z}, fallDelay)
	h.rollLeafDrops(players, dim, x, y, z)
}

// leafTrueDistance walks outward through connected leaves for the nearest
// trunk block, up to the six steps that matter. Bounded by construction:
// at most the ball of radius 6 through leaf blocks.
func (h *hub) leafTrueDistance(dim, x, y, z int) int {
	w := h.worldFor(dim)
	seen := map[blockPos]bool{{x, y, z}: true}
	frontier := []blockPos{{x, y, z}}
	for d := 1; d <= 6; d++ {
		var next []blockPos
		for _, p := range frontier {
			for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				q := blockPos{p.x + o[0], p.y + o[1], p.z + o[2]}
				if seen[q] {
					continue
				}
				seen[q] = true
				s := w.At(q.x, q.y, q.z)
				if isTrunkBlock(s) {
					return d
				}
				if isAnyLeaf(s) {
					next = append(next, q)
				}
			}
		}
		frontier = next
	}
	return 7
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
	if sp.mega != "" {
		if cx, cz, ok := h.findSaplingSquare(dim, x, z, y, sp.rng); ok {
			feature := sp.mega
			if sp.megaSecondary != "" && h.rng.Float64() < sp.secondaryChance {
				feature = sp.megaSecondary
			}
			square := [4][2]int{{cx, cz}, {cx + 1, cz}, {cx, cz + 1}, {cx + 1, cz + 1}}
			for _, c := range square {
				h.setBlockAt(players, dim, blockPos{c[0], y, c[1]}, worldgen.Air)
			}
			if !h.placeLiveTree(players, dim, cx, y, cz, feature) {
				for _, c := range square { // no room: the saplings stay planted
					h.setBlockAt(players, dim, blockPos{c[0], y, c[1]}, state)
				}
			}
			return
		}
		if sp.single == "" {
			return // dark/pale oak: a lone sapling never grows
		}
	}
	feature := sp.single
	flowers := sp.flowers != "" && h.flowersNear(dim, x, y, z)
	if sp.secondary != "" && h.rng.Float64() < sp.secondaryChance {
		feature = sp.secondary
		if flowers && sp.secondaryFlowers != "" {
			feature = sp.secondaryFlowers
		}
	} else if flowers {
		feature = sp.flowers
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	if !h.placeLiveTree(players, dim, x, y, z, feature) {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state)
	}
}

// flowersNear is TreeGrower.hasFlowers: any #flowers block within the 5x5x3
// box around the sapling — what turns the grown tree into its bee variant.
func (h *hub) flowersNear(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	for dy := -1; dy <= 1; dy++ {
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				if worldgen.IsFlower(w.At(x+dx, y+dy, z+dz)) {
					return true
				}
			}
		}
	}
	return false
}

// placeLiveTree grows a vanilla tree feature in the LIVE world — the second of
// PlaceTree's two drivers, and the one with real blocks to answer with. The
// fit gate therefore means here exactly what it means in vanilla: a sapling
// under a roof or against a wall measures the space it needs and refuses,
// instead of punching its canopy through the building. On refusal the caller
// puts the sapling back.
func (h *hub) placeLiveTree(players map[int32]*tracked, dim, x, y, z int, feature string) bool {
	c, ok := worldgen.TreeFeatures[feature]
	if !ok {
		return false
	}
	w := h.worldFor(dim)
	// validTreePos: air, leaves or replaceable plants (vanilla's
	// replaceable_by_trees includes leaves — a new tree grows up through a
	// neighbouring canopy); plus logs for the fit gate, as isFree allows.
	free := func(px, py, pz int) bool {
		s := w.At(px, py, pz)
		return s == worldgen.Air || worldgen.IsLeaves(s) || worldgen.IsReplaceable(s) || worldgen.IsLog(s)
	}
	set := func(px, py, pz int, st uint32, leaf bool) {
		if leaf {
			cur := w.At(px, py, pz)
			if cur != worldgen.Air && !worldgen.IsLeaves(cur) && !worldgen.IsReplaceable(cur) {
				return // leaves never eat a solid block — or the trunk just placed
			}
		}
		h.setBlockAt(players, dim, blockPos{px, py, pz}, st)
	}
	// The live path reads the real world everywhere — the podzol decorator's
	// ground check, the setDirtAt test and their kin see exactly what a
	// vanilla server would.
	read := func(px, py, pz int) uint32 { return w.At(px, py, pz) }
	return worldgen.PlaceTree(c, x, y, z, h.rng, worldgen.TreeDriver{
		Set: set, Free: free, Read: read,
		DirtGround: func(px, py, pz int) bool { return worldgen.IsDirtTag(w.At(px, py, pz)) },
		// MOTION_BLOCKING_NO_LEAVES by scanning the real column down from the
		// default world top. Only the litter decorator asks, and no
		// sapling-grown feature carries litter — this is for completeness.
		SurfaceTop: func(px, pz int) int {
			for py := worldgen.MinY + worldgen.SectionCount*16 - 1; py >= worldgen.MinY; py-- {
				s := w.At(px, py, pz)
				if s != worldgen.Air && !worldgen.IsLeaves(s) && (worldgen.Collides(s) || worldgen.IsFluid(s)) {
					return py + 1
				}
			}
			return worldgen.MinY
		},
		RootThrough: func(px, py, pz int) bool { return worldgen.IsRootGrowThrough(w.At(px, py, pz)) },
	})
}

// growHugeMushroom drives PlaceHugeMushroom against the live world with
// vanilla's STRICT envelope — air or leaves only. A huge mushroom refuses
// even a blade of grass in its space, where a tree would grow through it.
func (h *hub) growHugeMushroom(players map[int32]*tracked, dim, x, y, z int, brown bool) bool {
	w := h.worldFor(dim)
	return worldgen.PlaceHugeMushroom(brown, x, y, z, h.rng, worldgen.TreeDriver{
		Set: func(px, py, pz int, st uint32, leaf bool) {
			h.setBlockAt(players, dim, blockPos{px, py, pz}, st)
		},
		Free: func(px, py, pz int) bool {
			s := w.At(px, py, pz)
			return s == worldgen.Air || worldgen.IsLeaves(s)
		},
		Read:       func(px, py, pz int) uint32 { return w.At(px, py, pz) },
		DirtGround: func(px, py, pz int) bool { return worldgen.IsDirtTag(w.At(px, py, pz)) },
	})
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

// rollLeafDrops spawns a decaying leaf's loot (5% sapling / 2% sticks / 0.5% apple).
func (h *hub) rollLeafDrops(players map[int32]*tracked, dim, x, y, z int) {
	for _, d := range h.leafDrops() {
		h.spawnBlockDrop(players, dim, d.item, d.count, x, y, z)
	}
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
