package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The rest of vanilla's BonemealableBlocks. bonemeal.go covers crops,
// saplings, mushrooms, azaleas, cocoa, berry bushes and grass blocks; this
// file adds the others, each from its performBonemeal: tall flowers pop a
// copy, short grass and ferns grow tall, flower beds gain a petal (or pop a
// copy at four), sea pickles spread over coral, seagrass grows tall, the
// growing plants (kelp, the nether vines, cave vines) lengthen or fruit,
// dripleaves grow, rooted dirt sprouts roots, bamboo shoots up, hanging
// propagules ripen, the sniffer's crops advance, and bone meal on water
// seeds seagrass (and corals in a warm ocean). Nylium, fungi, moss and
// lichen — the feature-driven ones — are still to come.

var (
	tallFlowerRanges = blockRange("sunflower", "lilac", "rose_bush", "peony")
	tallFlowerItems  = []string{"sunflower", "lilac", "rose_bush", "peony"}
	flowerBedRanges  = blockRange("pink_petals", "wildflowers")
	flowerBedItems   = []string{"pink_petals", "wildflowers"}
	shortGrassState  = worldgen.BlockBase("short_grass")
	fernState        = worldgen.BlockBase("fern")
	seagrassState    = worldgen.BlockBase("seagrass")
	rootedDirtState  = worldgen.BlockBase("rooted_dirt")
	seaPickleRange   = blockRange("sea_pickle")[0]
	bigDripleafRange = blockRange("big_dripleaf")[0]
	dripleafStemRng  = blockRange("big_dripleaf_stem")[0]
	smallDripleafRng = blockRange("small_dripleaf")[0]
	coralBlockRanges = blockRange("tube_coral_block", "brain_coral_block", "bubble_coral_block", "fire_coral_block", "horn_coral_block")
	// #underwater_bonemeals: seagrass, the corals and the coral fans; #wall_corals.
	underwaterBonemeals = []string{"seagrass",
		"tube_coral", "brain_coral", "bubble_coral", "fire_coral", "horn_coral",
		"tube_coral_fan", "brain_coral_fan", "bubble_coral_fan", "fire_coral_fan", "horn_coral_fan"}
	wallCorals = []string{"tube_coral_wall_fan", "brain_coral_wall_fan", "bubble_coral_wall_fan", "fire_coral_wall_fan", "horn_coral_wall_fan"}
)

func isCoralBlock(s uint32) bool { return inRanges2(s, coralBlockRanges) }

// segmentAmount reads a flower bed's petal count (segment_amount, or the
// older flower_amount).
func segmentAmount(info worldgen.BlockInfo, s uint32) (string, int) {
	for _, name := range []string{"segment_amount", "flower_amount"} {
		if v := worldgen.GetProperty(info, s, name); v != "" {
			return name, atoi(v)
		}
	}
	return "", 0
}

// popCopy is Block.popResource(new ItemStack(this)).
func (h *hub) popCopy(players map[int32]*tracked, dim int, name string, x, y, z int) {
	if id, ok := itemByName[name]; ok {
		h.spawnBlockDrop(players, dim, int32(id), 1, x, y, z)
	}
}

