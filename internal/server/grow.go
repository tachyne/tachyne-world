package server

import "tachyne/internal/worldgen"

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
	for s := 0; s < h.world.Sections(); s++ {
		baseY := worldgen.MinY + s*16
		for i := 0; i < randomTickSpeed; i++ {
			x := cx*16 + h.rng.Intn(16)
			y := baseY + h.rng.Intn(16)
			z := cz*16 + h.rng.Intn(16)
			h.randomTickBlock(players, x, y, z)
		}
	}
}

func (h *hub) randomTickBlock(players map[int32]*tracked, x, y, z int) {
	state := h.world.At(x, y, z)
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
