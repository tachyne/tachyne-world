package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Two small random-tick behaviours that were missing entirely.
//
// Redstone ore lights when disturbed and goes dark again on a later random
// tick (RedStoneOreBlock.randomTick). Nylium reverts to netherrack when
// something opaque covers it (NyliumBlock.randomTick via canBeNylium), which
// only became reachable once the simulation started ticking the Nether.

var (
	// The `lit` property is the FIRST state of each ore, so the exported
	// constants — which point at the default, unlit form — are base+1.
	redstoneOreLit          = worldgen.RedstoneOre - 1
	redstoneOreDark         = worldgen.RedstoneOre
	deepslateRedstoneOreLit = worldgen.DeepslateRedstoneOre - 1
	deepslateRedstoneDark   = worldgen.DeepslateRedstoneOre

	netherrackBlock = worldgen.BlockBase("netherrack")
	nyliumStates    = func() []uint32 {
		var out []uint32
		for _, n := range []string{"crimson_nylium", "warped_nylium"} {
			lo, hi := worldgen.BlockRange(n)
			for st := lo; st <= hi; st++ {
				out = append(out, st)
			}
		}
		return out
	}()
)

// tickRedstoneOre darkens a lit redstone ore.
func (h *hub) tickRedstoneOre(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	switch state {
	case redstoneOreLit:
		h.setBlockAt(players, dim, blockPos{x, y, z}, redstoneOreDark)
	case deepslateRedstoneOreLit:
		h.setBlockAt(players, dim, blockPos{x, y, z}, deepslateRedstoneDark)
	case redstoneOreDark, deepslateRedstoneDark:
		// Already dark: handled, nothing to do.
	default:
		return false
	}
	return true
}

// tickNylium reverts covered nylium to netherrack.
func (h *hub) tickNylium(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	found := false
	for _, st := range nyliumStates {
		if st == state {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if h.opaqueAbove(dim, x, y, z) {
		h.setBlockAt(players, dim, blockPos{x, y, z}, netherrackBlock)
	}
	return true
}
