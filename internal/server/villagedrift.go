package server

import "math"

// MoveThroughVillageGoal — the reason a village is a frightening place after
// dark. A zombie (or husk, drowned, zombie villager) with nobody to chase
// does not simply mill about at night: it walks through the village, from
// one of its buildings to the next, which is how a horde ends up at the
// doors by morning.
//
// Vanilla picks an unvisited village POI within ten blocks of a random land
// position; the engine has no POI manager, so it walks to a point near the
// village centre and picks a new one each time it arrives. The visible
// behaviour — zombies converging on the village at night, and only at night
// — is the same.

const (
	villageDriftRadius = 48.0 // how far out a zombie still counts as "at" the village
	villageDriftSpread = 12.0 // …and how far from its centre it aims
	villageDriftArrive = 3.0  // close enough: pick another spot
)

// villageDriftStep steers an idle zombie-family mob through the village it is
// standing in. Reports whether it took the step.
func (h *hub) villageDriftStep(players map[int32]*tracked, m *mob) bool {
	if !zombieKind(m.etype) || m.dim != 0 || m.dying != 0 {
		return false
	}
	if m.hasTarget && !m.drifting {
		return false // something real to chase outranks the walk
	}
	if h.isDayTime() { // onlyAtNight
		m.drifting = false
		return false
	}
	v := h.world.Gen().VillageIn(int(m.x), int(m.z))
	if !v.Exists || math.Hypot(m.x-float64(v.X), m.z-float64(v.Z)) > villageDriftRadius {
		m.drifting = false
		return false
	}
	if m.drifting && math.Hypot(m.x-m.tx, m.z-m.tz) > villageDriftArrive {
		return true // still on the way to the last spot
	}
	// A new spot somewhere in the village, roughly where its buildings are.
	ang := h.rng.Float64() * 2 * math.Pi
	r := h.rng.Float64() * villageDriftSpread
	m.tx = float64(v.X) + math.Cos(ang)*r
	m.tz = float64(v.Z) + math.Sin(ang)*r
	m.hasTarget, m.drifting, m.rest, m.stroll = true, true, 0, strollMax
	return true
}
