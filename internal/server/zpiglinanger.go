package server

import (
	"math"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// ZombifiedPiglin's anger (NeutralMob + its own customServerAiStep). Hit
// one and it takes the attacker for its target and holds a grudge that
// stays topped up for as long as it has a target, then runs out 20 to 39
// seconds after it loses them (PERSISTENT_ANGER_TIME); until then it goes
// for that player again on sight, and nobody else. While it has a target
// it calls the pack in again every four to six seconds it can see them
// (ALERT_INTERVAL), grunts its angry call a moment after it first turns
// (FIRST_ANGER_SOUND_DELAY), and, grown up, moves 0.05 faster while angry
// (SPEED_MODIFIER_ATTACKING).

const (
	zpAngerMin      = 20 * 20 / mobMoveInterval // PERSISTENT_ANGER_TIME 20-39 s, in updates
	zpAngerSpan     = 19 * 20 / mobMoveInterval
	zpAlertMin      = 4 * 20 / mobMoveInterval // ALERT_INTERVAL 4-6 s, in updates
	zpAlertSpan     = 2 * 20 / mobMoveInterval
	zpFirstSoundMax = 20 // FIRST_ANGER_SOUND_DELAY 0-1 s, in ticks
	zpAttackSpeed   = 0.05
	zpSpeedSource   = "minecraft:attacking" // SPEED_MODIFIER_ATTACKING_ID
)

// zombifiedPiglinAngerAt is setTarget from a hurt or an alert, with the
// persistent grudge it starts (updatePersistentAnger on a new target).
func (h *hub) zombifiedPiglinAngerAt(m *mob, t *tracked) {
	if t == nil || m.dying > 0 {
		return
	}
	if m.targetEID == 0 && !m.hasTarget {
		h.zombifiedPiglinFirstTarget(m)
	}
	m.anger, m.zpHeld = h.rng.Intn(zpAngerSpan+1)+zpAngerMin, true
	m.targetEID, m.angryAt, m.unseenTicks = t.p.eid, t.p.eid, 0
	if h.rules.UniversalAnger {
		m.angryAt = 0 // angry at every player: the nearest will do once this one is gone
	}
	m.hasTarget, m.tx, m.tz = true, t.x, t.z
}

// zombifiedPiglinFirstTarget is setTarget's first-target bookkeeping.
func (h *hub) zombifiedPiglinFirstTarget(m *mob) {
	m.zpSoundIn = h.rng.Intn(zpFirstSoundMax + 1)
	m.zpAlertIn = zpAlertMin + h.rng.Intn(zpAlertSpan+1)
}

// zombifiedPiglinTarget stands in for acquireTarget: the attacker while it
// is in range and seen within 300 ticks, else whoever it is angry at, on
// sight; the grudge held while there is a target, spent after.
func (h *hub) zombifiedPiglinTarget(players map[int32]*tracked, m *mob) {
	reach := m.followRange()
	valid := func(eid int32) *tracked {
		t := players[eid]
		if t == nil || !isSurvival(t.gamemode) || t.dead || t.dim != m.dim ||
			sq(t.x-m.x)+sq(t.z-m.z) > reach*reach {
			return nil
		}
		return t
	}
	had := m.hasTarget
	var tgt *tracked
	if t := valid(m.targetEID); t != nil {
		if h.mobSees(m, t) {
			m.unseenTicks = 0
		} else {
			m.unseenTicks += mobMoveInterval
		}
		if m.unseenTicks <= hurtByUnseenMemory {
			tgt = t
		}
	}
	if tgt == nil {
		m.targetEID, m.unseenTicks = 0, 0
		if m.zpHeld { // the target is gone: the timer runs from a fresh roll
			m.anger, m.zpHeld = h.rng.Intn(zpAngerSpan+1)+zpAngerMin, false
		}
		if m.anger > 0 {
			m.anger--
			var pick *tracked
			if m.angryAt != 0 {
				pick = valid(m.angryAt)
			} else {
				pick = h.nearestHuntable(players, m.dim, m.x, m.z, reach)
			}
			if pick != nil && h.mobSees(m, pick) { // NearestAttackableTargetGoal(isAngryAt), mustSee
				tgt = pick
				m.targetEID = pick.p.eid
			}
		}
		if m.anger == 0 {
			m.angryAt = 0 // stopBeingAngry
		}
	}
	if tgt == nil {
		m.hasTarget = false
		return
	}
	if !had {
		h.zombifiedPiglinFirstTarget(m)
	}
	m.zpHeld = true // updatePersistentAnger(…, true): a target keeps it angry
	m.hasTarget, m.tx, m.tz = true, tgt.x, tgt.z
	// maybeAlertOthers: every four to six seconds, if it can see them.
	if m.zpAlertIn--; m.zpAlertIn <= 0 {
		if h.mobSees(m, tgt) {
			h.alertZombifiedPiglins(m, tgt)
		}
		m.zpAlertIn = zpAlertMin + h.rng.Intn(zpAlertSpan+1)
	}
}

// zombifiedPiglinAngerTick is customServerAiStep's speed and first sound.
func (h *hub) zombifiedPiglinAngerTick(players map[int32]*tracked, m *mob) {
	angry := m.anger > 0 || m.zpHeld
	speed := m.mobAttrs().Get(attr.MovementSpeed)
	switch {
	case angry && !m.baby && !speed.HasModifier(zpSpeedSource):
		speed.AddModifier(attr.Modifier{Source: zpSpeedSource, Amount: zpAttackSpeed * attrToStep, Op: attr.AddValue})
	case !angry && speed.HasModifier(zpSpeedSource):
		speed.RemoveModifier(zpSpeedSource)
	}
	if angry && m.zpSoundIn > 0 {
		if m.zpSoundIn -= mobMoveInterval; m.zpSoundIn <= 0 {
			m.zpSoundIn = 0
			h.playSoundDim(players, m.dim, "minecraft:entity.zombified_piglin.angry", sndHostile,
				m.x, m.y, m.z, 2, float32(math.Min(2, float64(h.voicePitch(m))*1.8)))
		}
	}
}
