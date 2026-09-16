package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bone meal, part two: the targets that place a FEATURE or spread. Nylium
// sprouts nether vegetation (NetherForestVegetationFeature, and on warped
// nylium sprouts and, one time in eight, twisting vines), netherrack beside
// nylium turns into it, a fungus on its nylium grows a huge fungus two
// times in five (HugeFungusFeature, planted), a moss block lays a moss
// patch with its vegetation (VegetationPatchFeature), glow lichen spreads
// a face (MultifaceSpreader), hanging moss lengthens, pale moss carpet
// climbs, mangrove leaves drop a propagule, bushes, firefly bushes and
// dry grass spread, and melon and pumpkin stems age.

var (
	crimsonNylium   = worldgen.BlockBase("crimson_nylium")
	warpedNylium    = worldgen.BlockBase("warped_nylium")
	netherrackState = worldgen.BlockBase("netherrack")
	soulSoilState   = worldgen.BlockBase("soul_soil")
	crimsonFungus   = worldgen.BlockBase("crimson_fungus")
	warpedFungus    = worldgen.BlockBase("warped_fungus")
	crimsonRoots    = worldgen.BlockBase("crimson_roots")
	warpedRoots     = worldgen.BlockBase("warped_roots")
	netherSprouts   = worldgen.BlockBase("nether_sprouts")
	warpedWartBlock = worldgen.BlockBase("warped_wart_block")
	netherWartBlock = worldgen.BlockBase("nether_wart_block")
	shroomlight     = worldgen.BlockBase("shroomlight")
	glowLichenRange = blockRange("glow_lichen")[0]
	hangingMossRng  = blockRange("pale_hanging_moss")[0]
	paleCarpetRange = blockRange("pale_moss_carpet")[0]
	mangroveLeaves  = blockRange("mangrove_leaves")[0]
	bushState       = worldgen.BlockID("bush")
	fireflyBush     = worldgen.BlockID("firefly_bush")
	shortDryGrass   = worldgen.BlockID("short_dry_grass")
	tallDryGrass    = worldgen.BlockID("tall_dry_grass")
	baseStoneRanges = blockRange("stone", "granite", "diorite", "andesite", "tuff", "deepslate")
	caveVinesRanges = blockRange("cave_vines", "cave_vines_plant")
	sandRanges      = blockRange("sand", "red_sand", "suspicious_sand", "suspicious_gravel")
	terracottaRngs  = blockRange("terracotta", "white_terracotta", "orange_terracotta", "magenta_terracotta", "light_blue_terracotta",
		"yellow_terracotta", "lime_terracotta", "pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta",
		"purple_terracotta", "blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta", "black_terracotta")
)

func isNylium(s uint32) bool { return s == crimsonNylium || s == warpedNylium }

// netherPlantMayPlaceOn is FungusBlock/RootsBlock/NetherSproutsBlock
// .mayPlaceOn: nylium, soul soil, mycelium, or what any plant takes.
func netherPlantMayPlaceOn(s uint32) bool {
	return isNylium(s) || s == soulSoilState || worldgen.IsDirtTag(s) || isFarmland(s)
}

// weightedPick returns an index drawn by weight.
func (h *hub) weightedPick(weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	r := h.rng.Intn(total)
	for i, w := range weights {
		if r < w {
			return i
		}
		r -= w
	}
	return len(weights) - 1
}

