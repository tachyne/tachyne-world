package server

import (
	"math"
)

// The rest of the piglin's brain (PiglinAi): whom it fights besides the
// players it dislikes, and what it does about a fight's end.
//
// Its attack target is the first of: the one it is angry at, the nearest
// player under universal anger, its NEMESIS — the nearest wither skeleton or
// wither it can see — and the nearest player not in gold. None at all while
// a zombified piglin stands within six blocks, or while it is backing off
// from something. A grown piglin that can hunt, has no grudge and has not
// hunted lately picks on the nearest adult hoglin it can see (StartHunting-
// Hoglin) unless a piglin in sight has hunted lately too; the grown piglins
// about join in, and all of them leave hoglins alone for thirty seconds to
// two minutes after. A bastion's piglins never hunt, and a bastion's hoglins
// are never hunted: the structure's templates say so.
//
// When the target dies it celebrates (StartCelebratingIfTargetDead): for
// fifteen seconds it goes to where the target fell, and after a hoglin one
// time in ten it dances, all the piglins that saw the same kill at once. A
// baby runs from a nemesis for five to seven seconds instead of fighting it.
// Hurt by a mob, it drops what it is doing and answers the blow, bringing
// the grown piglins about with it — unless the hoglins it can see outnumber
// the piglins, when it and the piglins in sight back off for five to twenty
// seconds and hunt no hoglin for a while.

const (
	metaIndexPiglinDancing = 19 // Piglin DATA_IS_DANCING (after the baby and charging flags)

	piglinHuntMin       = 600  // TIME_BETWEEN_HUNTS: 30–120 seconds
	piglinHuntSpan      = 1801 //
	piglinCelebrate     = 300  // CELEBRATION_TIME
	piglinDanceChance   = 0.1  // PROBABILITY_OF_CELEBRATION_DANCE
	piglinDanceSpeed    = 0.6  // SPEED_MULTIPLIER_WHEN_DANCING
	piglinCelebSpeed    = 1.0  // SPEED_MULTIPLIER_WHEN_GOING_TO_CELEBRATE_LOCATION
	piglinSightRange    = 16.0 // NEAREST_VISIBLE_LIVING_ENTITIES: the follow range
	piglinRetreatMin    = 100  // RETREAT_DURATION: 5–20 seconds
	piglinRetreatSpan   = 301
	piglinNemesisMin    = 100 // BABY_AVOID_NEMESIS_DURATION: 5–7 seconds
	piglinNemesisSpan   = 41
	piglinMuchFurther   = 4.0 // isOtherTargetMuchFurtherAwayThanCurrentAttackTarget's buffer
	piglinBabyFleeTicks = 100 // BABY_FLEE_DURATION_AFTER_GETTING_HIT
)

// playerQuarry and mobQuarry are a fight target with its aim point.
func playerQuarry(t *tracked) quarry {
	return quarry{t: t, x: t.x, y: t.y, z: t.z, aimY: t.y + 0.6}
}

func mobQuarry(o *mob) quarry {
	return quarry{o: o, x: o.x, y: o.y, z: o.z, aimY: o.y + mobEyeHeight(o)/0.85/3}
}

// piglinFoe is PiglinAi.findNearestValidAttackTarget.
func (h *hub) piglinFoe(players map[int32]*tracked, m *mob, maxDist float64) (quarry, bool) {
	if m.piglinFlee > 0 || h.piglinNearZombified(m) {
		return quarry{}, false // backing off, or a zombified piglin too close: nobody
	}
	if m.anger > 0 {
		if m.targetEID != 0 {
			if t := players[m.targetEID]; t != nil {
				if isSurvival(t.gamemode) && !t.dead && t.dim == m.dim &&
					(t.x-m.x)*(t.x-m.x)+(t.z-m.z)*(t.z-m.z) < maxDist*maxDist {
					return playerQuarry(t), true
				}
				if isSurvival(t.gamemode) && !t.dead && t.dim == m.dim {
					return quarry{}, false // angry at someone out of reach: nobody else is fought meanwhile
				}
			} else if o := h.mobs[m.targetEID]; o != nil && o.dying == 0 && o.dim == m.dim &&
				dist3(o.x, o.y, o.z, m.x, m.y, m.z) < maxDist {
				return mobQuarry(o), true // the hoglin it hunts, or the mob that hit it
			}
		} else if h.rules.UniversalAnger {
			if t := h.nearestHuntable(players, m.dim, m.x, m.z, maxDist); t != nil {
				return playerQuarry(t), true
			}
			return quarry{}, false
		}
	}
	if o := h.piglinNemesis(m, math.Min(maxDist, piglinSightRange)); o != nil {
		return mobQuarry(o), true
	}
	if t := h.nearestPiglinPrey(players, m, maxDist); t != nil {
		return playerQuarry(t), true
	}
	return quarry{}, false
}

