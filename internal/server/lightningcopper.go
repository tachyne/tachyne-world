package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Lightning cleans copper (LightningBolt.clearCopperOnLightningStrike). A
// bolt that strikes weathering copper — a lightning rod, most often — turns
// that block back to bare copper, then walks three to five random paths of
// one to eight steps out from it, each step taking one neighbouring copper
// block a stage back and throwing the electric-spark level event. A waxed
// block struck keeps its wax and its stage, but still sends the walks out.

const worldEventElectricSpark = 3002 // LevelEvent.PARTICLES_ELECTRIC_SPARK

// copperFirst is WeatheringCopper.getFirst: the unaffected form of a
// weathering copper state, same orientation.
func copperFirst(state uint32) uint32 {
	for {
		prev, ok := scrapedCopper(state)
		if !ok {
			return state
		}
		state = prev
	}
}

// clearCopperOnStrike runs the cleaning at the struck block (overworld).
func (h *hub) clearCopperOnStrike(players map[int32]*tracked, struck blockPos) {
	w := h.worldFor(dimOverworld)
	if w == nil {
		return
	}
	st := w.At(struck.x, struck.y, struck.z)
	_, weathering := copperOf(st)
	_, waxed := unwaxedCopper(st)
	if !weathering && !waxed {
		return
	}
	if weathering {
		if first := copperFirst(st); first != st {
			h.setCopperState(players, dimOverworld, struck.x, struck.y, struck.z, st, first)
		}
	}
	walks := h.rng.Intn(3) + 3
	for i := 0; i < walks; i++ {
		steps := h.rng.Intn(8) + 1
		at := struck
		for s := 0; s < steps; s++ {
			next, ok := h.copperCleaningStep(players, at)
			if !ok {
				break
			}
			at = next
		}
	}
}

// copperCleaningStep is randomStepCleaningCopper: up to ten random cells in
// the cube one block round pos; the first weathering copper found goes back
// a stage (if it has one to lose) and becomes the next step.
func (h *hub) copperCleaningStep(players map[int32]*tracked, pos blockPos) (blockPos, bool) {
	w := h.worldFor(dimOverworld)
	for i := 0; i < 10; i++ {
		c := blockPos{pos.x + h.rng.Intn(3) - 1, pos.y + h.rng.Intn(3) - 1, pos.z + h.rng.Intn(3) - 1}
		st := w.At(c.x, c.y, c.z)
		if _, ok := copperOf(st); !ok {
			continue
		}
		if prev, ok := scrapedCopper(st); ok {
			h.setCopperState(players, dimOverworld, c.x, c.y, c.z, st, prev)
		}
		h.toNearbyEv(players, dimOverworld, float64(c.x), float64(c.z),
			attachproto.WorldFX{Event: worldEventElectricSpark, X: c.x, Y: c.y, Z: c.z, Data: -1})
		return c, true
	}
	return pos, false
}
