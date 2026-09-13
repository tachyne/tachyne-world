package server

import "math"

// Mob-on-mob aggression for the two that vanilla gives it beyond players:
// a zoglin (Zoglin.isTargetable) goes for any living thing within its
// follow range that is not a zoglin or a creeper — biting for its hoglin
// damage every forty ticks (fifteen for a baby) and tossing what it bites
// — and an enderman goes for endermites within sixty-four (its
// NearestAttackableTargetGoal<Endermite>). Players keep the ordinary
// hostile hunt; this is the other half.

const (
	mobHuntReach   = 2.0
	mobHuntTimeout = 20 // bites every 20 ticks (the ordinary melee cadence)
)

// mobHuntPrey is what the species fights that is not a player.
func mobHuntPrey(m, o *mob) bool {
	switch m.etype {
	case entityZoglin:
		return o.etype != entityZoglin && o.etype != entityCreeper && !o.tamed
	case entityEnderman:
		return o.etype == entityEndermite
	}
	return false
}

// mobHuntStep runs each mob update. Returns whether it holds the mob.
func (h *hub) mobHuntStep(players map[int32]*tracked, m *mob) bool {
	if m.hasTarget { // a player in reach outranks the mob hunt (StartAttacking picks the nearest; players first here)
		if t := h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange()); t != nil {
			m.wolfPrey = 0
			return false
		}
	}
	if m.wolfBiteCD > 0 {
		m.wolfBiteCD -= mobMoveInterval
	}
	target := h.mobs[m.wolfPrey]
	if target != nil && (target.dying > 0 || target.dim != m.dim || dist3(target.x, target.y, target.z, m.x, m.y, m.z) > m.followRange()+8) {
		target, m.wolfPrey = nil, 0
	}
	if target == nil {
		bestD := m.followRange()
		h.grid().nearby(m.dim, m.x, m.z, bestD, func(o *mob) {
			if o == m || o.dying > 0 || !mobHuntPrey(m, o) {
				return
			}
			if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD {
				target, bestD = o, d
			}
		})
		if target == nil {
			return false
		}
		m.wolfPrey = target.eid
	}
	dx, dz := target.x-m.x, target.z-m.z
	if d := math.Hypot(dx, dz); d > mobHuntReach || math.Abs(target.y-m.y) > 2 {
		h.steerTo(m, target.x, target.z, 1.0)
		m.rest = 0
		return true
	}
	m.vx, m.vz = 0, 0
	if m.wolfBiteCD <= 0 {
		m.wolfBiteCD = mobHuntTimeout
		dmg := float64(m.attackDamage())
		if m.etype == entityZoglin {
			m.wolfBiteCD = hoglinAttackCD(m) * mobMoveInterval
			dmg = float64(h.hoglinBiteDamage(m))
			h.hoglinBiteStart(players, m)
		}
		target.lastAttacker = m.eid
		target.hurtKind(dmg, dtMobAttack)
		h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
		if m.etype == entityZoglin && !m.baby { // HoglinBase.throwTarget on a mob
			if kdx, kdz := target.x-m.x, target.z-m.z; kdx != 0 || kdz != 0 {
				dd := math.Hypot(kdx, kdz)
				f := h.rng.Float64()*0.5 + 0.2
				target.vx, target.vz, target.kb, target.reroute = kdx/dd*f*mobMoveInterval, kdz/dd*f*mobMoveInterval, 3, 0
			}
		}
		if target.health <= 0 {
			h.killMob(players, target)
			m.wolfPrey = 0
		}
	}
	return true
}
