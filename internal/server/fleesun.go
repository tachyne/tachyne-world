package server

import (
	"math"
)

// Skeletons flee the sun (FleeSunGoal, skeleton / stray / bogged): a
// skeleton burning in daylight with nobody to shoot at and no helmet on
// looks for cover — ten tries at a spot within ten blocks and three up or
// down that the sky cannot see and that is dim — and walks there at its
// normal pace. Wither skeletons never burn and zombies have no such goal.

const (
	fleeSunTries = 10  // getHidePos attempts
	fleeSunReach = 1.0 // navigation "done" distance
	fleeSunLight = 8   // a dim spot: getWalkTargetValue < 0 ⇔ brightness < 0.5
)

// skeletonKind reports the AbstractSkeleton family that carries FleeSunGoal.
func skeletonKind(etype int) bool {
	return etype == entitySkeleton || etype == entityStray || etype == entityBogged
}

// fleeSunStep is the goal's tick: returns whether it is steering the
// skeleton toward cover.
func (h *hub) fleeSunStep(players map[int32]*tracked, m *mob) bool {
	if m.hasTarget || !m.burning || m.dim != 0 || !h.isDayTime() || m.gear[0].item != 0 {
		m.hidePos = blockPos{}
		return false
	}
	if m.hidePos == (blockPos{}) {
		pos, ok := h.sunHidePos(m)
		if !ok {
			return false
		}
		m.hidePos = pos
	}
	tx, tz := float64(m.hidePos.x)+0.5, float64(m.hidePos.z)+0.5
	dx, dz := tx-m.x, tz-m.z
	d := math.Hypot(dx, dz)
	if d <= fleeSunReach {
		m.hidePos = blockPos{} // navigation done
		return false
	}
	sp := m.moveSpeed()
	m.vx, m.vz = dx/d*sp, dz/d*sp
	m.rest = 0
	return true
}

// sunHidePos is getHidePos: a random spot the sky cannot see whose walk
// value is negative — for a monster, one dimmer than half brightness.
func (h *hub) sunHidePos(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for i := 0; i < fleeSunTries; i++ {
		x, y, z := bx+h.rng.Intn(20)-10, by+h.rng.Intn(6)-3, bz+h.rng.Intn(20)-10
		if h.skyExposedAt(x, y, z) || !w.Walkable(x, z) {
			continue
		}
		feet := w.MobFeetFrom(x, z, y)
		if feet != y {
			continue // not standable at that height
		}
		sky, blk := w.LightAt(x, y, z)
		if int(sky) >= fleeSunLight || int(blk) >= fleeSunLight {
			continue
		}
		return blockPos{x, y, z}, true
	}
	return blockPos{}, false
}
