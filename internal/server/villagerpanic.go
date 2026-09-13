package server

import "math"

// Villagers flee (VillagerHostilesSensor + the PANIC package): a villager
// with one of the hostiles it fears within that hostile's range — zombies,
// husks, drowned and zombie villagers at eight, vexes at eight,
// vindicators and zoglins at ten, evokers, illusioners and ravagers at
// twelve, pillagers at fifteen — runs from it at one and a half times its
// pace to six blocks off, and likewise from whatever hurt it, for five
// seconds after the blow.

const (
	villagerFleeSpeed = 1.5 // getPanicPackage: f × 1.5
	villagerFleeTo    = 6.0 // SetWalkTargetAwayFrom(…, 6)
	villagerHurtTicks = 100 // HURT_BY memory
)

var villagerFears = map[int]float64{}

func init() {
	for name, r := range map[string]float64{
		"drowned": 8, "evoker": 12, "husk": 8, "illusioner": 12, "pillager": 15, "ravager": 12,
		"vex": 8, "vindicator": 10, "zoglin": 10, "zombie": 8, "zombie_villager": 8,
	} {
		villagerFears[entityID(name)] = r
	}
}

// villagerPanicStep runs each mob update. Returns whether it holds the
// villager.
func (h *hub) villagerPanicStep(players map[int32]*tracked, m *mob) bool {
	if m.sleeping {
		return false
	}
	if m.villagerHurt { // HurtBySensor
		m.villagerHurt = false
		m.villagerHurtLeft = villagerHurtTicks
	}
	// NEAREST_HOSTILE: the nearest feared mob within its own range.
	var threatX, threatZ float64
	found := false
	bestD := math.Inf(1)
	h.grid().nearby(m.dim, m.x, m.z, 15, func(o *mob) {
		r, ok := villagerFears[o.etype]
		if !ok || o.dying > 0 || math.Abs(o.y-m.y) > 4 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d <= r && d < bestD {
			threatX, threatZ, bestD, found = o.x, o.z, d, true
		}
	})
	if !found && m.villagerHurtLeft > 0 {
		m.villagerHurtLeft -= mobMoveInterval
		if t := players[m.lastAttacker]; t != nil && t.dim == m.dim {
			threatX, threatZ, found = t.x, t.z, true
		} else if o := h.mobs[m.lastAttacker]; o != nil && o.dim == m.dim {
			threatX, threatZ, found = o.x, o.z, true
		}
	}
	if !found {
		return false
	}
	if math.Hypot(m.x-threatX, m.z-threatZ) >= villagerFleeTo && bestD == math.Inf(1) {
		return false // far enough from what hurt it
	}
	h.steerAwayFrom(m, threatX, threatZ, villagerFleeSpeed)
	m.rest = 0
	return true
}