// applyBoneMealFeatures is the tail of the bone-meal dispatch.
func (h *hub) applyBoneMealFeatures(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	w := h.worldFor(dim)
	pos := blockPos{x, y, z}
	info, _ := worldgen.InfoForState(state)

	// StemBlock: two to five ages, and at seven the fruiting tick.
	for _, base := range []uint32{melonStemBase, pumpkinStemBase} {
		if state >= base && state <= base+7 {
			age := int(state - base)
			if age == 7 {
				return false
			}
			age = min(7, age+2+h.rng.Intn(4))
			h.setBlockAt(players, dim, pos, base+uint32(age))
			if age == 7 {
				h.tickStem(players, dim, x, y, z, base+7)
			}
			return true
		}
	}
	// BambooSaplingBlock: the first stalk segment.
	if state == bambooSapling {
		if w.At(x, y+1, z) != worldgen.Air || !h.inWorldYIn(dim, y+1) {
			return false
		}
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, bambooState(0, bambooLeavesSmall, 0))
		return true
	}
	// NetherrackBlock: nylium spreads onto it from any of the 26 cells about.
	if state == netherrackState {
		if worldgen.Collides(w.At(x, y+1, z)) {
			return false
		}
		red, blue := false, false
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					switch w.At(x+dx, y+dy, z+dz) {
					case crimsonNylium:
						red = true
					case warpedNylium:
						blue = true
					}
				}
			}
		}
		switch {
		case red && blue:
			if h.rng.Intn(2) == 0 {
				h.setBlockAt(players, dim, pos, warpedNylium)
			} else {
				h.setBlockAt(players, dim, pos, crimsonNylium)
			}
		case blue:
			h.setBlockAt(players, dim, pos, warpedNylium)
		case red:
			h.setBlockAt(players, dim, pos, crimsonNylium)
		default:
			return false
		}
		return true
	}
	// NyliumBlock: the forest's vegetation about the cell above.
	if isNylium(state) {
		if w.At(x, y+1, z) != worldgen.Air || !h.inWorldYIn(dim, y+1) {
			return false
		}
		if state == crimsonNylium {
			h.netherForestVegetation(players, dim, x, y+1, z, []uint32{crimsonRoots, crimsonFungus, warpedFungus}, []int{87, 11, 1})
		} else {
			h.netherForestVegetation(players, dim, x, y+1, z, []uint32{warpedRoots, crimsonRoots, warpedFungus, crimsonFungus}, []int{85, 1, 13, 1})
			h.netherForestVegetation(players, dim, x, y+1, z, []uint32{netherSprouts}, []int{1})
			if h.rng.Intn(8) == 0 {
				h.twistingVinesFeature(players, dim, x, y+1, z, 3, 1, 2)
			}
		}
		return true
	}
	// NetherFungusBlock: on its own nylium, a huge fungus two times in five.
	if state == crimsonFungus || state == warpedFungus {
		want := crimsonNylium
		if state == warpedFungus {
			want = warpedNylium
		}
		if w.At(x, y-1, z) != want || !h.inWorldYIn(dim, y+1) {
			return false
		}
		if h.rng.Float64() < 0.4 {
			h.placeHugeFungus(players, dim, x, y, z, state == warpedFungus)
		}
		return true
	}
	// MossBlock (BonemealableFeaturePlacerBlock): a moss patch above.
	if state == worldgen.MossBlock {
		if w.At(x, y+1, z) != worldgen.Air {
			return false
		}
		h.mossPatch(players, dim, x, y+1, z)
		return true
	}
	// GlowLichenBlock: one more face, somewhere it can hold.
	if inRange(state, glowLichenRange) {
		return h.spreadLichen(players, dim, pos, state, info)
	}
	// HangingMossBlock: one more segment below the tip.
	if inRange(state, hangingMossRng) {
		tip := pos
		for inRange(w.At(tip.x, tip.y-1, tip.z), hangingMossRng) {
			tip.y--
		}
		if w.At(tip.x, tip.y-1, tip.z) != worldgen.Air || !h.inWorldYIn(dim, tip.y-1) {
			return false
		}
		h.setBlockAt(players, dim, tip, worldgen.SetProperty(info, w.At(tip.x, tip.y, tip.z), "tip", "false"))
		h.setBlockAt(players, dim, blockPos{tip.x, tip.y - 1, tip.z}, worldgen.SetProperty(info, state, "tip", "true"))
		return true
	}
	// MossyCarpetBlock (pale moss carpet): a base carpet grows a topper up
	// the walls beside it.
	if inRange(state, paleCarpetRange) {
		if worldgen.GetProperty(info, state, "bottom") != "true" {
			return false
		}
		topper, ok := h.paleCarpetTopper(dim, pos, info)
		if !ok {
			return false
		}
		h.setBlockAt(players, dim, blockPos{x, y + 1, z}, topper)
		return true
	}
	// MangroveLeavesBlock: a propagule hangs below.
	if inRange(state, mangroveLeaves) {
		if w.At(x, y-1, z) != worldgen.Air {
			return false
		}
		h.setBlockAt(players, dim, blockPos{x, y - 1, z}, propaguleState(0, true, 0, false))
		return true
	}
	// BushBlock / FireflyBushBlock: a copy on a free neighbour.
	if state == bushState || state == fireflyBush {
		return h.spreadToNeighbour(players, dim, pos, state, func(below uint32) bool { return worldgen.IsDirtTag(below) || isFarmland(below) })
	}
	// ShortDryGrassBlock → tall; TallDryGrassBlock spreads short.
	if state == shortDryGrass {
		h.setBlockAt(players, dim, pos, tallDryGrass)
		return true
	}
	if state == tallDryGrass {
		return h.spreadToNeighbour(players, dim, pos, shortDryGrass, dryVegetationMayPlaceOn)
	}
	return false
}

