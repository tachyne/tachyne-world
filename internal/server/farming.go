package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Tilling and planting — the two halves of starting a farm by hand.
//
// Both were missing. A hoe had no use-on-block behaviour at all, and none of
// the planting items could be placed, because protocol.BlockForItem is built by
// matching an item's name to a block's and every planting item is deliberately
// named differently from the block it becomes (wheat_seeds -> wheat, carrot ->
// carrots, ...). Farms could only be built in creative, by placing farmland and
// crop blocks directly.
//
// Ported from HoeItem.TILLABLES, the ItemNameBlockItem registrations in Items,
// and the mayPlaceOn/canSurvive rules on CropBlock, StemBlock and
// NetherWartBlock.

var (
	dirtBlock       = worldgen.BlockBase("dirt")
	grassBlock      = worldgen.BlockBase("grass_block")
	coarseDirtBlock = worldgen.BlockBase("coarse_dirt")
	dirtPathBlock   = worldgen.BlockBase("dirt_path")
	soulSandBlock   = worldgen.BlockBase("soul_sand")

	itemHangingRoots = itemByName["hanging_roots"]
)

// tillResult is what a hoe turns a block into.
//
// Vanilla gates every TILLABLES entry on onlyIfAirAbove EXCEPT rooted dirt,
// which tills from any face and pops hanging roots — hence a per-entry flag
// rather than one blanket check.
type tillResult struct {
	into     uint32
	drop     int32 // item popped by the conversion; 0 for none
	airAbove bool  // require a clear cell above and a face other than DOWN
}

// tillables mirrors HoeItem.TILLABLES.
var tillables = map[uint32]tillResult{
	grassBlock:      {into: farmlandMin, airAbove: true},
	dirtPathBlock:   {into: farmlandMin, airAbove: true},
	dirtBlock:       {into: farmlandMin, airAbove: true},
	coarseDirtBlock: {into: dirtBlock, airAbove: true},
	rootedDirtBlock: {into: dirtBlock, drop: itemHangingRoots},
}

// isHoe reports whether the item is one of the six hoes.
func isHoe(item int32) bool {
	switch item {
	case itemByName["wooden_hoe"], itemByName["stone_hoe"], itemByName["iron_hoe"],
		itemByName["golden_hoe"], itemByName["diamond_hoe"], itemByName["netherite_hoe"]:
		return true
	}
	return false
}

// tryTill handles a hoe used on a block. Reports whether it consumed the click;
// a non-tillable block returns false so normal placement carries on, matching
// vanilla's InteractionResult.PASS.
func (s *Server) tryTill(p *player, x, y, z int, dir int32, seq int32) bool {
	if !isHoe(p.heldItem()) {
		return false
	}
	res, ok := tillables[s.worldFor(p).Block(x, y, z)]
	if !ok {
		return false
	}
	if res.airAbove {
		// HoeItem.onlyIfAirAbove: face != DOWN (0) and the cell above is air.
		if dir == 0 || !worldgen.IsReplaceable(s.worldFor(p).At(x, y+1, z)) {
			return false
		}
	}

	s.putBlock(p, x, y, z, res.into, true, seq)
	if res.drop != 0 {
		s.hub.post(evPopItem{item: res.drop, count: 1, dim: p.dim,
			x: float64(x) + 0.5, y: float64(y) + 0.5, z: float64(z) + 0.5})
	}
	if s.modes.get(p.name) == gmSurvival {
		s.hub.post(evToolWear{eid: p.eid, slot: p.held})
	}
	return true
}

// cropPlant is a plantable item's target block plus the ground rule it needs.
type cropPlant struct {
	block uint32
	// needsLight mirrors CropBlock.canSurvive's hasSufficientLight gate. It is
	// NOT universal: melon/pumpkin stems are StemBlock (VegetationBlock), and
	// nether wart is likewise, so neither checks light — only the CropBlock
	// family does.
	needsLight bool
	// soulSand selects NetherWartBlock's #supports_nether_wart ground instead
	// of CropBlock's #supports_crops (which contains only farmland).
	soulSand bool
}

// cropForSeed maps a planting item to what it plants. These are vanilla's
// ItemNameBlockItem registrations, where the item and block names differ on
// purpose — which is exactly why the generated item->block table misses them.
//
// pitcher_pod is deliberately absent: PitcherCropBlock is a DoublePlantBlock
// needing two cells and a HALF property, so it belongs with the other two-tall
// placements rather than here.
func cropForSeed(item int32) (cropPlant, bool) {
	switch item {
	case itemByName["wheat_seeds"]:
		return cropPlant{block: worldgen.BlockBase("wheat"), needsLight: true}, true
	case itemByName["carrot"]:
		return cropPlant{block: worldgen.BlockBase("carrots"), needsLight: true}, true
	case itemByName["potato"]:
		return cropPlant{block: worldgen.BlockBase("potatoes"), needsLight: true}, true
	case itemByName["beetroot_seeds"]:
		return cropPlant{block: worldgen.BlockBase("beetroots"), needsLight: true}, true
	case itemByName["torchflower_seeds"]:
		return cropPlant{block: worldgen.BlockBase("torchflower_crop"), needsLight: true}, true
	case itemByName["melon_seeds"]:
		return cropPlant{block: worldgen.BlockBase("melon_stem")}, true
	case itemByName["pumpkin_seeds"]:
		return cropPlant{block: worldgen.BlockBase("pumpkin_stem")}, true
	case itemNetherWart:
		return cropPlant{block: worldgen.BlockBase("nether_wart"), soulSand: true}, true
	}
	return cropPlant{}, false
}

// isFarmland reports whether a state is farmland at any moisture level.
func isFarmland(state uint32) bool {
	return state >= farmlandMin && state <= farmlandMin+7
}

// canPlantAt applies the plant's ground and light rules at the cell it would
// occupy: mayPlaceOn against the block below, then CropBlock.canSurvive's
// light>=8 where that family applies it.
func (s *Server) canPlantAt(p *player, c cropPlant, x, y, z int) bool {
	below := s.worldFor(p).At(x, y-1, z)
	if c.soulSand {
		if below != soulSandBlock {
			return false
		}
	} else if !isFarmland(below) {
		return false
	}
	if !c.needsLight {
		return true
	}
	// LevelLightEngine.getRawBrightness(pos, 0) = max(blockLight, skyLight).
	sky, block := s.worldFor(p).LightAt(x, y, z)
	return max(sky, block) >= 8
}