// piglinNearZombified is PiglinAi.isNearZombified.
func (h *hub) piglinNearZombified(m *mob) bool {
	near := false
	h.grid().nearby(m.dim, m.x, m.z, piglinZombieDist, func(o *mob) {
		if !near && o.etype == entityZombifiedPiglin && o.dying == 0 &&
			dist3(o.x, o.y, o.z, m.x, m.y, m.z) < piglinZombieDist && h.mobSeesMob(m, o) {
			near = true
		}
	})
	return near
}

// isPiglinNemesis is the sensors' nemesis test: a wither skeleton or the wither.
func isPiglinNemesis(o *mob) bool {
	return o.etype == entityWitherSkeleton || o.etype == entityWither
}

// piglinNemesis is NEAREST_VISIBLE_NEMESIS.
func (h *hub) piglinNemesis(m *mob, r float64) *mob {
	var best *mob
	bestD := r
	h.grid().nearby(m.dim, m.x, m.z, r, func(o *mob) {
		if o.dying > 0 || !isPiglinNemesis(o) {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD && h.mobSeesMob(m, o) {
			best, bestD = o, d
		}
	})
	return best
}

// piglinAcquire is the piglin's share of acquireTarget: the dead-target
// rules, then its attack target — a hunt started if it has none.
func (h *hub) piglinAcquire(players map[int32]*tracked, m *mob, reach float64) {
	m.piglinCoolDown()
	h.piglinTargetDead(players, m)
	m.preyTarget = 0
	if m.baby {
		h.piglinBabyAvoidNemesis(m)
	}
	// StartAttacking is gated on isAdult: a baby piglin hunts nobody; an
	// admiring piglin has eyes only for its gold.
	if !m.baby && m.admireUntil == 0 {
		q, ok := h.piglinFoe(players, m, reach)
		if !ok && h.piglinStartHunt(players, m) {
			q, ok = h.piglinFoe(players, m, reach)
		}
		if ok {
			m.hasTarget, m.tx, m.tz = true, q.x, q.z
			m.piglinFoe = q.eid()
			if q.o != nil {
				m.preyTarget = q.o.eid // its bite, its spear and its bolts go to this one
			}
			return
		}
	}
	m.hasTarget, m.piglinFoe = false, 0
}

// piglinTargetDead runs the core activity's rules on a target that has
// died: RememberIfHoglinWasKilled, StartCelebratingIfTargetDead and
// StopBeingAngryIfTargetDead.
func (h *hub) piglinTargetDead(players map[int32]*tracked, m *mob) {
	eid := m.piglinFoe
	if eid == 0 {
		return
	}
	var pos blockPos
	hoglin, player := false, false
	if t := players[eid]; t != nil {
		if !t.dead {
			return
		}
		player, pos = true, blockPos{floorInt(t.x), floorInt(t.y), floorInt(t.z)}
	} else if o := h.mobs[eid]; o != nil {
		if o.dying == 0 {
			return
		}
		hoglin, pos = o.etype == entityHoglin, blockPos{floorInt(o.x), floorInt(o.y), floorInt(o.z)}
	} else {
		m.piglinFoe = 0 // gone without dying: StopAttackingIfTargetInvalid
		return
	}
	now := h.tick.Load()
	if hoglin {
		m.huntedUntil = now + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
	}
	if m.celebrateUntil == 0 {
		if hoglin && piglinDanceRoll(now) {
			h.setPiglinDancing(players, m, true)
		}
		m.celebrateUntil, m.celebratePos = now+piglinCelebrate, pos
		h.playSoundDim(players, m.dim, "minecraft:entity.piglin.celebrate", sndHostile, m.x, m.y, m.z, 1, h.voicePitch(m))
	}
	m.piglinFoe, m.preyTarget = 0, 0
	if (!player || h.rules.ForgiveDead) && m.targetEID == eid {
		m.anger, m.targetEID = 0, 0
	}
}

// piglinDanceRoll is wantsToDance's draw: a random source seeded with the
// game time, so every piglin that sees one hoglin fall dances or none does.
func piglinDanceRoll(now uint64) bool {
	z := now + 0x9e3779b97f4a7c15 // splitmix64
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	z ^= z >> 31
	return float64(z>>40)/float64(1<<24) < piglinDanceChance
}

// setPiglinDancing is Piglin.setDancing.
func (h *hub) setPiglinDancing(players map[int32]*tracked, m *mob, on bool) {
	if m.dancing == on {
		return
	}
	m.dancing = on
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexPiglinDancing, on)))
}