// applyBoneMealMore handles the targets bonemeal.go does not; it returns
// (valid target, and so the meal is spent).
func (h *hub) applyBoneMealMore(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	w := h.worldFor(dim)
	pos := blockPos{x, y, z}
	info, _ := worldgen.InfoForState(state)

	// TallFlowerBlock: a copy pops out.
	for i, r := range tallFlowerRanges {
		if inRange(state, r) {
			h.popCopy(players, dim, tallFlowerItems[i], x, y, z)
			return true
		}
	}
	// TallGrassBlock: short grass and ferns grow tall, given the room.
	if state == shortGrassState || state == fernState {
		if w.At(x, y+1, z) != worldgen.Air || !h.inWorldYIn(dim, y+1) {
			return false
		}
		grown := "tall_grass"
		if state == fernState {
			grown = "large_fern"
		}
		h.placeDoublePlant(players, dim, pos, worldgen.BlockID(grown))
		return true
	}
	// FlowerBedBlock: another petal, or at four a copy pops out.
	for i, r := range flowerBedRanges {
		if inRange(state, r) {
			if name, n := segmentAmount(info, state); n > 0 && n < 4 {
				h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, state, name, itoa(n+1)))
			} else {
				h.popCopy(players, dim, flowerBedItems[i], x, y, z)
			}
			return true
		}
	}
	// SeaPickleBlock: alive (in water) on a coral block: pickles sprout on the
	// coral around it and the clicked one fills out to four.
	if inRange(state, seaPickleRange) {
		if worldgen.GetProperty(info, state, "waterlogged") != "true" || !isCoralBlock(w.At(x, y-1, z)) {
			return false
		}
		h.spreadSeaPickles(players, dim, x, y, z)
		h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, state, "pickles", "4"))
		return true
	}
	// SeagrassBlock: tall seagrass, given water above.
	if state == seagrassState {
		if w.At(x, y+1, z) != worldgen.Water {
			return false
		}
		h.placeDoublePlant(players, dim, pos, worldgen.BlockID("tall_seagrass"))
		return true
	}
	// GrowingPlantHeadBlock / GrowingPlantBodyBlock: kelp and the nether vines
	// lengthen from the head; cave vines fruit instead.
	for _, g := range growingPlants {
		if state >= g.headLo && state <= g.headHi {
			if g.berryStride == 2 { // CaveVinesBlock: berries on a bare head
				if g.headAt(g.age(state), true) == state {
					return false
				}
				h.setBlockAt(players, dim, pos, g.headAt(g.age(state), true))
				return true
			}
			return h.bonemealPlantHead(players, dim, pos, g, state)
		}
		if g.berryStride == 2 && inRange(state, blockRange("cave_vines_plant")[0]) { // CaveVinesPlantBlock
			if worldgen.GetProperty(info, state, "berries") == "true" {
				return false
			}
			h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, state, "berries", "true"))
			return true
		}
		if state == g.body { // the body: find the head and grow it
			hx, hy, hz := x, y, z
			for i := 0; i < 64; i++ {
				hy += g.dy
				s := w.At(hx, hy, hz)
				if s >= g.headLo && s <= g.headHi {
					return h.bonemealPlantHead(players, dim, blockPos{hx, hy, hz}, g, s)
				}
				if s != g.body {
					return false
				}
			}
			return false
		}
	}
	// BigDripleafBlock / BigDripleafStemBlock: one more stem, the leaf on top.
	if inRange(state, bigDripleafRange) {
		return h.growBigDripleaf(players, dim, pos, worldgen.GetProperty(info, state, "facing"))
	}
	if inRange(state, dripleafStemRng) {
		top := pos
		for i := 0; i < 64; i++ {
			top.y++
			s := w.At(top.x, top.y, top.z)
			if inRange(s, bigDripleafRange) {
				return h.growBigDripleaf(players, dim, top, worldgen.GetProperty(info, state, "facing"))
			}
			if !inRange(s, dripleafStemRng) {
				return false
			}
		}
		return false
	}
	// SmallDripleafBlock: becomes a big dripleaf two to five tall.
	if inRange(state, smallDripleafRng) {
		lower := pos
		if worldgen.GetProperty(info, state, "half") == "upper" {
			lower.y--
			if !inRange(w.At(lower.x, lower.y, lower.z), smallDripleafRng) {
				return false
			}
		}
		facing := worldgen.GetProperty(info, state, "facing")
		h.setBlockAt(players, dim, blockPos{lower.x, lower.y + 1, lower.z}, fluidOnly(w.At(lower.x, lower.y+1, lower.z)))
		h.placeBigDripleafRandomHeight(players, dim, lower, facing)
		return true
	}
	// RootedDirtBlock: hanging roots below.
	if state == rootedDirtState {
		if w.At(x, y-1, z) != worldgen.Air || !h.inWorldYIn(dim, y-1) {
			return false
		}
		h.setBlockAt(players, dim, blockPos{x, y - 1, z}, worldgen.BlockID("hanging_roots"))
		return true
	}
	// BambooStalkBlock: one or two segments on top.
	if isBamboo(state) {
		return h.bonemealBamboo(players, dim, x, y, z)
	}
	// MangrovePropaguleBlock: a hanging one ripens; a planted one grows the tree.
	if isPropagule(state) {
		if propaguleHanging(state) {
			if propaguleAge(state) >= 4 {
				return false
			}
			h.setBlockAt(players, dim, pos, propaguleState(propaguleAge(state)+1, true, propaguleStage(state), propaguleWet(state)))
			return true
		}
		return h.bonemealSapling(players, dim, x, y, z, state)
	}
	// TorchflowerCropBlock: the increase is at least two, so the flower blooms.
	if state >= torchflowerCropMin && state <= torchflowerCropMax {
		h.setBlockAt(players, dim, pos, worldgen.BlockID("torchflower"))
		return true
	}
	// PitcherCropBlock: one age, on the lower half.
	if state >= pitcherCropMin && state <= pitcherCropMax {
		age, lower := pitcherAgeHalf(state)
		if !lower {
			pos.y--
			below := w.At(pos.x, pos.y, pos.z)
			if below < pitcherCropMin || below > pitcherCropMax {
				return false
			}
			age, _ = pitcherAgeHalf(below)
		}
		if age >= 4 {
			return false
		}
		newAge := age + 1
		if h.plantBrightness(dim, pos.x, pos.y, pos.z, 0) < 8 || !h.inWorldYIn(dim, pos.y+1) {
			return false
		}
		if pitcherIsDouble(newAge) {
			above := w.At(pos.x, pos.y+1, pos.z)
			if above != worldgen.Air && (above < pitcherCropMin || above > pitcherCropMax) {
				return false
			}
		}
		h.setBlockAt(players, dim, pos, pitcherLower(newAge))
		if pitcherIsDouble(newAge) {
			h.setBlockAt(players, dim, blockPos{pos.x, pos.y + 1, pos.z}, pitcherUpper(newAge))
		}
		return true
	}
	return false
}

