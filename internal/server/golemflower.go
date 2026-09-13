package server

import "math"

// The iron golem's poppy (OfferFlowerGoal + IronGolem.offerFlower): one
// tick in eight thousand, a golem with a villager within six blocks holds
// out a poppy for four hundred ticks, standing still and facing them, and
// puts it away again; and its punches show the arm swing.

const (
	golemFlowerOdds       = 8000 // OfferFlowerGoal: nextInt(8000) == 0
	golemFlowerTicks      = 400
	golemFlowerRange      = 6.0
	entityStatusFlowerOn  = 11 // IronGolem.offerFlower(true)
	entityStatusFlowerOff = 34
)

// golemOfferTick runs each mob update. Returns whether it holds the golem.
func (h *hub) golemOfferTick(players map[int32]*tracked, m *mob) bool {
	if m.golemFlower > 0 {
		m.golemFlower -= mobMoveInterval
		if m.golemFlower <= 0 {
			m.golemFlower = 0
			h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusFlowerOff))
			return false
		}
		if v := h.nearestVillager(m, golemFlowerRange); v != nil {
			m.yaw = float32(math.Atan2(-(v.x-m.x), v.z-m.z) * 180 / math.Pi)
		}
		m.vx, m.vz = 0, 0
		return true
	}
	if m.hasTarget || h.rng.Intn(golemFlowerOdds/mobMoveInterval) != 0 {
		return false
	}
	if h.nearestVillager(m, golemFlowerRange) == nil {
		return false
	}
	m.golemFlower = golemFlowerTicks
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusFlowerOn))
	m.vx, m.vz = 0, 0
	return true
}