// piglinStopCelebrating erases CELEBRATE_LOCATION and DANCING.
func (h *hub) piglinStopCelebrating(players map[int32]*tracked, m *mob) {
	m.celebrateUntil = 0
	h.setPiglinDancing(players, m, false)
}

// piglinCelebrateStep is the CELEBRATE activity: to the place the target
// fell (within two blocks at full pace, or four at 0.6 when dancing), then
// milling about it. Returns whether it holds the piglin this update.
func (h *hub) piglinCelebrateStep(players map[int32]*tracked, m *mob) bool {
	if m.celebrateUntil == 0 {
		return false
	}
	if h.tick.Load() >= m.celebrateUntil || m.dying > 0 {
		h.piglinStopCelebrating(players, m)
		m.idleWalk = nil
		return false
	}
	if m.hasTarget || m.admireUntil != 0 {
		return false // FIGHT and ADMIRE_ITEM outrank it
	}
	closeEnough, speed := 2.0, piglinCelebSpeed
	if m.dancing {
		closeEnough, speed = 4, piglinDanceSpeed
	}
	p := m.celebratePos
	if dist3(float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, math.Floor(m.x)+0.5, math.Floor(m.y)+0.5, math.Floor(m.z)+0.5) >= closeEnough {
		if w := m.idleWalk; w == nil || w.eid != 0 {
			// GoToTargetLocation: a block beside the spot.
			m.idleWalk = &idleWalk{x: float64(p.x+h.rng.Intn(3)-1) + 0.5, z: float64(p.z+h.rng.Intn(3)-1) + 0.5,
				close: closeEnough, speed: speed, left: idleWalkUpdates}
		}
		return h.idleWalkStep(m)
	}
	if m.idleWalk != nil {
		return h.idleWalkStep(m)
	}
	// The celebration's RunOne: a look at a piglin, a two-block stroll at
	// 0.6, or ten to twenty ticks of nothing — a third each.
	switch h.rng.Intn(3) {
	case 1:
		if x, z, ok := h.landRandomPos(m, 2, 1); ok {
			m.idleWalk = &idleWalk{x: x, z: z, close: 1, speed: piglinDanceSpeed, left: idleWalkUpdates}
			return h.idleWalkStep(m)
		}
	case 2:
		m.idleWalk = &idleWalk{still: true, left: (10 + h.rng.Intn(11)) / mobMoveInterval}
	}
	m.vx, m.vz = m.vx*0.6, m.vz*0.6
	return true
}

// piglinStartHunt is StartHuntingHoglin. Reports whether it started one.
func (h *hub) piglinStartHunt(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if m.baby || m.noHunt || m.anger > 0 || now < m.huntedUntil || m.celebrateUntil != 0 || m.piglinFlee > 0 {
		return false // an IDLE behaviour: not while celebrating or backing off
	}
	var prey *mob
	bestD := piglinSightRange
	h.grid().nearby(m.dim, m.x, m.z, piglinSightRange, func(o *mob) {
		if o.etype != entityHoglin || o.baby || o.noHunt || o.dying > 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD && h.mobSeesMob(m, o) {
			prey, bestD = o, d
		}
	})
	if prey == nil {
		return false
	}
	seen := h.piglinsInSight(m)
	for _, o := range seen {
		if now < o.huntedUntil {
			return false // one in sight hunted lately
		}
	}
	h.piglinAngerAtMob(m, prey)
	m.huntedUntil = now + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
	h.piglinBroadcastAnger(players, m, prey)
	for _, o := range seen {
		o.huntedUntil = now + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
	}
	return true
}

// piglinsInSight is NEAREST_VISIBLE_ADULT_PIGLINS, the piglins among them
// (a brute keeps no hunting memory).
func (h *hub) piglinsInSight(m *mob) []*mob {
	var out []*mob
	h.grid().nearby(m.dim, m.x, m.z, piglinSightRange, func(o *mob) {
		if o == m || o.etype != entityPiglin || o.baby || o.dying > 0 {
			return
		}
		if dist3(o.x, o.y, o.z, m.x, m.y, m.z) < piglinSightRange && h.mobSeesMob(m, o) {
			out = append(out, o)
		}
	})
	return out
}

