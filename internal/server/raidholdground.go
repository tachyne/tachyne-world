package server

import "math"

// Raider.HoldGroundAttackGoal (the pillager's and the vindicator's): a
// patrolling raider outside any raid that has just found a target, and was
// not hurt into it by a player, does not charge. It stops and stares, and
// only once the target comes within ten blocks does it turn aggressive —
// and with it every raider within eight blocks — and the attack goal takes
// over. A patrol spotted across a field watches you rather than opening
// fire.

const (
	holdGroundRadius = 10.0 // HoldGroundAttackGoal(this, 10.0F)
	holdGroundShout  = 8.0  // getBoundingBox().inflate(8.0, 8.0, 8.0)
)

// holdsGround reports the species that register the goal.
func holdsGround(etype int) bool {
	return etype == entityPillager || etype == entityVindicator
}

// updateHoldGround runs with the mob step, after the target pick: the
// goal's canUse, and its tick deciding whether the stand-off is over.
func (h *hub) updateHoldGround(players map[int32]*tracked, m *mob) {
	m.holdingGround = false
	if !holdsGround(m.etype) || m.raidCenter != (blockPos{}) || !m.patrolling ||
		!m.hasTarget || m.aggressive || m.dying > 0 ||
		(m.hurtByPlayerTil != 0 && h.tick.Load() < m.hurtByPlayerTil) {
		return
	}
	q, ok := h.rangedQuarry(players, m, m.followRange()+deaggroSlack)
	if !ok {
		return
	}
	if dist3(q.x, q.y, q.z, m.x, m.y, m.z) > holdGroundRadius {
		m.holdingGround = true
		return
	}
	// Within ten blocks: aggressive, and the goal stops, rousing the
	// raiders about it on the way out.
	h.setAggressive(players, m, true)
	for _, o := range h.mobs {
		if o == m || o.dim != m.dim || o.dying > 0 || !o.hasTarget || !raiderKind(o.etype) {
			continue
		}
		if math.Abs(o.x-m.x) <= holdGroundShout && math.Abs(o.y-m.y) <= holdGroundShout &&
			math.Abs(o.z-m.z) <= holdGroundShout {
			h.setAggressive(players, o, true)
		}
	}
}

// holdGroundStep is the goal's movement: the navigation stopped, the head
// on the target.
func (h *hub) holdGroundStep(m *mob) bool {
	m.vx, m.vz = 0, 0
	yaw := float32(math.Atan2(-(m.tx-m.x), m.tz-m.z) * 180 / math.Pi)
	m.yaw, m.headYaw = yaw, yaw
	return true
}

// raiderKind is the Raider class: the illagers, the ravager and the witch.
func raiderKind(etype int) bool {
	return isIllager(etype) || etype == entityRavager || etype == entityWitch
}
