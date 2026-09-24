package server

import "math"

// The vex's flight (Vex: VexMoveControl, VexChargeAttackGoal,
// VexRandomMoveGoal). A vex has no pathfinding and no gravity, and it passes
// through blocks. It is steered by a wanted point: every tick it accelerates
// towards the point by 0.05 × the speed modifier, air friction takes a tenth
// of its speed, and within its own size of the point it halves its speed and
// stops wanting. With nothing wanted, one in four goal updates it picks
// either a charge (a target more than two blocks off: the point is the
// target's eyes, speed 1.0, the charging pose and cry) or a drift (an empty
// block within 7 × 5 × 7 of where it was summoned, speed 0.25). A charge
// strikes when the vex's box touches the target's, re-aims inside three
// blocks, and ends with the blow.

const (
	vexAccel      = 0.05
	vexChargeSp   = 1.0
	vexDriftSp    = 0.25
	vexAirDrag    = 0.91
	vexVertDrag   = 0.98
	vexGoalOdds   = 4      // reducedTickDelay(7)
	vexStopRadius = 0.5333 // the box's mean size, (0.4 + 0.8 + 0.4) / 3
)

func (h *hub) setVexCharging(players map[int32]*tracked, m *mob, on bool) {
	if m.vexCharging == on {
		return
	}
	m.vexCharging = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(vexFlagsMeta(m)))
}

// vexTarget is the player the vex is after (NearestAttackableTarget, or its
// evoker's target, which is the same player here).
func (h *hub) vexTarget(players map[int32]*tracked, m *mob) *tracked {
	if !m.hostile {
		return nil
	}
	return h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange())
}

// vexFlight runs one mob update (two ticks) of the vex's goals and flight.
func (h *hub) vexFlight(players map[int32]*tracked, m *mob) {
	t := h.vexTarget(players, m)
	if m.vexCharging && (t == nil || !m.vexWant) {
		h.setVexCharging(players, m, false)
	}
	if !m.vexWant && h.rng.Intn(vexGoalOdds) == 0 {
		if t != nil && dist3sq(m.x, m.y, m.z, t.x, t.y, t.z) > 4 {
			m.vexWX, m.vexWY, m.vexWZ, m.vexSpeed, m.vexWant = t.x, t.y+1.62, t.z, vexChargeSp, true
			h.setVexCharging(players, m, true)
			h.playSoundDim(players, m.dim, "minecraft:entity.vex.charge", sndHostile, m.x, m.y, m.z, 1, 1)
		} else if t == nil || h.rng.Intn(vexGoalOdds) == 0 {
			h.vexDrift(m)
		}
	}
	for tick := 0; tick < mobMoveInterval; tick++ {
		if m.vexCharging && t != nil {
			if vexTouches(m, t) {
				m.attackCD = 0
				h.mobMelee(players, m) // doHurtTarget
				h.setVexCharging(players, m, false)
			} else if dist3sq(m.x, m.y, m.z, t.x, t.y, t.z) < 9 {
				m.vexWX, m.vexWY, m.vexWZ = t.x, t.y+1.62, t.z
			}
		}
		if m.vexWant {
			dx, dy, dz := m.vexWX-m.x, m.vexWY-m.y, m.vexWZ-m.z
			if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d < vexStopRadius {
				m.vexWant = false
				m.vexVX, m.vexVY, m.vexVZ = m.vexVX*0.5, m.vexVY*0.5, m.vexVZ*0.5
			} else {
				k := m.vexSpeed * vexAccel / d
				m.vexVX, m.vexVY, m.vexVZ = m.vexVX+dx*k, m.vexVY+dy*k, m.vexVZ+dz*k
			}
		}
		m.x, m.y, m.z = m.x+m.vexVX, m.y+m.vexVY, m.z+m.vexVZ // noPhysics: through blocks
		m.vexVX, m.vexVY, m.vexVZ = m.vexVX*vexAirDrag, m.vexVY*vexVertDrag, m.vexVZ*vexAirDrag
	}
	if t != nil {
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	} else if m.vexVX != 0 || m.vexVZ != 0 {
		m.yaw = float32(math.Atan2(-m.vexVX, m.vexVZ) * 180 / math.Pi)
	}
	m.headYaw = m.yaw
	m.vx, m.vz = 0, 0
}

// vexDrift is VexRandomMoveGoal: three tries for an empty block around the
// vex's bound origin (its own block when it has none).
func (h *hub) vexDrift(m *mob) {
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	o := m.vexOrigin
	if !m.vexHasOrigin {
		o = blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)}
	}
	for i := 0; i < 3; i++ {
		x, y, z := o.x+h.rng.Intn(15)-7, o.y+h.rng.Intn(11)-5, o.z+h.rng.Intn(15)-7
		if w.At(x, y, z) == 0 {
			m.vexWX, m.vexWY, m.vexWZ = float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
			m.vexSpeed, m.vexWant = vexDriftSp, true
			return
		}
	}
}

// vexTouches is the charge's box test: the vex's 0.4 × 0.8 box against a
// player's 0.6 × 1.8.
func vexTouches(m *mob, t *tracked) bool {
	return math.Abs(m.x-t.x) < 0.5 && math.Abs(m.z-t.z) < 0.5 && m.y < t.y+1.8 && m.y+0.8 > t.y
}
