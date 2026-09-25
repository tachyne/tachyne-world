package server

import "math"

// The elder guardian's home (ElderGuardian.customServerAiStep: setHomeTo
// its own block, radius 16, the first time it has none) and the
// MoveTowardsRestrictionGoal every guardian carries: a guardian outside its
// home swims back toward it, so an elder stays in its monument rather than
// drifting off after whatever it last looked at. The attack goal outranks
// it; with nothing to attack, the way home outranks the stroll.

const elderHomeRadius = 16

// guardianHomeStep sets an elder's home and walks a guardian back inside
// it. Reports whether it took the movement.
func (h *hub) guardianHomeStep(m *mob) bool {
	if m.etype == entityElderGuardian && m.homeR == 0 {
		m.homePos, m.homeR = blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)}, elderHomeRadius
	}
	if m.homeR == 0 || m.hasTarget || m.beamTarget != 0 {
		return false
	}
	dx := float64(floorInt(m.x) - m.homePos.x)
	dy := float64(floorInt(m.y) - m.homePos.y)
	dz := float64(floorInt(m.z) - m.homePos.z)
	r := float64(m.homeR)
	if dx*dx+dy*dy+dz*dz < r*r { // isWithinHome
		return false
	}
	hx, hy, hz := float64(m.homePos.x)+0.5, float64(m.homePos.y), float64(m.homePos.z)+0.5
	m.vx, m.vz = straightSteer(m, hx, hz, 0.5)
	m.vy = math.Max(-m.moveSpeed(), math.Min(m.moveSpeed(), (hy-m.y)*0.1))
	m.rest = 0
	return true
}
