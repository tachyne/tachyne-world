package server

import "math"

// Wolves hunt (Wolf's target goals): a wild wolf goes for sheep, rabbits
// and foxes (NonTameRandomTargetGoal, looked for one tick in ten) and for
// turtle hatchlings on land; any wolf goes for skeletons; and a tamed
// wolf goes for whatever hurt its owner or whatever its owner hit — never
// a creeper or a ghast, never a pet of the same owner. It chases at full
// pace and bites for its attack damage every twenty ticks.

const (
	wolfHuntRange    = 16.0 // FOLLOW_RANGE
	wolfHuntReach    = 2.0
	wolfBiteTicks    = 20
	wolfPreyLookOdds = 10 // NonTameRandomTargetGoal randomInterval
	wolfHuntGiveUp   = 24.0
)

func wolfPrey(o *mob) bool { // PREY_SELECTOR
	return o.etype == entitySheep || o.etype == entityRabbit || o.etype == entityFox
}

func skeletonFamily(etype int) bool { // AbstractSkeleton
	return etype == entitySkeleton || etype == entityStray || etype == entityBogged || etype == entityWitherSkeleton
}

// wolfWantsToAttack is Wolf.wantsToAttack: never creepers or ghasts, never
// a fellow pet of the same owner.
func wolfWantsToAttack(m, o *mob) bool {
	if o.etype == entityCreeper || o.etype == entityGhast {
		return false
	}
	if o.tamed && m.tamed && o.owner == m.owner {
		return false
	}
	return true
}

// wolfHuntStep runs each mob update. Returns whether it holds the wolf.
func (h *hub) wolfHuntStep(players map[int32]*tracked, m *mob) bool {
	if m.sitting || m.loveTicks > 0 || m.panic > 0 || m.tempted {
		m.wolfPrey = 0
		return false
	}
	if m.wolfBiteCD > 0 {
		m.wolfBiteCD -= mobMoveInterval
	}
	target := h.mobs[m.wolfPrey]
	if target != nil && (target.dying > 0 || target.dim != m.dim || dist3(target.x, target.y, target.z, m.x, m.y, m.z) > wolfHuntGiveUp) {
		target, m.wolfPrey = nil, 0
	}
	if target == nil {
		target = h.wolfPickTarget(players, m)
		if target == nil {
			return false
		}
		m.wolfPrey = target.eid
	}
	dx, dz := target.x-m.x, target.z-m.z
	d := math.Hypot(dx, dz)
	if d > wolfHuntReach || math.Abs(target.y-m.y) > 2 {
		h.steerTo(m, target.x, target.z, 1.0)
		m.rest = 0
		return true
	}
	m.vx, m.vz = 0, 0
	if m.wolfBiteCD <= 0 {
		m.wolfBiteCD = wolfBiteTicks
		target.lastAttacker = m.eid
		target.hurtKind(float64(m.attackDamage()), dtMobAttack)
		h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
		if target.health <= 0 {
			h.killMob(players, target)
			m.wolfPrey = 0
		}
	}
	return true
}

// wolfPickTarget is the target selector, in vanilla's priority order.
func (h *hub) wolfPickTarget(players map[int32]*tracked, m *mob) *mob {
	if m.tamed {
		if owner := players[m.owner]; owner != nil && owner.dim == m.dim {
			for _, eid := range []int32{owner.lastHurtByMob, owner.lastHitMob} { // OwnerHurtBy / OwnerHurt
				if o := h.mobs[eid]; o != nil && o != m && o.dying == 0 && wolfWantsToAttack(m, o) &&
					dist3(o.x, o.y, o.z, m.x, m.y, m.z) <= wolfHuntRange {
					return o
				}
			}
		}
	}
	var best *mob
	bestD := wolfHuntRange
	consider := func(o *mob) {
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD {
			best, bestD = o, d
		}
	}
	lookForPrey := !m.tamed && h.rng.Intn(wolfPreyLookOdds/mobMoveInterval) == 0
	h.grid().nearby(m.dim, m.x, m.z, wolfHuntRange, func(o *mob) {
		if o == m || o.dying > 0 {
			return
		}
		switch {
		case lookForPrey && (wolfPrey(o) || (o.etype == entityTurtle && o.baby && !h.inWater(o.dim, o.x, o.y, o.z))):
			consider(o)
		case skeletonFamily(o.etype):
			consider(o)
		}
	})
	return best
}
