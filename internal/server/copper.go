package server

import (
	"log"
	"sort"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Copper weathering, reimplemented from the vanilla WeatheringCopper /
// ChangeOverTimeBlock oracle: an un-waxed copper block randomly oxidizes one
// stage (unaffected→exposed→weathered→oxidized). Per random tick there is a
// 0.05689 gate, then a neighbourhood scan: any LESS-oxidized copper within
// Manhattan distance 4 halts oxidation, while more-oxidized neighbours speed
// it up (f = (more+1)/(more+same+1), applied squared).

// copperFamilies are the nine oxidizing block lines, each listed
// unaffected→exposed→weathered→oxidized. The four stages of a line share an
// identical property layout, so a stage change is a base swap that preserves
// the in-block state offset (vanilla withPropertiesOf). Waxed variants are
// absent — they never oxidize and do not count in the scan.
var copperFamilies = [][4]string{
	{"copper_block", "exposed_copper", "weathered_copper", "oxidized_copper"},
	{"cut_copper", "exposed_cut_copper", "weathered_cut_copper", "oxidized_cut_copper"},
	{"chiseled_copper", "exposed_chiseled_copper", "weathered_chiseled_copper", "oxidized_chiseled_copper"},
	{"cut_copper_slab", "exposed_cut_copper_slab", "weathered_cut_copper_slab", "oxidized_cut_copper_slab"},
	{"cut_copper_stairs", "exposed_cut_copper_stairs", "weathered_cut_copper_stairs", "oxidized_cut_copper_stairs"},
	{"copper_door", "exposed_copper_door", "weathered_copper_door", "oxidized_copper_door"},
	{"copper_trapdoor", "exposed_copper_trapdoor", "weathered_copper_trapdoor", "oxidized_copper_trapdoor"},
	{"copper_grate", "exposed_copper_grate", "weathered_copper_grate", "oxidized_copper_grate"},
	{"copper_bulb", "exposed_copper_bulb", "weathered_copper_bulb", "oxidized_copper_bulb"},
}

type copperRange struct {
	lo, hi   uint32 // this block's [min,max] state range
	stage    int    // 0 unaffected .. 3 oxidized
	nextBase uint32 // base of the next-stage block (unused at stage 3)
}

var (
	copperRanges       []copperRange // sorted by lo, for binary search
	copperLo, copperHi uint32        // coarse bounds for a cheap reject
)

// safeRange looks up a block's state range without letting an unknown name
// (a typo or a version that lacks it) panic package init.
func safeRange(name string) (lo, hi uint32, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	lo, hi = worldgen.BlockRange(name)
	return lo, hi, true
}

func init() {
	for _, fam := range copperFamilies {
		var los [4]uint32
		valid := true
		for i, name := range fam {
			lo, _, ok := safeRange(name)
			if !ok {
				log.Printf("copper: unknown block %q — family skipped", name)
				valid = false
				break
			}
			los[i] = lo
		}
		if !valid {
			continue
		}
		for stage, name := range fam {
			lo, hi, _ := safeRange(name)
			next := uint32(0)
			if stage < 3 {
				next = los[stage+1]
			}
			copperRanges = append(copperRanges, copperRange{lo: lo, hi: hi, stage: stage, nextBase: next})
			if copperLo == 0 || lo < copperLo {
				copperLo = lo
			}
			if hi > copperHi {
				copperHi = hi
			}
		}
	}
	sort.Slice(copperRanges, func(i, j int) bool { return copperRanges[i].lo < copperRanges[j].lo })
}

// copperOf classifies a block state as copper, returning its range record.
func copperOf(state uint32) (copperRange, bool) {
	if state < copperLo || state > copperHi {
		return copperRange{}, false
	}
	i := sort.Search(len(copperRanges), func(i int) bool { return copperRanges[i].hi >= state })
	if i < len(copperRanges) && state >= copperRanges[i].lo && state <= copperRanges[i].hi {
		return copperRanges[i], true
	}
	return copperRange{}, false
}

// tickCopper runs one ChangeOverTimeBlock.changeOverTime on a copper block.
// Returns whether the block was copper (handled).
func (h *hub) tickCopper(players map[int32]*tracked, x, y, z int, state uint32) bool {
	cr, ok := copperOf(state)
	if !ok {
		return false
	}
	if cr.stage >= 3 || h.rng.Float64() >= 0.05688889 { // fully oxidized, or gate closed
		return true
	}
	// Neighbourhood scan (Manhattan ≤ 4): a less-oxidized copper block halts
	// oxidation; tally more-oxidized vs same-stage.
	more, same := 0, 0
	for dx := -4; dx <= 4; dx++ {
		for dy := -4; dy <= 4; dy++ {
			ad := abs(dx) + abs(dy)
			for dz := -4; dz <= 4; dz++ {
				if ad+abs(dz) > 4 || (dx == 0 && dy == 0 && dz == 0) {
					continue
				}
				ncr, nok := copperOf(h.world.At(x+dx, y+dy, z+dz))
				if !nok {
					continue
				}
				switch {
				case ncr.stage < cr.stage:
					return true // a fresher neighbour blocks all oxidation
				case ncr.stage > cr.stage:
					more++
				default:
					same++
				}
			}
		}
	}
	chance := float64(more+1) / float64(more+same+1)
	chance *= chance
	if cr.stage == 0 {
		chance *= 0.75 // getChanceModifier: slower from unaffected
	}
	if h.rng.Float64() < chance {
		h.setBlock(players, blockPos{x, y, z}, cr.nextBase+(state-cr.lo))
	}
	return true
}
