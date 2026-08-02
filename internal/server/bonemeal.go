package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bone meal (BoneMealItem.applyBonemeal), reimplemented from the vanilla
// source: right-click a growable block to advance it — crops age up 2–5
// stages, saplings grow at 45%, and grass blocks scatter short grass and the
// occasional flower around them. Consumes one bone meal and sparks the green
// growth particle.

const particleHappyVillager = 42 // canonical-770 minecraft:happy_villager

var (
	itemBoneMeal         = int32(itemByName["bone_meal"])
	azaleaBlockState     = worldgen.BlockBase("azalea")
	floweringAzaleaState = worldgen.BlockBase("flowering_azalea")
	redMushroomState     = worldgen.BlockBase("red_mushroom")
	brownMushroomState   = worldgen.BlockBase("brown_mushroom")
	bmFlowerBlocks       = []uint32{
		worldgen.BlockBase("dandelion"), worldgen.BlockBase("poppy"),
		worldgen.BlockBase("azure_bluet"), worldgen.BlockBase("cornflower"),
		worldgen.BlockBase("oxeye_daisy"),
	}
)

type evBoneMeal struct {
	eid     int32
	x, y, z int
	slot    int32
}

func (evBoneMeal) isHubEvent() {}

// onBoneMeal applies bone meal to the clicked block; returns nothing but
// consumes the item and emits particles when it takes effect.
func (h *hub) onBoneMeal(players map[int32]*tracked, e evBoneMeal) {
	t := players[e.eid]
	if t == nil {
		return
	}
	w := h.worldFor(t.dim)
	state := w.At(e.x, e.y, e.z)
	if !h.applyBoneMeal(players, t.dim, e.x, e.y, e.z, state) {
		return
	}
	if t.gamemode == gmSurvival {
		s := &t.inv.slots[e.slot]
		if s.item == itemBoneMeal {
			if s.count--; s.count <= 0 {
				*s = invStack{}
			}
			h.sendSlot(t, int(e.slot))
		}
	}
	h.toNearbyEv(players, t.dim, float64(e.x), float64(e.z), attachproto.Particles{
		PID: particleHappyVillager, X: float64(e.x) + 0.5, Y: float64(e.y) + 0.5, Z: float64(e.z) + 0.5,
		Spread: 0.3, Count: 15})
}

// applyBoneMeal advances the block, returning whether it did anything.
func (h *hub) applyBoneMeal(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	// Crops: age up 2–5 stages, capped at maturity (CropBlock.getBonemealAgeIncrease).
	for _, r := range cropRanges {
		if inRange(state, r) {
			if state >= r[1] {
				return false // already mature
			}
			ns := state + uint32(2+h.rng.Intn(4)) // Mth.nextInt(2,5)
			if ns > r[1] {
				ns = r[1]
			}
			h.setBlockAt(players, dim, blockPos{x, y, z}, ns)
			return true
		}
	}
	// Saplings: 45% chance to advance (SaplingBlock.advanceTree). Stage 0 goes
	// to stage 1; only a stage-1 sapling grows the tree — bone meal took the
	// tree path straight from stage 0 before, skipping a step.
	for _, sp := range saplingSpecies {
		if inRange(state, sp.rng) {
			if h.rng.Float64() < 0.45 {
				if state == sp.rng[0] {
					h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
				} else {
					h.growSapling(players, dim, x, y, z, state, sp)
				}
			}
			return true // vanilla consumes the meal either way
		}
	}
	// Small mushrooms: 40% to grow the huge mushroom in place
	// (MushroomBlock.growMushroom lifts the mushroom out for the attempt and
	// restores it when the feature refuses the spot).
	if state == redMushroomState || state == brownMushroomState {
		if h.rng.Float64() < 0.4 {
			h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
			if !h.growHugeMushroom(players, dim, x, y, z, state == brownMushroomState) {
				h.setBlockAt(players, dim, blockPos{x, y, z}, state)
			}
		}
		return true // consumed either way
	}
	// Azalea and flowering azalea: 45% to grow the azalea tree in place
	// (AzaleaBlock.performBonemeal via the AZALEA grower). The bush is not
	// replaceable-by-trees, so the grower lifts it out first and puts it
	// back if the tree refuses the spot.
	if state == azaleaBlockState || state == floweringAzaleaState {
		if h.rng.Float64() < 0.45 {
			h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
			if !h.placeLiveTree(players, dim, x, y, z, "azalea_tree") {
				h.setBlockAt(players, dim, blockPos{x, y, z}, state)
			}
		}
		return true // consumed either way
	}
	// Cocoa: advance one age stage, capped at 2 (CocoaBlock is bonemealable).
	if state >= cocoaBase && state <= cocoaBase+11 {
		if info, ok := worldgen.InfoForState(state); ok {
			switch worldgen.GetProperty(info, state, "age") {
			case "0":
				h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "age", "1"))
				return true
			case "1":
				h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "age", "2"))
				return true
			}
		}
		return false // already ripe
	}
	// Sweet berry bush: advance one age stage, capped at 3.
	if state >= berryBase && state < berryBase+3 {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state+1)
		return true
	}
	// Grass block: scatter short grass + flowers nearby (GrassBlock.performBonemeal).
	if state == worldgen.GrassBlock {
		return h.bonemealGrass(players, dim, x, y, z)
	}
	return false
}

// bonemealGrass sprinkles short grass (and the occasional flower) on the
// grass-block tops around a point, vanilla's radius-spreading placement.
func (h *hub) bonemealGrass(players map[int32]*tracked, dim, x, y, z int) bool {
	w := h.worldFor(dim)
	placed := false
	for i := 0; i < 48; i++ {
		// Vanilla walks outward with a vertical spread; approximate with a
		// small radius and a slight y jitter.
		px := x + h.rng.Intn(7) - 3
		pz := z + h.rng.Intn(7) - 3
		py := y + h.rng.Intn(3) - 1
		if w.At(px, py, pz) != worldgen.Air || w.At(px, py-1, pz) != worldgen.GrassBlock {
			continue
		}
		block := worldgen.ShortGrass
		if h.rng.Intn(10) == 0 { // ~10% a flower, like vanilla
			block = bmFlowerBlocks[h.rng.Intn(len(bmFlowerBlocks))]
		}
		h.setBlockAt(players, dim, blockPos{px, py, pz}, block)
		placed = true
	}
	return placed
}