// piglinAngerAtMob is PiglinAi.setAngerTarget on a mob: ANGRY_AT for 600
// ticks, and a hoglin also puts it off hunting for a while.
func (h *hub) piglinAngerAtMob(m, o *mob) {
	m.anger, m.targetEID, m.unseenTicks = piglinAngerUpdates, o.eid, 0
	m.hasTarget, m.tx, m.tz = true, o.x, o.z
	if o.etype == entityHoglin && !m.noHunt {
		m.huntedUntil = h.tick.Load() + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
	}
}

// angerTargetPos is where the one a piglin is angry at stands.
func (h *hub) angerTargetPos(players map[int32]*tracked, m *mob) (float64, float64, bool) {
	if m.anger == 0 || m.targetEID == 0 {
		return 0, 0, false
	}
	if t := players[m.targetEID]; t != nil {
		return t.x, t.z, true
	}
	if o := h.mobs[m.targetEID]; o != nil && o.dying == 0 {
		return o.x, o.z, true
	}
	return 0, 0, false
}

// piglinBroadcastAnger is broadcastAngerTarget with a mob target: every
// grown piglin about that has nobody nearer takes it on — for a hoglin, only
// the ones that may hunt it.
func (h *hub) piglinBroadcastAnger(players map[int32]*tracked, m, target *mob) {
	h.grid().nearby(m.dim, m.x, m.z, piglinGuardRange, func(o *mob) {
		if o == m || o.etype != entityPiglin || o.baby || o.dying > 0 {
			return
		}
		if target.etype == entityHoglin && (o.noHunt || target.noHunt) {
			return
		}
		if dist3(target.x, target.y, target.z, o.x, o.y, o.z) >= o.followRange() {
			return // not attackable from where it stands
		}
		if x, z, ok := h.angerTargetPos(players, o); ok &&
			(o.x-x)*(o.x-x)+(o.z-z)*(o.z-z) <= (o.x-target.x)*(o.x-target.x)+(o.z-target.z)*(o.z-target.z) {
			return // setAngerTargetIfCloserThanCurrent
		}
		h.piglinAngerAtMob(o, target)
	})
}

// piglinBabyAvoidNemesis is babyAvoidNemesis: a baby that sees a nemesis
// runs from it for five to seven seconds.
func (h *hub) piglinBabyAvoidNemesis(m *mob) {
	if m.piglinFlee > 0 {
		return
	}
	if o := h.piglinNemesis(m, piglinSightRange); o != nil {
		h.piglinAvoid(m, o, piglinNemesisMin+h.rng.Intn(piglinNemesisSpan))
	}
}

// piglinAvoid sets AVOID_TARGET: the avoid step walks it away from o.
func (h *hub) piglinAvoid(m, o *mob, ticks int) {
	m.piglinFlee, m.piglinFleeFrom = ticks, o.eid
	m.piglinFleeX, m.piglinFleeZ = o.x, o.z
	m.hasTarget = false
}

