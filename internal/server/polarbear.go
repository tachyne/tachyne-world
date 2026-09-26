package server

import "math"

// Polar bears (PolarBearAttackPlayersGoal, PolarBearHurtByTargetGoal,
// PolarBearMeleeAttackGoal): an adult with a cub within eight blocks turns
// on any player within ten; a hit bear rouses every adult bear around it
// (a hit cub only rouses the others and does not fight); and a bear about
// to bite rears up on its hind legs with a warning growl, dropping again
// as the bite lands or the target draws off.

const (
	metaIndexBearStanding = 17 // DATA_STANDING_ID (1.21.5; an Animal, so 18 on 26.2)
	bearCubRangeXZ        = 8.0
	bearCubRangeY         = 4.0
	bearGuardRange        = 10.0 // NearestAttackableTargetGoal follow distance × 0.5
	bearStandReach        = 3.0  // within (width + 3) of the target
	bearStandCD           = 10   // getTicksUntilNextAttack <= 10
	bearFoxRange          = 20.0 // FOLLOW_RANGE: how far the fox target goal looks
	bearFoxLookOdds       = 10   // NearestAttackableTargetGoal randomInterval
	bearChaseSpeed        = 1.25 // PolarBearMeleeAttackGoal: MeleeAttackGoal(1.25, true)
	bearBiteReach         = 2.0
	bearBiteTicks         = 20
)

func bearStandingMeta(m *mob) []byte { return boolMeta(m.eid, metaIndexBearStanding, m.bearStanding) }

func (h *hub) setBearStanding(players map[int32]*tracked, m *mob, on bool) {
	if m.bearStanding == on {
		return
	}
	m.bearStanding = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(bearStandingMeta(m)))
	if on {
		h.playSoundDim(players, m.dim, "minecraft:entity.polar_bear.warning", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// bearHasCubNear is the attack-players goal's check.
func (h *hub) bearHasCubNear(m *mob) bool {
	found := false
	h.grid().nearby(m.dim, m.x, m.z, bearCubRangeXZ, func(o *mob) {
		if o != m && o.etype == entityPolarBear && o.baby && o.dying == 0 && math.Abs(o.y-m.y) <= bearCubRangeY &&
			math.Abs(o.x-m.x) <= bearCubRangeXZ && math.Abs(o.z-m.z) <= bearCubRangeXZ {
			found = true
		}
	})
	return found
}

// polarBearStep runs each mob update; it never holds the bear (the hunt
// and the bite are the ordinary hostile paths once provoked).
func (h *hub) polarBearStep(players map[int32]*tracked, m *mob) {
	// A cub in sight makes any player within ten a target.
	if !m.hostile && !m.baby && h.bearHasCubNear(m) {
		if t := h.nearestTargetable(players, m, bearGuardRange); t != nil {
			h.provoke(m, t)
		}
	}
	if !m.hostile || !m.hasTarget {
		if m.wolfPrey == 0 { // a fox hunt rears on its own clock (bearFoxStep)
			h.setBearStanding(players, m, false)
		}
		return
	}
	t := h.nearestTargetable(players, m, m.followRange())
	if t == nil {
		h.setBearStanding(players, m, false)
		return
	}
	reach := 0.6 + bearStandReach // the player's width plus three
	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) < reach*reach {
		if m.attackCD*mobMoveInterval <= bearStandCD {
			h.setBearStanding(players, m, true) // rearing up, the warning
		}
		return
	}
	h.setBearStanding(players, m, false)
}

// bearFoxStep is the adult bear's NearestAttackableTargetGoal<Fox> (target
// priority 4, one look in ten ticks, the fox in sight) run through its
// PolarBearMeleeAttackGoal: it lumbers after the fox at 1.25, rears as the
// bite comes due, and bites every twenty ticks. A provoked bear is on the
// hostile path instead; a cub never hunts. Returns whether it holds the bear.
// (The bear borrows the wolf's prey and bite-cooldown fields.)
func (h *hub) bearFoxStep(players map[int32]*tracked, m *mob) bool {
	if m.baby || m.hostile || m.panic > 0 || m.tempted {
		m.wolfPrey = 0
		return false
	}
	if m.wolfBiteCD > 0 {
		m.wolfBiteCD -= mobMoveInterval
	}
	fox := h.mobs[m.wolfPrey]
	if fox != nil && (fox.dying > 0 || fox.dim != m.dim || dist3(fox.x, fox.y, fox.z, m.x, m.y, m.z) > bearFoxRange) {
		fox, m.wolfPrey = nil, 0
	}
	if fox == nil {
		if h.rng.Intn(bearFoxLookOdds/mobMoveInterval) != 0 {
			return false
		}
		best := bearFoxRange
		h.grid().nearby(m.dim, m.x, m.z, bearFoxRange, func(o *mob) {
			if o.etype != entityFox || o.dying > 0 {
				return
			}
			if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best && h.mobSeesMob(m, o) {
				fox, best = o, d
			}
		})
		if fox == nil {
			return false
		}
		m.wolfPrey = fox.eid
	}
	m.rest = 0
	if math.Hypot(fox.x-m.x, fox.z-m.z) > bearBiteReach || math.Abs(fox.y-m.y) > 2 {
		h.setBearStanding(players, m, false)
		h.steerTo(m, fox.x, fox.z, bearChaseSpeed)
		return true
	}
	m.vx, m.vz = 0, 0
	if m.wolfBiteCD > 0 {
		h.setBearStanding(players, m, m.wolfBiteCD <= bearStandCD) // up on its hind legs, the warning
		return true
	}
	h.setBearStanding(players, m, false)
	m.wolfBiteCD = bearBiteTicks
	fox.lastAttacker = m.eid
	fox.hurtKind(float64(m.attackDamage()), dtMobAttack)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	if fox.health <= 0 {
		h.killMob(players, fox)
		m.wolfPrey = 0
	} else if panicsAt(fox, dtMobAttack) {
		fox.panic, fox.fleeX, fox.fleeZ = h.panicFor(fox), m.x, m.z
	}
	return true
}
