package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Mangrove propagules, ported from MangrovePropaguleBlock.randomTick.
//
// A propagule leads two lives. HANGING under mangrove leaves it simply ripens,
// gaining an age every random tick with no gate at all. PLANTED on the ground
// it behaves like a sapling — one tick in seven, advance the stage and then
// grow the tree.
//
// Note the planted branch has NO light requirement. MangrovePropaguleBlock
// overrides randomTick outright rather than extending SaplingBlock's, so the
// `brightness >= 9` gate every other sapling obeys does not apply here.

const (
	propaguleMaxAge = 4
	propaguleGrow   = 7 // planted: nextInt(7) == 0
)

var (
	propaguleBase = func() uint32 { lo, _ := worldgen.BlockRange("mangrove_propagule"); return lo }()
	propaguleHi   = func() uint32 { _, hi := worldgen.BlockRange("mangrove_propagule"); return hi }()

	// Blocks a hanging propagule can hang from (#supports_hanging_mangrove_propagule).
	propaguleSupports = func() map[uint32]bool {
		m := map[uint32]bool{}
		lo, hi := worldgen.BlockRange("mangrove_leaves")
		for st := lo; st <= hi; st++ {
			m[st] = true
		}
		return m
	}()
)

// State layout is age × hanging × stage × waterlogged, waterlogged varying
// fastest, and the `true` value of each boolean comes FIRST.
func propaguleState(age int, hanging bool, stage int, waterlogged bool) uint32 {
	s := propaguleBase + uint32(age)*8 + uint32(stage)*2
	if !hanging {
		s += 4
	}
	if !waterlogged {
		s++
	}
	return s
}

func isPropagule(s uint32) bool { return s >= propaguleBase && s <= propaguleHi }

func propaguleAge(s uint32) int      { return int(s-propaguleBase) / 8 }
func propaguleHanging(s uint32) bool { return (s-propaguleBase)%8/4 == 0 }
func propaguleStage(s uint32) int    { return int(s-propaguleBase) % 4 / 2 }
func propaguleWet(s uint32) bool     { return (s-propaguleBase)%2 == 0 }

// tickPropagule runs one MangrovePropaguleBlock.randomTick.
func (h *hub) tickPropagule(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isPropagule(state) {
		return false
	}
	age, hanging := propaguleAge(state), propaguleHanging(state)

	if hanging {
		// Ripening under the leaves — no probability roll, no light check.
		if !propaguleSupports[h.worldFor(dim).At(x, y+1, z)] {
			return true // nothing to hang from; support handling lives elsewhere
		}
		if age < propaguleMaxAge {
			h.setBlockAt(players, dim, blockPos{x, y, z},
				propaguleState(age+1, true, propaguleStage(state), propaguleWet(state)))
		}
		return true
	}

	// Planted: SaplingBlock.advanceTree, gated only on the 1-in-7 roll.
	if h.rng.Intn(propaguleGrow) != 0 {
		return true
	}
	if propaguleStage(state) == 0 {
		h.setBlockAt(players, dim, blockPos{x, y, z},
			propaguleState(age, false, 1, propaguleWet(state)))
		return true
	}
	// TreeGrower MANGROVE: tall_mangrove at 0.85, mangrove otherwise.
	feature := "mangrove"
	if h.rng.Float64() < 0.85 {
		feature = "tall_mangrove"
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	if !h.placeLiveTree(players, dim, x, y, z, feature) {
		h.setBlockAt(players, dim, blockPos{x, y, z}, state)
	}
	return true
}
