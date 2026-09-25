package server

import "math"

// MoveThroughVillageGoal — the reason a village is a frightening place after
// dark. A zombie (or husk, drowned, zombie villager) with nobody to chase
// does not simply mill about at night: it walks through the village, from
// one of its buildings to the next, which is how a horde ends up at the
// doors by morning.
//
// Vanilla looks for an occupied village point of interest (a bed, a
// workstation or a bell some villager has claimed) near a random land spot
// fifteen blocks about it, takes the one nearest the zombie that it has not
// visited among its last sixteen, walks to within four blocks of it and
// remembers it. A village is where villagers live — one players built
// counts, a generated one standing empty does not.

const (
	villageDriftReach  = 25  // LandRandomPos(15, 7) plus the POI search's 10
	villageDriftArrive = 4.0 // distanceToPoi
	villageDriftMemory = 16  // the visited list keeps its last sixteen
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
	if m.drifting {
		reach := villageDriftArrive + m.box().w
		if math.Hypot(m.x-m.tx, m.z-m.tz) > reach {
			return true // still on the way to the last point
		}
		m.driftVisited = append(m.driftVisited, m.driftPoi) // stop(): close enough, it has been there
		if len(m.driftVisited) > villageDriftMemory {
			m.driftVisited = m.driftVisited[1:]
		}
		m.drifting, m.hasTarget = false, false
	}
	now := h.tick.Load()
	if now < m.driftNext {
		return false
	}
	poi, ok := h.villageDriftPoi(m)
	if !ok {
		m.driftNext = now + 20 // nothing near: look again in a second, not every update
		return false
	}
	m.driftPoi = poi
	m.tx, m.tz = float64(poi.x)+0.5, float64(poi.z)+0.5
	m.hasTarget, m.drifting, m.rest, m.stroll = true, true, 0, strollMax
	return true
}

// villageDriftPoi is the goal's pick: the nearest occupied village point
// within reach that the zombie has not visited lately.
func (h *hub) villageDriftPoi(m *mob) (blockPos, bool) {
	w := h.poiWorld(m.dim)
	if w == nil {
		return blockPos{}, false
	}
	occupied := map[blockPos]bool{}
	for _, o := range h.mobs {
		if o.etype != entityVillager || o.dying > 0 || o.dim != m.dim {
			continue
		}
		if dist3(o.x, o.y, o.z, m.x, m.y, m.z) > villageDriftReach+48 {
			continue // too far to hold a point within reach of this zombie
		}
		for _, p := range []blockPos{o.bed, o.work, o.jobPos, o.meet} {
			if p != (blockPos{}) {
				occupied[p] = true
			}
		}
	}
	if len(occupied) == 0 {
		return blockPos{}, false
	}
	visited := func(p blockPos) bool {
		for _, v := range m.driftVisited {
			if v == p {
				return true
			}
		}
		return false
	}
	near := w.POIsNear(floorInt(m.x), floorInt(m.y), floorInt(m.z), villageDriftReach, nil)
	for _, p := range near { // closest first
		pos := blockPos{p.X, p.Y, p.Z}
		if occupied[pos] && !visited(pos) {
			return pos, true
		}
	}
	return blockPos{}, false
}