// dryVegetationMayPlaceOn is #dry_vegetation_may_place_on: sand, terracotta,
// dirt, farmland.
func dryVegetationMayPlaceOn(s uint32) bool {
	return inRanges2(s, sandRanges) || inRanges2(s, terracottaRngs) || worldgen.IsDirtTag(s) || isFarmland(s)
}

// spreadToNeighbour is BonemealableBlock.findSpreadableNeighbourPos: a
// shuffled horizontal neighbour that is air and would hold the plant.
func (h *hub) spreadToNeighbour(players map[int32]*tracked, dim int, pos blockPos, place uint32, mayPlaceOn func(uint32) bool) bool {
	w := h.worldFor(dim)
	order := h.rng.Perm(4)
	for _, i := range order {
		d := horizNeighbors[i]
		nx, nz := pos.x+d.x, pos.z+d.z
		if w.At(nx, pos.y, nz) == worldgen.Air && mayPlaceOn(w.At(nx, pos.y-1, nz)) {
			h.setBlockAt(players, dim, blockPos{nx, pos.y, nz}, place)
			return true
		}
	}
	return false
}

// netherForestVegetation is NetherForestVegetationFeature.place with spread
// width 3 and height 1: nine tries about the origin, each a weighted plant
// on empty ground it can hold.
func (h *hub) netherForestVegetation(players map[int32]*tracked, dim, x, y, z int, states []uint32, weights []int) {
	w := h.worldFor(dim)
	if !isNylium(w.At(x, y-1, z)) {
		return
	}
	for i := 0; i < 9; i++ {
		px := x + h.rng.Intn(3) - h.rng.Intn(3)
		pz := z + h.rng.Intn(3) - h.rng.Intn(3)
		s := states[h.weightedPick(weights)]
		if w.At(px, y, pz) == worldgen.Air && netherPlantMayPlaceOn(w.At(px, y-1, pz)) {
			h.setBlockAt(players, dim, blockPos{px, y, pz}, s)
		}
	}
}

// twistingVinesFeature is TwistingVinesFeature.place: width² tries about
// the origin, each dropped to the first air above ground and, on netherrack,
// warped nylium or warped wart, a column one to maxHeight tall (doubled one
// time in six, one tall one time in five), body below a head aged 17–25.
func (h *hub) twistingVinesFeature(players map[int32]*tracked, dim, x, y, z, width, height, maxHeight int) {
	w := h.worldFor(dim)
	invalid := func(px, py, pz int) bool {
		if w.At(px, py, pz) != worldgen.Air {
			return true
		}
		b := w.At(px, py-1, pz)
		return b != netherrackState && b != warpedNylium && b != warpedWartBlock
	}
	if invalid(x, y, z) {
		return
	}
	tw := growingPlants[1]
	for i := 0; i < width*width; i++ {
		px := x + h.rng.Intn(2*width+1) - width
		py := y + h.rng.Intn(2*height+1) - height
		pz := z + h.rng.Intn(2*width+1) - width
		for w.At(px, py, pz) == worldgen.Air && h.inWorldYIn(dim, py-1) { // findFirstAirBlockAboveGround
			py--
		}
		py++
		if !h.inWorldYIn(dim, py) || invalid(px, py, pz) {
			continue
		}
		n := 1 + h.rng.Intn(maxHeight)
		if h.rng.Intn(6) == 0 {
			n *= 2
		}
		if h.rng.Intn(5) == 0 {
			n = 1
		}
		for k := 1; k <= n; k++ {
			if w.At(px, py, pz) == worldgen.Air {
				if k == n || w.At(px, py+1, pz) != worldgen.Air {
					h.setBlockAt(players, dim, blockPos{px, py, pz}, tw.headAt(17+h.rng.Intn(9), false))
					break
				}
				h.setBlockAt(players, dim, blockPos{px, py, pz}, tw.body)
			}
			py++
		}
	}
}

