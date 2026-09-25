package server

import (
	"math"
)

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

// vexTarget is what the vex is after. VexCopyOwnerTargetGoal (priority 2)
// takes its evoker's target, whatever it is and seen or not — the villager
// or golem the evoker is fighting as much as a player; failing that, the
// player goal (priority 3) takes the nearest player it can see.
func (h *hub) vexTarget(players map[int32]*tracked, m *mob) (quarry, bool) {
	if !m.hostile {
		return quarry{}, false
	}
	if o := h.mobs[m.vexOwner]; o != nil && o.dying == 0 && o.hasTarget {
		if t := players[o.targetEID]; t != nil && isSurvival(t.gamemode) && !t.dead && t.dim == m.dim && o.preyTarget == 0 {
			return quarry{t: t, x: t.x, y: t.y, z: t.z}, true
		}
		if p := h.mobs[o.preyTarget]; p != nil && p.dying == 0 && p.dim == m.dim {
			return quarry{o: p, x: p.x, y: p.y, z: p.z}, true
		}
	}
	// NearestAttackableTargetGoal(Player, mustSee): acquireTarget ran it
	// (huntTarget) earlier in this update.
	if t := players[m.targetEID]; t != nil && m.hasTarget && t.dim == m.dim && !t.dead {
		return quarry{t: t, x: t.x, y: t.y, z: t.z}, true
	}
	return quarry{}, false
}

// vexQuarryEyes is where a charge aims: the target's eyes.
func vexQuarryEyes(q quarry) float64 {
	if q.t != nil {
		return q.y + playerEyeStand
	}
	return q.y + mobEyeHeight(q.o)
}

// vexFlight runs one mob update (two ticks) of the vex's goals and flight.
func (h *hub) vexFlight(players map[int32]*tracked, m *mob) {
	q, has := h.vexTarget(players, m)
	if m.vexCharging && (!has || !m.vexWant) {
		h.setVexCharging(players, m, false)
	}
	if !m.vexWant && h.rng.Intn(vexGoalOdds) == 0 {
		if has && dist3sq(m.x, m.y, m.z, q.x, q.y, q.z) > 4 {
			m.vexWX, m.vexWY, m.vexWZ, m.vexSpeed, m.vexWant = q.x, vexQuarryEyes(q), q.z, vexChargeSp, true
			h.setVexCharging(players, m, true)
			h.playSoundDim(players, m.dim, "minecraft:entity.vex.charge", sndHostile, m.x, m.y, m.z, 1, 1)
		} else if !has || h.rng.Intn(vexGoalOdds) == 0 {
			h.vexDrift(m)
		}
	}
	for tick := 0; tick < mobMoveInterval; tick++ {
		if m.vexCharging && has {
			if vexTouches(m, q) {
				if q.t != nil {
					m.attackCD = 0
					h.mobMelee(players, m) // doHurtTarget
				} else {
					h.vexStrikeMob(players, m, q.o)
				}
				h.setVexCharging(players, m, false)
			} else if dist3sq(m.x, m.y, m.z, q.x, q.y, q.z) < 9 {
				m.vexWX, m.vexWY, m.vexWZ = q.x, vexQuarryEyes(q), q.z
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
	if has {
		m.yaw = float32(math.Atan2(-(q.x-m.x), q.z-m.z) * 180 / math.Pi)
	} else if m.vexVX != 0 || m.vexVZ != 0 {
		m.yaw = float32(math.Atan2(-m.vexVX, m.vexVZ) * 180 / math.Pi)
	}
	m.headYaw = m.yaw
	m.vx, m.vz = 0, 0
}

// vexStrikeMob is the charge's doHurtTarget on a mob: its ATTACK_DAMAGE as a
// mob attack, the usual knockback, and the hurt creature's panic.
func (h *hub) vexStrikeMob(players map[int32]*tracked, m, v *mob) {
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	v.hurtKind(float64(hostileMelee(m)+mobHeldBonus(m)), dtMobAttack)
	v.lastAttacker = m.eid
	h.mobDamageEv(players, v, dtMobAttack, m.eid)
	h.mobKnockFrom(players, v, m.x, m.z)
	if v.health <= 0 {
		h.killMob(players, v)
		return
	}
	if !v.hostile {
		v.panic, v.fleeX, v.fleeZ, v.reroute = panicTicks, m.x, m.z, 0
	}
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
func vexTouches(m *mob, q quarry) bool {
	if q.t != nil {
		return math.Abs(m.x-q.x) < 0.5 && math.Abs(m.z-q.z) < 0.5 && m.y < q.y+1.8 && m.y+0.8 > q.y
	}
	b := q.o.box()
	half := 0.2 + b.w/2 // the vex's 0.4 box and the target's
	return math.Abs(m.x-q.x) < half && math.Abs(m.z-q.z) < half && m.y < q.y+b.h && m.y+0.8 > q.y
}
