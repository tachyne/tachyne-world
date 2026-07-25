package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Mushroom spread, ported from MushroomBlock.randomTick: one tick in 25, and
// only while fewer than five of the same mushroom sit in the surrounding
// 9x3x9 box — that population cap is what stops a cave floor turning solid.
//
// Placement is a short random walk rather than a single offset: vanilla rolls
// a candidate, steps to it if it is a legal spot, re-rolls, four times over,
// then plants at wherever the last roll landed if that too is legal. The walk
// is what lets a mushroom creep away from its parent instead of only ever
// filling the ring around it.

const (
	mushroomSpreadChance = 25 // nextInt(25) == 0
	mushroomCrowdLimit   = 5  // same-type mushrooms allowed in the 9x3x9 box
	mushroomLightMax     = 13 // rawBrightness must be BELOW this to spread
)

var (
	mushroomStates = func() []uint32 {
		var out []uint32
		for _, n := range []string{"brown_mushroom", "red_mushroom"} {
			out = append(out, worldgen.BlockBase(n))
		}
		return out
	}()

	// Blocks that let a mushroom ignore the light rule entirely
	// (#mushroom_grow_block: mycelium, podzol, and both nyliums).
	mushroomAnyLightGround = func() map[uint32]bool {
		m := map[uint32]bool{}
		for _, n := range []string{"mycelium", "podzol", "crimson_nylium", "warped_nylium"} {
			lo, hi := worldgen.BlockRange(n)
			for st := lo; st <= hi; st++ {
				m[st] = true
			}
		}
		return m
	}()
)

func isMushroom(state uint32) bool {
	for _, s := range mushroomStates {
		if s == state {
			return true
		}
	}
	return false
}

// mushroomCanSurvive is MushroomBlock.canSurvive: a special ground block allows
// any light, otherwise the spot must be dim AND sit on something solid.
func (h *hub) mushroomCanSurvive(dim, x, y, z int) bool {
	below := h.worldFor(dim).At(x, y-1, z)
	if mushroomAnyLightGround[below] {
		return true
	}
	if h.plantBrightness(dim, x, y, z, 0) >= mushroomLightMax {
		return false
	}
	return worldgen.IsSolidFull(below)
}

// tickMushroom runs one MushroomBlock.randomTick.
func (h *hub) tickMushroom(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isMushroom(state) {
		return false
	}
	if h.rng.Intn(mushroomSpreadChance) != 0 {
		return true
	}
	// Crowding check over the 9x3x9 box around this mushroom.
	seen := 0
	for dx := -4; dx <= 4; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -4; dz <= 4; dz++ {
				if h.worldFor(dim).At(x+dx, y+dy, z+dz) == state {
					if seen++; seen >= mushroomCrowdLimit {
						return true // too many neighbours already
					}
				}
			}
		}
	}

	roll := func(px, py, pz int) (int, int, int) {
		return px + h.rng.Intn(3) - 1,
			py + h.rng.Intn(2) - h.rng.Intn(2),
			pz + h.rng.Intn(3) - 1
	}
	cx, cy, cz := x, y, z
	tx, ty, tz := roll(cx, cy, cz)
	for i := 0; i < 4; i++ {
		if h.worldFor(dim).At(tx, ty, tz) == worldgen.Air && h.mushroomCanSurvive(dim, tx, ty, tz) {
			cx, cy, cz = tx, ty, tz
		}
		tx, ty, tz = roll(cx, cy, cz)
	}
	if h.worldFor(dim).At(tx, ty, tz) == worldgen.Air && h.mushroomCanSurvive(dim, tx, ty, tz) {
		h.setBlockAt(players, dim, blockPos{tx, ty, tz}, state)
	}
	return true
}
