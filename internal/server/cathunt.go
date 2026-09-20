package server

import "math"

// Cats and ocelots hunt (Cat's NonTameRandomTargetGoal for rabbits and
// turtle hatchlings on land, Ocelot's NearestAttackableTargetGoal for
// chickens and hatchlings, OcelotAttackGoal): a wild cat, or any ocelot,
// picks its prey within sixteen — looked for one tick in ten — creeps up
// on it and bites every twenty ticks, giving up past fifteen blocks.

const (
	catHuntRange  = 16.0
	catHuntGiveUp = 15.0 // OcelotAttackGoal: distanceToSqr > 225
	catHuntReach  = 2.0
	catBiteTicks  = 20
)

func catPrey(m, o *mob) bool {
	if o.etype == entityTurtle {
		return o.baby
	}
	if m.etype == entityOcelot {
		return o.etype == entityChicken
	}
	return o.etype == entityRabbit
}

// catHuntStep runs each mob update for a cat or ocelot. Returns whether
// it holds the mob.
func (h *hub) catHuntStep(players map[int32]*tracked, m *mob) bool {
	if (m.etype == entityCat && m.tamed) || m.sitting || m.loveTicks > 0 || m.panic > 0 || m.tempted {
		m.wolfPrey = 0
		return false
	}
	if m.wolfBiteCD > 0 {
		m.wolfBiteCD -= mobMoveInterval
	}
	target := h.mobs[m.wolfPrey]
	if target != nil && (target.dying > 0 || target.dim != m.dim || dist3(target.x, target.y, target.z, m.x, m.y, m.z) > catHuntGiveUp) {
		target, m.wolfPrey = nil, 0
	}
	if target == nil {
		if h.rng.Intn(wolfPreyLookOdds/mobMoveInterval) != 0 {
			return false
		}
		bestD := catHuntRange
		h.grid().nearby(m.dim, m.x, m.z, catHuntRange, func(o *mob) {
			if o == m || o.dying > 0 || !catPrey(m, o) {
				return
			}
			if o.etype == entityTurtle && h.inWater(o.dim, o.x, o.y, o.z) {
				return // BABY_ON_LAND
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
	if d := math.Hypot(dx, dz); d > catHuntReach || math.Abs(target.y-m.y) > 2 {
		h.steerTo(m, target.x, target.z, 1.0)
		m.rest = 0
		return true
	}
	m.vx, m.vz = 0, 0
	if m.wolfBiteCD <= 0 {
		m.wolfBiteCD = catBiteTicks
		target.lastAttacker = m.eid
		target.hurtKind(float64(m.attackDamage()), dtMobAttack)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
		if target.health <= 0 {
			h.killMob(players, target)
			m.wolfPrey = 0
		}
	}
	return true
}
