package server

import (
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// worldEventElectricSpark is level event 3002 (ELECTRIC_SPARK), the spark
// burst a bolt leaves on each copper block it cleans.
const worldEventElectricSpark = 3002

// clearCopperOnLightningStrike ports LightningBolt.clearCopperOnLightningStrike:
// a bolt that lands on copper (weathering or waxed) resets a weathering block
// to its unaffected stage, then runs 3–5 pairs of short random walks that
// each scrape one stage off the weathering copper they step onto.
func (h *hub) clearCopperOnLightningStrike(players map[int32]*tracked, dim int, pos blockPos) {
	w := h.worldFor(dim)
	st := w.At(pos.x, pos.y, pos.z)
	cr, weathering := copperOf(st)
	_, waxed := unwaxedCopper(st)
	if !weathering && !waxed {
		return
	}
	if weathering {
		first := st
		for cr.stage > 0 { // WeatheringCopper.getFirst
			first = cr.prevBase + (first - cr.lo)
			cr, _ = copperOf(first)
		}
		if first != st {
			h.changeCopper(players, dim, pos, st, first)
		}
	}
	strikes := h.rng.Intn(3) + 3
	for i := 0; i < strikes; i++ {
		steps := h.rng.Intn(8) + 1
		h.randomWalkCleaningCopper(players, dim, pos, steps)
	}
}

// randomWalkCleaningCopper is LightningBolt.randomWalkCleaningCopper: up to
// steps hops from the strike, each onto a copper block next to the last.
func (h *hub) randomWalkCleaningCopper(players map[int32]*tracked, dim int, origin blockPos, steps int) {
	at := origin
	for i := 0; i < steps; i++ {
		next, ok := h.randomStepCleaningCopper(players, dim, at)
		if !ok {
			return
		}
		at = next
	}
}

// randomStepCleaningCopper is LightningBolt.randomStepCleaningCopper: ten
// random cells of the 3×3×3 cube around pos (BlockPos.randomInCube); the
// first weathering copper found loses a stage (unaffected copper keeps its
// state but still takes the spark) and becomes the next step.
func (h *hub) randomStepCleaningCopper(players map[int32]*tracked, dim int, pos blockPos) (blockPos, bool) {
	w := h.worldFor(dim)
	for i := 0; i < 10; i++ {
		c := blockPos{pos.x + h.rng.Intn(3) - 1, pos.y + h.rng.Intn(3) - 1, pos.z + h.rng.Intn(3) - 1}
		st := w.At(c.x, c.y, c.z)
		if _, ok := copperOf(st); !ok {
			continue
		}
		if prev, ok := scrapedCopper(st); ok {
			h.changeCopper(players, dim, c, st, prev)
		}
		h.toNearbyEv(players, dim, float64(c.x), float64(c.z), attachproto.WorldFX{Event: worldEventElectricSpark, X: c.x, Y: c.y, Z: c.z})
		return c, true
	}
	return blockPos{}, false
}

// changeCopper writes a copper stage change the way setBlockAndUpdate
// settles it: a double chest's partner and a door's other half adopt the
// new block (CopperChestBlock/DoorBlock.updateShape).
func (h *hub) changeCopper(players map[int32]*tracked, dim int, pos blockPos, old, next uint32) {
	h.setCopperState(players, dim, pos.x, pos.y, pos.z, old, next)
	if !strings.HasSuffix(copperName(next), "_door") {
		return
	}
	dy, half := 1, "upper"
	if worldgen.GetProperty(copperInfo(next), next, "half") == "upper" {
		dy, half = -1, "lower"
	}
	if other := h.worldFor(dim).At(pos.x, pos.y+dy, pos.z); strings.HasSuffix(copperName(other), "_door") {
		h.setBlockLive(players, dim, pos.x, pos.y+dy, pos.z, worldgen.SetProperty(copperInfo(next), next, "half", half))
	}
}
