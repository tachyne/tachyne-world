package server

// Fluid current on mobs. Vanilla's Entity.updateFluidHeightAndDoFluidPushing
// runs for every entity standing in a fluid: the flow vectors of the cells its
// box overlaps are averaged, NORMALISED (players are the exception — they keep
// the magnitude), scaled by 0.014 for water and 0.007 for lava, and added to
// the delta movement. It is what carries anything loose downstream.
//
// The engine had it for dropped ITEMS only (itemphys.go), so a river moved the
// things you dropped in it and left every mob standing still.
//
// Three honest limits. The push is horizontal: the mob step is an x/z walk and
// has nowhere to put the vertical component, so a waterfall does not press a
// mob down. It is sampled at the feet cell rather than averaged over the whole
// box, which is the difference between a tall mob straddling two flows and one
// standing in a single cell. And it is WATER only — fluidFlow is written
// against water's levels, and generalising it would mean reworking the live
// item-physics path for a lava current that reaches little beyond a strider.

import "math"

// waterPushPerTick and lavaPushPerTick are vanilla's scales. The mob pass runs
// every mobMoveInterval ticks, so one application covers that many ticks —
// the same reasoning the enderman carry odds use.
const waterPushPerTick = 0.014

// applyFluidPush adds the current at a mob's feet to its shove for this step.
// It rides pushX/pushZ rather than the steering velocity because vanilla adds
// it after the AI has moved and outside the movement-speed clamp: a current is
// meant to be able to carry a mob faster than it walks.
func (h *hub) applyFluidPush(m *mob) {
	if m.dying > 0 || m.statik {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	pos := blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)}
	fx, _, fz, ok := h.fluidFlow(m.dim, pos)
	if !ok {
		return
	}
	d := math.Hypot(fx, fz)
	if d == 0 {
		return
	}
	// Everything that is not a player is normalised first, so a trickle and a
	// torrent push a mob equally hard — vanilla's own oddity.
	step := waterPushPerTick * mobMoveInterval
	m.pushX += fx / d * step
	m.pushZ += fz / d * step
}