// weepingVinesColumn is WeepingVinesFeature.placeWeepingVinesColumn: down
// from pos, body until the last cell (or a block below), which is a head
// aged minAge–maxAge.
func (h *hub) weepingVinesColumn(players map[int32]*tracked, dim, x, y, z, total, minAge, maxAge int) {
	w := h.worldFor(dim)
	wv := growingPlants[2]
	for k := 0; k <= total; k++ {
		if w.At(x, y, z) == worldgen.Air {
			if k == total || w.At(x, y-1, z) != worldgen.Air {
				h.setBlockAt(players, dim, blockPos{x, y, z}, wv.headAt(minAge+h.rng.Intn(maxAge-minAge+1), false))
				break
			}
			h.setBlockAt(players, dim, blockPos{x, y, z}, wv.body)
		}
		y--
	}
}

// placeHugeFungus is HugeFungusFeature.place for a planted fungus, on the
// shared placer (worldgen.PlaceHugeFungus grows the forests' too); a plant
// the stem pushes through breaks with its drops.
func (h *hub) placeHugeFungus(players map[int32]*tracked, dim, x, y, z int, warped bool) {
	w := h.worldFor(dim)
	worldgen.PlaceHugeFungus(h.rng, x, y, z, warped, true, worldgen.FungusDriver{
		Read: func(px, py, pz int) uint32 { return w.At(px, py, pz) },
		Set:  func(px, py, pz int, s uint32) { h.setBlockAt(players, dim, blockPos{px, py, pz}, s) },
		Destroy: func(px, py, pz int) {
			s := w.At(px, py, pz)
			if h.rules.DoTileDrops {
				for _, d := range h.evalBlockLoot(lootCtx{state: s, rng: h.rng.Intn, randf: h.rng.Float64}) {
					h.spawnBlockDrop(players, dim, d.item, d.count, px, py, pz)
				}
			}
		},
		InWorld: func(py int) bool { return h.inWorldYIn(dim, py) },
	})
}

// mossReplaceable is #moss_replaceable: the overworld's base stone, cave
// vines, and dirt.
func mossReplaceable(s uint32) bool {
	return inRanges2(s, baseStoneRanges) || inRanges2(s, caveVinesRanges) || worldgen.IsDirtTag(s)
}

// mossPatch is VegetationPatchFeature with MOSS_PATCH_BONEMEAL: a patch two
// to three wide each way (corners skipped, edges three times in four), each
// column found by dropping up to five cells to the floor, its ground turned
// to moss one deep where replaceable, and on three in five of those the
// moss vegetation (flowering azalea 4, azalea 7, moss carpet 25, short
// grass 50, tall grass 10).
func (h *hub) mossPatch(players map[int32]*tracked, dim, x, y, z int) {
	w := h.worldFor(dim)
	xr, zr := 1+1+h.rng.Intn(2), 1+1+h.rng.Intn(2)
	var surface []blockPos
	for dx := -xr; dx <= xr; dx++ {
		edgeX := dx == -xr || dx == xr
		for dz := -zr; dz <= zr; dz++ {
			edgeZ := dz == -zr || dz == zr
			corner := edgeX && edgeZ
			edge := (edgeX || edgeZ) && !corner
			if corner || (edge && h.rng.Float64() > 0.75) {
				continue
			}
			px, py, pz := x+dx, y, z+dz
			for i := 0; i < 5 && w.At(px, py, pz) == worldgen.Air; i++ {
				py--
			}
			for i := 0; i < 5 && w.At(px, py, pz) != worldgen.Air; i++ {
				py++
			}
			ground := w.At(px, py-1, pz)
			if w.At(px, py, pz) != worldgen.Air || !worldgen.Collides(ground) {
				continue
			}
			if ground != worldgen.MossBlock { // placeGround, depth 1
				if !mossReplaceable(ground) {
					continue
				}
				h.setBlockAt(players, dim, blockPos{px, py - 1, pz}, worldgen.MossBlock)
			}
			surface = append(surface, blockPos{px, py - 1, pz})
		}
	}
	states := []uint32{worldgen.BlockID("flowering_azalea"), worldgen.BlockID("azalea"), worldgen.BlockID("moss_carpet"), shortGrassState, worldgen.BlockID("tall_grass")}
	weights := []int{4, 7, 25, 50, 10}
	for _, g := range surface {
		if h.rng.Float64() >= 0.6 {
			continue
		}
		p := blockPos{g.x, g.y + 1, g.z}
		if w.At(p.x, p.y, p.z) != worldgen.Air {
			continue
		}
		s := states[h.weightedPick(weights)]
		if s == states[4] { // tall grass: both halves, room above
			if w.At(p.x, p.y+1, p.z) != worldgen.Air {
				continue
			}
			h.placeDoublePlant(players, dim, p, s)
			continue
		}
		h.setBlockAt(players, dim, p, s)
	}
}

