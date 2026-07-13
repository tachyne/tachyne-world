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
	itemBoneMeal   = int32(itemByName["bone_meal"])
	bmFlowerBlocks = []uint32{
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
			h.setBlock(players, blockPos{x, y, z}, ns)
			return true
		}
	}
	// Saplings: 45% chance to grow the tree (SaplingBlock.advanceTree).
	for _, r := range saplingRanges {
		if inRange(state, r) {
			if h.rng.Float64() < 0.45 {
				h.growTree(players, x, y, z)
			}
			return true // vanilla consumes the meal either way
		}
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
		h.setBlock(players, blockPos{px, py, pz}, block)
		placed = true
	}
	return placed
}