// piglinStopAdmiring is stopHoldingOffHandItem without the barter: the
// ingot goes to its pocket, and the admiring ends.
func (h *hub) piglinStopAdmiring(players map[int32]*tracked, m *mob) {
	if m.admireUntil == 0 {
		return
	}
	m.admireUntil = 0
	if m.offhand.item != 0 {
		m.hoard = append(m.hoard, m.offhand)
		m.offhand = invStack{}
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
}

// mobHurtByMob is wasHurtBy for a mob's blow, for the brains that answer it.
func (h *hub) mobHurtByMob(players map[int32]*tracked, v, a *mob) {
	if v.dying > 0 || v.health <= 0 || a == nil || a.dying > 0 {
		return
	}
	switch v.etype {
	case entityPiglin:
		h.piglinHurtByMob(players, v, a)
	case entityHoglin:
		h.hoglinHurtByMob(v, a)
	case entityWanderingTrader:
		h.traderLlamasDefendMob(v, a)
	}
}

// piglinHurtByMob is PiglinAi.wasHurtBy for a mob that is not a piglin.
func (h *hub) piglinHurtByMob(players map[int32]*tracked, m, a *mob) {
	if a.etype == entityPiglin {
		return
	}
	h.piglinStopAdmiring(players, m)
	h.piglinStopCelebrating(players, m)
	if m.piglinFlee > 0 && m.piglinFleeFrom != 0 {
		if o := h.mobs[m.piglinFleeFrom]; o == nil || o.etype != a.etype {
			m.piglinFlee, m.piglinFleeFrom = 0, 0 // a different kind of attacker: the old avoidance ends
		}
	}
	if m.baby {
		h.piglinAvoid(m, a, piglinBabyFleeTicks)
		if a.dim == m.dim && dist3(a.x, a.y, a.z, m.x, m.y, m.z) < m.followRange() {
			h.piglinBroadcastAnger(players, m, a)
		}
		return
	}
	if a.etype == entityHoglin && h.hoglinsOutnumberPiglins(m) {
		h.piglinRetreatFrom(m, a)
		for _, o := range h.piglinsInSight(m) {
			h.piglinRetreatFrom(o, a) // broadcastRetreat
		}
		return
	}
	// maybeRetaliate: not while backing off, not at someone out of range,
	// and not at someone much farther than the target it has.
	if m.piglinFlee > 0 || a.dim != m.dim || dist3(a.x, a.y, a.z, m.x, m.y, m.z) >= m.followRange() {
		return
	}
	if m.hasTarget {
		cur := (m.tx-m.x)*(m.tx-m.x) + (m.tz-m.z)*(m.tz-m.z)
		if d := (a.x-m.x)*(a.x-m.x) + (a.z-m.z)*(a.z-m.z); d > cur+piglinMuchFurther*piglinMuchFurther {
			return
		}
	}
	h.piglinAngerAtMob(m, a)
	h.piglinBroadcastAnger(players, m, a)
}

// hoglinsOutnumberPiglins counts the grown hoglins and piglins in sight
// (the piglin itself among the piglins).
func (h *hub) hoglinsOutnumberPiglins(m *mob) bool {
	piglins, hoglins := 1, 0
	h.grid().nearby(m.dim, m.x, m.z, piglinSightRange, func(o *mob) {
		if o == m || o.baby || o.dying > 0 || dist3(o.x, o.y, o.z, m.x, m.y, m.z) >= piglinSightRange {
			return
		}
		switch o.etype {
		case entityPiglin, entityPiglinBrute:
			if h.mobSeesMob(m, o) {
				piglins++
			}
		case entityHoglin:
			if h.mobSeesMob(m, o) {
				hoglins++
			}
		}
	})
	return hoglins > piglins
}

// piglinRetreatFrom is setAvoidTargetAndDontHuntForAWhile.
func (h *hub) piglinRetreatFrom(m, o *mob) {
	m.anger, m.targetEID, m.preyTarget, m.piglinFoe = 0, 0, 0, 0
	h.piglinAvoid(m, o, piglinRetreatMin+h.rng.Intn(piglinRetreatSpan))
	m.huntedUntil = h.tick.Load() + uint64(piglinHuntMin+h.rng.Intn(piglinHuntSpan))
}

// hoglinHurtByMob is HoglinAi.wasHurtBy for a mob's blow: pacified no
// longer, a piglet runs, and a grown hoglin turns on whatever hit it that
// is not a hoglin — unless it is already retreating from piglins and a
// piglin it was — and the grown hoglins about join it.
func (h *hub) hoglinHurtByMob(v, a *mob) {
	v.hogPacified = 0
	if v.baby {
		v.hogRetreat = 100 + h.rng.Intn(301)
		v.hogRetreatX, v.hogRetreatZ = a.x, a.z
		return
	}
	if a.etype == entityHoglin || (v.hogRetreat > 0 && a.etype == entityPiglin) {
		return
	}
	if a.dim != v.dim || dist3(a.x, a.y, a.z, v.x, v.y, v.z) >= v.followRange() {
		return
	}
	v.fightBack = a.eid
	h.grid().nearby(v.dim, v.x, v.z, hoglinSeeRange, func(o *mob) {
		if o == v || o.etype != entityHoglin || o.baby || o.dying > 0 || o.hogPacified > 0 || o.hogRetreat > 0 {
			return
		}
		if cur := h.mobs[o.fightBack]; cur != nil && cur.dying == 0 &&
			dist3(cur.x, cur.y, cur.z, o.x, o.y, o.z) <= dist3(a.x, a.y, a.z, o.x, o.y, o.z) {
			return
		}
		o.fightBack = a.eid // broadcastAttackTarget
	})
}