// spreadLichen is MultifaceSpreader.spreadFromRandomFaceTowardRandomDirection
// for glow lichen: from a face it has, toward a direction off that axis it
// lacks, the first of SAME_POSITION, SAME_PLANE and WRAP_AROUND that lands
// on air, water or lichen with a solid block to hold the new face.
func (h *hub) spreadLichen(players map[int32]*tracked, dim int, pos blockPos, state uint32, info worldgen.BlockInfo) bool {
	w := h.worldFor(dim)
	hasFace := func(s uint32, i int) bool {
		si, _ := worldgen.InfoForState(s)
		return worldgen.GetProperty(si, s, faceDirs[i].prop) == "true"
	}
	opposite := func(i int) int { return i ^ 1 } // faceDirs pairs: down/up, north/south, west/east
	axis := func(i int) int { return i / 2 }
	canPlace := func(p blockPos, face int) (uint32, bool) {
		s := w.At(p.x, p.y, p.z)
		lichen := inRange(s, glowLichenRange)
		if !(s == worldgen.Air || lichen || s == worldgen.Water) {
			return 0, false
		}
		if lichen && hasFace(s, face) {
			return 0, false
		}
		d := faceDirs[face].d
		if !holdsBlock(w.At(p.x+d[0], p.y+d[1], p.z+d[2])) {
			return 0, false
		}
		base := worldgen.BlockID("glow_lichen")
		bi, _ := worldgen.InfoForState(base)
		switch {
		case lichen:
			base = s
		case s == worldgen.Water:
			base = worldgen.SetProperty(bi, base, "waterlogged", "true")
		}
		return worldgen.SetProperty(bi, base, faceDirs[face].prop, "true"), true
	}
	for _, from := range h.rng.Perm(6) {
		if !hasFace(state, from) {
			continue
		}
		for _, dir := range h.rng.Perm(6) {
			if axis(dir) == axis(from) || hasFace(state, dir) {
				continue
			}
			dd, fd := faceDirs[dir].d, faceDirs[from].d
			tries := [3]struct {
				p    blockPos
				face int
			}{
				{pos, dir}, // SAME_POSITION
				{blockPos{pos.x + dd[0], pos.y + dd[1], pos.z + dd[2]}, from},                                  // SAME_PLANE
				{blockPos{pos.x + dd[0] + fd[0], pos.y + dd[1] + fd[1], pos.z + dd[2] + fd[2]}, opposite(dir)}, // WRAP_AROUND
			}
			for _, t := range tries {
				if s, ok := canPlace(t.p, t.face); ok {
					h.setBlockAt(players, dim, t.p, s)
					return true
				}
			}
		}
	}
	return false
}

// paleCarpetTopper is MossyCarpetBlock.createTopperWithSideChance with every
// side kept: the cell above (air, or a non-base carpet) gets a base-less
// carpet with a low side wherever the neighbour of that cell holds it.
func (h *hub) paleCarpetTopper(dim int, pos blockPos, info worldgen.BlockInfo) (uint32, bool) {
	w := h.worldFor(dim)
	above := blockPos{pos.x, pos.y + 1, pos.z}
	prev := w.At(above.x, above.y, above.z)
	prevCarpet := inRange(prev, paleCarpetRange)
	if prevCarpet && worldgen.GetProperty(info, prev, "bottom") == "true" {
		return 0, false
	}
	if !prevCarpet && !(prev == worldgen.Air || worldgen.IsReplaceable(prev)) {
		return 0, false
	}
	s := worldgen.SetProperty(info, worldgen.BlockID("pale_moss_carpet"), "bottom", "false")
	any := false
	for _, f := range faceDirs[2:] { // the four sides
		side := "none"
		if holdsBlock(w.At(above.x+f.d[0], above.y, above.z+f.d[2])) {
			side, any = "low", true
		}
		s = worldgen.SetProperty(info, s, f.prop, side)
	}
	if !any || s == prev {
		return 0, false
	}
	return s, true
}
