package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// GrowingPlantHeadBlock: kelp, weeping vines, twisting vines and cave vines all
// share one growth rule, so it is ported once here and parameterised rather
// than written out four times.
//
// The head advances while age < 25, rolling its own per-species probability. It
// grows one cell along its growth direction, and the cell it came from becomes
// the species' BODY block — vanilla does that conversion through updateShape
// when a head sees another head or body ahead of it; doing it at the moment of
// growth is equivalent and needs no shape machinery.

const growingPlantMaxAge = 25

type growingPlant struct {
	headLo, headHi uint32 // head states, age 0..25
	body           uint32 // the body (…_plant) block left behind
	dy             int    // growth direction: +1 up, -1 down
	prob           float64
	intoWater      bool    // kelp grows through water; the vines grow through air
	berryChance    float64 // cave vines only: chance a grown segment carries berries
	berryStride    uint32  // states per age when a `berries` property doubles them
}

var growingPlants = func() []growingPlant {
	mk := func(head, body string, dy int, prob float64, intoWater bool, berry float64) growingPlant {
		lo, hi := worldgen.BlockRange(head)
		stride := uint32(1)
		if (hi-lo+1)/(growingPlantMaxAge+1) == 2 {
			stride = 2 // an extra boolean property doubles the states per age
		}
		return growingPlant{
			headLo: lo, headHi: hi, body: worldgen.BlockBase(body),
			dy: dy, prob: prob, intoWater: intoWater,
			berryChance: berry, berryStride: stride,
		}
	}
	return []growingPlant{
		mk("kelp", "kelp_plant", +1, 0.14, true, 0),
		mk("twisting_vines", "twisting_vines_plant", +1, 0.1, false, 0),
		mk("weeping_vines", "weeping_vines_plant", -1, 0.1, false, 0),
		mk("cave_vines", "cave_vines_plant", -1, 0.1, false, 0.11),
	}
}()

// age reads a head state's age, accounting for the extra property cave vines
// carry (their states run age0-berries, age0-noberries, age1-berries, …).
func (g growingPlant) age(state uint32) int {
	return int((state - g.headLo) / g.berryStride)
}

// headAt builds the head state for an age, optionally with berries.
func (g growingPlant) headAt(age int, berries bool) uint32 {
	s := g.headLo + uint32(age)*g.berryStride
	if g.berryStride == 2 && berries {
		return s // berries=true is the FIRST state of the pair
	}
	if g.berryStride == 2 {
		return s + 1
	}
	return s
}

// tickGrowingPlant runs one GrowingPlantHeadBlock.randomTick.
func (h *hub) tickGrowingPlant(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	for _, g := range growingPlants {
		if state < g.headLo || state > g.headHi {
			continue
		}
		age := g.age(state)
		if age >= growingPlantMaxAge || h.rng.Float64() >= g.prob {
			return true
		}
		ny := y + g.dy
		into := h.worldFor(dim).At(x, ny, z)
		ok := into == worldgen.Air
		if g.intoWater {
			ok = worldgen.IsWater(into)
		}
		if !ok {
			return true
		}
		berries := g.berryChance > 0 && h.rng.Float64() < g.berryChance
		h.setBlockAt(players, dim, blockPos{x, ny, z}, g.headAt(age+1, berries))
		// The cell we grew out of is no longer the tip.
		h.setBlockAt(players, dim, blockPos{x, y, z}, g.body)
		return true
	}
	return false
}