// bonemealSapling is the sapling path for a planted propagule (SaplingBlock
// .performBonemeal → advanceTree at 45%, which the sapling case in
// applyBoneMeal already does for its own ranges).
func (h *hub) bonemealSapling(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if h.rng.Float64() < 0.45 {
		h.advancePropagule(players, dim, x, y, z, state)
	}
	return true
}

// placeDoublePlant is DoublePlantBlock.placeAt: the lower half at pos and
// the upper above it.
func (h *hub) placeDoublePlant(players map[int32]*tracked, dim int, pos blockPos, lower uint32) {
	info, _ := worldgen.InfoForState(lower)
	h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, lower, "half", "lower"))
	h.setBlockAt(players, dim, blockPos{pos.x, pos.y + 1, pos.z}, worldgen.SetProperty(info, lower, "half", "upper"))
}

// bonemealPlantHead is GrowingPlantHeadBlock.performBonemeal: the head
// steps forward getBlocksToGrowWhenBonemealed times (kelp one, the nether
// vines a 0.826-series run), each cell it leaves becoming body.
func (h *hub) bonemealPlantHead(players map[int32]*tracked, dim int, pos blockPos, g growingPlant, state uint32) bool {
	w := h.worldFor(dim)
	canGrowInto := func(s uint32) bool {
		if g.intoWater {
			return s == worldgen.Water
		}
		return s == worldgen.Air
	}
	if !canGrowInto(w.At(pos.x, pos.y+g.dy, pos.z)) || !h.inWorldYIn(dim, pos.y+g.dy) {
		return false
	}
	n := 1
	if !g.intoWater { // NetherVines.getBlocksToGrowWhenBonemealed
		n = 0
		for p := 1.0; h.rng.Float64() < p; p *= 0.826 {
			n++
		}
	}
	age := min(g.age(state)+1, growingPlantMaxAge)
	cur := pos
	for i := 0; i < n; i++ {
		next := blockPos{cur.x, cur.y + g.dy, cur.z}
		if !canGrowInto(w.At(next.x, next.y, next.z)) || !h.inWorldYIn(dim, next.y) {
			break
		}
		h.setBlockAt(players, dim, cur, g.body)
		h.setBlockAt(players, dim, next, g.headAt(age, false))
		cur = next
		age = min(age+1, growingPlantMaxAge)
	}
	return true
}

