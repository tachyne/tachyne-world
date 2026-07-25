package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Bamboo, ported from BambooStalkBlock.randomTick + growBamboo.
//
// A stalk only grows from its TOP segment (stage 0), one tick in three, into
// clear air lit to 9 or better, and stops at 16 tall. The fiddly part is the
// leaves: a new segment sprouts SMALL leaves, and when it sprouts above a
// segment that already has leaves it takes LARGE ones and pushes the ones
// below down a rank — the segment beneath drops to SMALL and the one under
// that to NONE. That shuffle is what gives a mature stalk its bare trunk and
// leafy crown; without it every segment would keep the leaves it was born with.

const (
	bambooMaxHeight  = 16
	bambooGrowChance = 3 // nextInt(3) == 0
	bambooLightMin   = 9

	bambooLeavesNone  = 0
	bambooLeavesSmall = 1
	bambooLeavesLarge = 2
)

var (
	bambooBase    = worldgen.BlockBase("bamboo") // age × leaves × stage, stage fastest
	bambooSapling = worldgen.BlockBase("bamboo_sapling")
)

// bambooState composes a stalk state; leaves is the none/small/large index.
func bambooState(age, leaves, stage int) uint32 {
	return bambooBase + uint32(age*6+leaves*2+stage)
}

func isBamboo(s uint32) bool { return s >= bambooBase && s < bambooBase+12 }

func bambooAge(s uint32) int    { return int(s-bambooBase) / 6 }
func bambooLeaves(s uint32) int { return int(s-bambooBase) % 6 / 2 }
func bambooStage(s uint32) int  { return int(s-bambooBase) % 2 }

// bambooHeightBelow counts the stalk segments directly beneath a position,
// stopping at the 16-segment cap (getHeightBelowUpToMax).
func (h *hub) bambooHeightBelow(dim, x, y, z int) int {
	n := 0
	for n < bambooMaxHeight && isBamboo(h.worldFor(dim).At(x, y-1-n, z)) {
		n++
	}
	return n
}

// tickBamboo runs one BambooStalkBlock.randomTick.
func (h *hub) tickBamboo(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isBamboo(state) {
		return false
	}
	if bambooStage(state) != 0 { // not the growing tip
		return true
	}
	if h.rng.Intn(bambooGrowChance) != 0 {
		return true
	}
	if h.worldFor(dim).At(x, y+1, z) != worldgen.Air {
		return true
	}
	if h.plantBrightness(dim, x, y+1, z, 0) < bambooLightMin {
		return true
	}
	height := h.bambooHeightBelow(dim, x, y, z) + 1
	if height >= bambooMaxHeight {
		return true
	}

	below := h.worldFor(dim).At(x, y-1, z)
	twoBelow := h.worldFor(dim).At(x, y-2, z)

	leaves := bambooLeavesNone
	if !isBamboo(below) || bambooLeaves(below) == bambooLeavesNone {
		leaves = bambooLeavesSmall
	} else {
		leaves = bambooLeavesLarge
		if isBamboo(twoBelow) {
			// Push the crown up: the segment below drops a rank, the one under
			// it goes bare.
			h.setBlockAt(players, dim, blockPos{x, y - 1, z},
				bambooState(bambooAge(below), bambooLeavesSmall, bambooStage(below)))
			h.setBlockAt(players, dim, blockPos{x, y - 2, z},
				bambooState(bambooAge(twoBelow), bambooLeavesNone, bambooStage(twoBelow)))
		}
	}

	age := 0
	if bambooAge(state) == 1 || isBamboo(twoBelow) {
		age = 1
	}
	// Past 11 tall a quarter of segments cap the stalk, and 15 always does.
	stage := 0
	if (height >= 11 && h.rng.Float64() < 0.25) || height == 15 {
		stage = 1
	}
	h.setBlockAt(players, dim, blockPos{x, y + 1, z}, bambooState(age, leaves, stage))
	return true
}

// tickBambooSapling grows the sapling into its first stalk segment.
func (h *hub) tickBambooSapling(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if state != bambooSapling {
		return false
	}
	if h.rng.Intn(bambooGrowChance) != 0 {
		return true
	}
	if h.worldFor(dim).At(x, y+1, z) != worldgen.Air ||
		h.plantBrightness(dim, x, y+1, z, 0) < bambooLightMin {
		return true
	}
	h.setBlockAt(players, dim, blockPos{x, y + 1, z},
		bambooState(0, bambooLeavesSmall, 0))
	return true
}
