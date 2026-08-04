package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The sniffer egg.
//
// Nothing hatched it: an egg dug out of a suspicious-sand brush sat on the
// ground for ever, which made the whole archaeology-to-sniffer chain a dead
// end. It cracks twice and then opens, and moss underneath halves the wait —
// which is the one piece of husbandry the egg actually asks of you.

const (
	snifferHatchTicks     = 24000 // a full day, split across the three stages
	snifferHatchBoosted   = 12000 // …halved by moss beneath it
	snifferHatchJitter    = 300   // vanilla's random spread, so a clutch is staggered
	snifferMaxHatch       = 2     // the state at which the next tick opens it
	snifferEggHatchStages = 3
)

var (
	snifferEggLo, snifferEggHi, snifferEggOK = worldgen.BlockRangeOK("sniffer_egg")
	mossBlockState                           = worldgen.BlockBase("moss_block")
	paleMossBlockState                       = worldgen.BlockBase("pale_moss_block")
)

func isSnifferEgg(s uint32) bool {
	return snifferEggOK && s >= snifferEggLo && s <= snifferEggHi
}

// snifferHatchBoost is the SNIFFER_EGG_HATCH_BOOST tag: moss under the egg.
func (h *hub) snifferHatchBoost(dim, x, y, z int) bool {
	below := h.worldFor(dim).At(x, y-1, z)
	return below == mossBlockState || below == paleMossBlockState
}

// scheduleSnifferEgg books the next crack. Vanilla schedules a third of the
// hatch time plus a jitter, and re-schedules from onPlace each time the state
// changes — so each of the three stages waits its own third.
func (h *hub) scheduleSnifferEgg(dim, x, y, z int) {
	total := snifferHatchTicks
	if h.snifferHatchBoost(dim, x, y, z) {
		total = snifferHatchBoosted
	}
	delay := uint64(total/snifferEggHatchStages + h.rng.Intn(snifferHatchJitter))
	if h.snifferEggs == nil {
		h.snifferEggs = map[simPos]uint64{}
	}
	h.snifferEggs[simPos{dim: dim, blockPos: blockPos{x, y, z}}] = h.tick.Load() + delay
	h.scheduleIn(dim, blockPos{x, y, z}, delay)
}

// tickSnifferEgg cracks the egg, and opens it on the last stage.
//
// processUpdate fires for ANY neighbour change, not only for the tick we
// booked, so the egg keeps its own due time: without that, walking past and
// placing a torch would hatch it on the spot.
func (h *hub) tickSnifferEgg(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isSnifferEgg(state) {
		return false
	}
	key := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	now := h.tick.Load()
	due, known := h.snifferEggs[key]
	if !known {
		// First time we have seen this egg — start its clock. Lazy rather than
		// on-place so eggs already sitting in a loaded world also hatch.
		h.scheduleSnifferEgg(dim, x, y, z)
		return true
	}
	if now < due {
		return true // its own tick has not come round yet
	}
	delete(h.snifferEggs, key)
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	if hatch := worldgen.GetProperty(info, state, "hatch"); hatch != "2" {
		next := "1"
		if hatch == "1" {
			next = "2"
		}
		h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.SetProperty(info, state, "hatch", next))
		h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.crack", sndBlock, cx, cy, cz, 0.7, 1)
		h.scheduleSnifferEgg(dim, x, y, z)
		return true
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	h.playSoundDim(players, dim, "minecraft:block.sniffer_egg.hatch", sndBlock, cx, cy, cz, 0.7, 1)
	if m := h.spawnSpecies(players, entitySniffer, dim, cx, float64(y), cz); m != nil {
		m.baby, m.growLeft = true, growUpTicks
		h.toNearbyEv(players, dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	}
	return true
}
