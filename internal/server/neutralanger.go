package server

// How a provoked animal calms down. A wolf, polar bear, llama, panda or
// dolphin that is hit turns on its attacker (HurtByTargetGoal), and the
// engine made it hostile to do so — but nothing ever turned it back: once
// the attacker was gone it went for whichever player it saw next, forever.
//
// Vanilla keeps two things apart. HurtByTargetGoal holds the attacker while
// it is in follow range and seen within the last 300 ticks. The NeutralMob
// species (wolf, polar bear) also remember whom they are angry at for 20-39
// seconds (PERSISTENT_ANGER_TIME, restarted while they hold the target), and
// re-target that player on sight until it runs out. A llama drops its
// target the moment it has spat (LlamaHurtByTargetGoal). When nothing is
// left the animal goes back to its peaceful life.

const (
	neutralAngerMin   = 20 * 20 / mobMoveInterval // PERSISTENT_ANGER_TIME: 20-39 s, in mob updates
	neutralAngerRange = 19 * 20 / mobMoveInterval
)

// persistentAnger are the NeutralMob retaliators.
func persistentAnger(etype int) bool {
	return etype == entityWolf || etype == entityPolarBear || etype == entityBee
}

// angerHeldWhileTargeting is updatePersistentAnger's stayAngryIfTargetPresent:
// a wolf or a polar bear stays angry for as long as it holds its target, but a
// bee passes false, so its 20-39 seconds run out even mid-chase and it gives
// up (Bee.customServerAiStep).
func angerHeldWhileTargeting(etype int) bool {
	return etype != entityBee
}

// neutralAngerTime is a fresh PERSISTENT_ANGER_TIME roll.
func (h *hub) neutralAngerTime() int {
	return neutralAngerMin + h.rng.Intn(neutralAngerRange+1)
}

// provokedTarget resolves a provoked retaliator's target in place of
// acquireTarget, which would take any player at all. Returns false when
// acquireTarget should run instead (universal anger, or a species it does
// not cover).
func (h *hub) provokedTarget(players map[int32]*tracked, m *mob) bool {
	if !m.retaliates || m.tamed {
		return false
	}
	persistent := persistentAnger(m.etype)
	if persistent && m.anger > 0 {
		m.anger--
	}
	if m.etype == entityBee && (m.anger == 0 || m.beeStingDie > 0) {
		// A bee's anger ends with its clock or its sting (doHurtTarget calls
		// stopBeingAngry), and BeeHurtByOtherGoal and BeeAttackGoal both
		// want it angry: it lets the target go and goes back to its flowers.
		h.calmDown(m)
		return true
	}
	reach := m.followRange()
	valid := func(eid int32) *tracked {
		t := players[eid]
		if t == nil || !isSurvival(t.gamemode) || t.dead || t.dim != m.dim ||
			sq(t.x-m.x)+sq(t.z-m.z) > reach*reach {
			return nil
		}
		return t
	}
	// HurtByTargetGoal.canContinueToUse: in range, seen within 300 ticks.
	if t := valid(m.targetEID); t != nil {
		if h.mobSees(m, t) {
			m.unseenTicks = 0
		} else {
			m.unseenTicks += mobMoveInterval
		}
		if m.unseenTicks <= hurtByUnseenMemory {
			m.hasTarget, m.tx, m.tz = true, t.x, t.z
			if persistent && angerHeldWhileTargeting(m.etype) && m.anger < neutralAngerMin {
				m.anger = h.neutralAngerTime() // updatePersistentAnger(…, true): held target, fresh timer
			}
			return true
		}
	}
	m.targetEID, m.unseenTicks, m.hasTarget = 0, 0, false
	if persistent && m.anger > 0 {
		if m.angryAt == 0 {
			return false // universal anger: the nearest player will do
		}
		// NearestAttackableTargetGoal<Player>(isAngryAt): the grudge is
		// taken up again on sight.
		if t := valid(m.angryAt); t != nil && h.mobSees(m, t) {
			m.targetEID = t.p.eid
			m.hasTarget, m.tx, m.tz = true, t.x, t.z
		}
		return true
	}
	h.calmDown(m)
	return true
}

// calmDown is stopBeingAngry plus the lost target: the animal is peaceful
// again.
func (h *hub) calmDown(m *mob) {
	m.anger, m.targetEID, m.angryAt, m.unseenTicks = 0, 0, 0, 0
	m.llamaDefending, m.zpHeld = false, false
	if m.etype == entityEnderman {
		// Enderman.setTarget(null): the look goal lets go, the daylight
		// clock reads from zero and STARED_AT comes down (endermanSync
		// tells the viewers).
		m.angryUUID, m.enderLook, m.enderTargetAt, m.enderStared, m.enderHeld = [16]byte{}, false, 0, false, false
	}
	if m.etype == entityZombifiedPiglin || m.etype == entityEnderman {
		m.hasTarget = false // a monster calms down but stays one
		return
	}
	m.hostile, m.behavior, m.hasTarget = false, Behavior(wanderBehavior{}), false
	if m.etype == entityBee {
		m.behavior = beeBehavior{} // back to its flower-and-hive errands
	}
}
