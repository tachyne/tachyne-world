package server

import "github.com/tachyne/tachyne-world/internal/world"

// Raider.RaiderMoveThroughVillageGoal(1.05, 1): a raider of an active raid
// with nobody to fight roams the village from home to home — a random bed
// within forty-eight blocks that it has not been to among its last three —
// walking a little faster than it wanders, and counts a bed visited once
// within a block of it.

const (
	raidVillageSpeed = 1.05
	raidVillageReach = 48
	raidVillageNear  = 1.0
)

// raidVillageStep reports whether it took the raider's move.
func (h *hub) raidVillageStep(m *mob) bool {
	if !h.raiderInActiveRaid(m) || m.hasTarget || m.mount != 0 || m.rider != 0 {
		m.raidPoiSet = false
		return false
	}
	w := h.poiWorld(m.dim)
	if w == nil {
		return false
	}
	if m.raidPoiSet {
		p := m.raidPoi
		d := dist3(float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, m.x, m.y, m.z)
		if d < m.box().w+raidVillageNear {
			if d < raidVillageNear+0.5 { // stop(): within distanceToPoi of its centre
				m.raidVisited = append(m.raidVisited, p)
			}
			m.raidPoiSet = false
			return false
		}
	} else {
		if len(m.raidVisited) > 2 { // updateVisited
			m.raidVisited = m.raidVisited[1:]
		}
		homes := w.POIsNear(floorInt(m.x), floorInt(m.y), floorInt(m.z), raidVillageReach, func(p world.POI) bool {
			if p.Kind != poiKindHome {
				return false
			}
			for _, v := range m.raidVisited {
				if v == (blockPos{p.X, p.Y, p.Z}) {
					return false
				}
			}
			return true
		})
		if len(homes) == 0 {
			return false
		}
		p := homes[h.rng.Intn(len(homes))]
		m.raidPoi, m.raidPoiSet = blockPos{p.X, p.Y, p.Z}, true
	}
	vx, vz := h.pathSteer(m, float64(m.raidPoi.x)+0.5, float64(m.raidPoi.z)+0.5)
	m.vx, m.vz = vx*raidVillageSpeed, vz*raidVillageSpeed
	m.rest = 0
	return true
}