// spreadSeaPickles is SeaPickleBlock.performBonemeal's diamond: two blocks
// out sideways, the two cells at and just below the clicked height, one in
// six over water on coral takes one to four pickles.
func (h *hub) spreadSeaPickles(players map[int32]*tracked, dim, x, y, z int) {
	w := h.worldFor(dim)
	base := worldgen.BlockBase("sea_pickle")
	info, _ := worldgen.InfoForState(base)
	zSpan, zOff := 1, 0
	for i := 0; i < 5; i++ {
		px := x - 2 + i
		for j := 0; j < zSpan; j++ {
			pz := z - zOff + j
			for py := y - 1; py <= y; py++ {
				if px == x && py == y && pz == z || h.rng.Intn(6) != 0 || w.At(px, py, pz) != worldgen.Water || !isCoralBlock(w.At(px, py-1, pz)) {
					continue
				}
				s := worldgen.SetProperty(info, base, "pickles", itoa(1+h.rng.Intn(4)))
				s = worldgen.SetProperty(info, s, "waterlogged", "true")
				h.setBlockAt(players, dim, blockPos{px, py, pz}, s)
			}
		}
		if i < 2 {
			zSpan, zOff = zSpan+2, zOff+1
		} else {
			zSpan, zOff = zSpan-2, zOff-1
		}
	}
}

// dripleafReplaceable is BigDripleafBlock.canReplace: air, water or a small
// dripleaf.
func dripleafReplaceable(s uint32) bool {
	return s == worldgen.Air || s == worldgen.Water || inRange(s, smallDripleafRng)
}

// fluidOnly keeps the water of a cell and drops the block.
func fluidOnly(s uint32) uint32 {
	if s == worldgen.Water || worldgen.IsWaterlogged(s) {
		return worldgen.Water
	}
	return worldgen.Air
}

func dripleafStemState(facing string, wet bool) uint32 {
	s := worldgen.BlockID("big_dripleaf_stem")
	info, _ := worldgen.InfoForState(s)
	s = worldgen.SetProperty(info, s, "facing", facing)
	if wet {
		s = worldgen.SetProperty(info, s, "waterlogged", "true")
	}
	return s
}

func bigDripleafState(facing string, wet bool) uint32 {
	s := worldgen.BlockID("big_dripleaf")
	info, _ := worldgen.InfoForState(s)
	s = worldgen.SetProperty(info, s, "facing", facing)
	if wet {
		s = worldgen.SetProperty(info, s, "waterlogged", "true")
	}
	return s
}

// growBigDripleaf is BigDripleafBlock.performBonemeal: the leaf becomes a
// stem and a new leaf opens above.
func (h *hub) growBigDripleaf(players map[int32]*tracked, dim int, leaf blockPos, facing string) bool {
	w := h.worldFor(dim)
	above := w.At(leaf.x, leaf.y+1, leaf.z)
	if !dripleafReplaceable(above) || !h.inWorldYIn(dim, leaf.y+1) {
		return false
	}
	h.setBlockAt(players, dim, leaf, dripleafStemState(facing, worldgen.IsWaterlogged(w.At(leaf.x, leaf.y, leaf.z))))
	h.setBlockAt(players, dim, blockPos{leaf.x, leaf.y + 1, leaf.z}, bigDripleafState(facing, above == worldgen.Water))
	return true
}

// placeBigDripleafRandomHeight is BigDripleafBlock.placeWithRandomHeight:
// two to five tall, as far as the cells allow, stems below the leaf.
func (h *hub) placeBigDripleafRandomHeight(players map[int32]*tracked, dim int, bottom blockPos, facing string) {
	w := h.worldFor(dim)
	want := 2 + h.rng.Intn(4)
	height := 0
	for height < want && dripleafReplaceable(w.At(bottom.x, bottom.y+height, bottom.z)) {
		height++
	}
	if height == 0 {
		return
	}
	for i := 0; i < height-1; i++ {
		p := blockPos{bottom.x, bottom.y + i, bottom.z}
		h.setBlockAt(players, dim, p, dripleafStemState(facing, w.At(p.x, p.y, p.z) == worldgen.Water))
	}
	p := blockPos{bottom.x, bottom.y + height - 1, bottom.z}
	h.setBlockAt(players, dim, p, bigDripleafState(facing, w.At(p.x, p.y, p.z) == worldgen.Water))
}

// bonemealBamboo is BambooStalkBlock.performBonemeal: one or two segments on
// the top of the stalk, while it is under sixteen, uncapped, and clear.
func (h *hub) bonemealBamboo(players map[int32]*tracked, dim, x, y, z int) bool {
	w := h.worldFor(dim)
	above := 0
	for isBamboo(w.At(x, y+above+1, z)) && above < bambooMaxHeight {
		above++
	}
	below := h.bambooHeightBelow(dim, x, y, z)
	total := above + below + 1
	topY := y + above
	if total >= bambooMaxHeight || bambooStage(w.At(x, topY, z)) == 1 || w.At(x, topY+1, z) != worldgen.Air || !h.inWorldYIn(dim, topY+1) {
		return false
	}
	for n := 1 + h.rng.Intn(2); n > 0; n-- {
		top := w.At(x, topY, z)
		if total >= bambooMaxHeight || bambooStage(top) == 1 || w.At(x, topY+1, z) != worldgen.Air || !h.inWorldYIn(dim, topY+1) {
			return true
		}
		h.growBambooAt(players, dim, x, topY, z, top, total)
		topY++
		total++
	}
	return true
}

// bonemealWater is BoneMealItem.growWaterPlant: on a full water cell beside
// a sturdy face, up to 128 tries seed seagrass (in a warm ocean, corals and
// coral fans a quarter of the time, and a wall fan on the clicked face
// first), each try wandering a step further; seagrass met on the way grows
// tall one time in ten.
func (h *hub) bonemealWater(players map[int32]*tracked, dim, x, y, z, dx, dy, dz int) bool {
	w := h.worldFor(dim)
	if w.At(x, y, z) != worldgen.Water {
		return false
	}
	warm := dim == dimOverworld && h.world.BiomeAt(x, z) == "minecraft:warm_ocean"
	horizontal := dy == 0
	facing := faceOfOffset(dx, dy, dz)
tries:
	for j := 0; j < 128; j++ {
		px, py, pz := x, y, z
		for i := 0; i < j/16; i++ {
			px += h.rng.Intn(3) - 1
			py += (h.rng.Intn(3) - 1) * h.rng.Intn(3) / 2
			pz += h.rng.Intn(3) - 1
			if worldgen.Collides(w.At(px, py, pz)) {
				continue tries
			}
		}
		grow := worldgen.BlockID("seagrass")
		wall := false
		if warm {
			if j == 0 && horizontal {
				grow, wall = worldgen.BlockID(wallCorals[h.rng.Intn(len(wallCorals))]), true
			} else if h.rng.Intn(4) == 0 {
				grow = worldgen.BlockID(underwaterBonemeals[h.rng.Intn(len(underwaterBonemeals))])
			}
		}
		if wall {
			info, _ := worldgen.InfoForState(grow)
			f := facing
			for d := 0; d < 4 && !h.wallCoralSurvives(dim, px, py, pz, f); d++ {
				f = [4]string{"north", "south", "west", "east"}[h.rng.Intn(4)]
			}
			grow = worldgen.SetProperty(info, grow, "facing", f)
			if !h.wallCoralSurvives(dim, px, py, pz, f) {
				continue
			}
		} else if !worldgen.Collides(w.At(px, py-1, pz)) {
			continue // seagrass, corals and fans need solid ground
		}
		at := w.At(px, py, pz)
		switch {
		case at == worldgen.Water:
			h.setBlockAt(players, dim, blockPos{px, py, pz}, grow)
		case at == seagrassState && w.At(px, py+1, pz) == worldgen.Water && h.rng.Intn(10) == 0:
			h.placeDoublePlant(players, dim, blockPos{px, py, pz}, worldgen.BlockID("tall_seagrass"))
		}
	}
	return true
}

func faceOfOffset(dx, dy, dz int) string {
	switch {
	case dy > 0:
		return "up"
	case dy < 0:
		return "down"
	case dx > 0:
		return "east"
	case dx < 0:
		return "west"
	case dz > 0:
		return "south"
	}
	return "north"
}

// wallCoralSurvives: the block behind a wall fan (opposite its facing) is
// solid.
func (h *hub) wallCoralSurvives(dim, x, y, z int, facing string) bool {
	bx, bz := x, z
	switch facing {
	case "north":
		bz++
	case "south":
		bz--
	case "west":
		bx++
	case "east":
		bx--
	}
	return worldgen.Collides(h.worldFor(dim).At(bx, y, bz))
}
