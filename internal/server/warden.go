package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Warden behaviour: a Darkness aura, the FIGHT activity (a heavy melee blow
// every 18 ticks, and the charged sonic boom for a target it cannot reach),
// and digging away after a spell with nobody to be angry at. The Warden is
// blind: it attacks only whoever it has grown ANGRY at (wardenanger.go), and
// it sniffs, roars and burrows through the poses in wardenpose.go.

// entityStatusSonicCharge is Warden.handleEntityEvent 62: the charge-up.
const entityStatusSonicCharge = 62

const (
	wardenDarknessR = 20.0 // players this close get the Darkness effect
	wardenSonicR    = 15.0 // sonic-boom reach sideways (SonicBoom: closerThan 15, 20)
	wardenSonicRY   = 20.0 // …and up or down
	wardenSonicDmg  = 10.0 // sonic boom bypasses armour AND shields
	// SonicBoom runs for DURATION 60 ticks: the charge sound at the start, the
	// blast itself at TICKS_BEFORE_PLAYING_SOUND 34, and a 40-tick cooldown
	// when it stops — booms land 100 ticks apart at best. While it runs the
	// warden does not melee (ATTACK_COOLING_DOWN for the whole duration).
	wardenSonicRunUpd  = 60 / mobMoveInterval
	wardenSonicHitUpd  = 34 / mobMoveInterval
	wardenSonicCoolUpd = 40 / mobMoveInterval
	// Warden.setAttackTarget: a fresh target holds the boom off for 200
	// ticks, TIME_TO_USE_MELEE_UNTIL_SONIC_BOOM — it walks up and hits first.
	wardenSonicFreshUpd = 200 / mobMoveInterval
	// MeleeAttack.create(18): 18 ticks between blows. mobMelee counts the
	// biting update itself, so this is one short of the nine updates.
	wardenMeleeCD = 18/mobMoveInterval - 1
	// WardenAi.DIGGING_COOLDOWN: 1200 ticks after the last interaction before
	// it burrows away. This counted 300 updates — 600 ticks, half of vanilla —
	// and the comment claiming ~60 s was reading updates as ticks.
	wardenDigAwayUpd = 1200 / mobMoveInterval
)

// wardenTick runs once per mob update (every mobMoveInterval ticks) for a Warden.
func (h *hub) wardenTick(players map[int32]*tracked, m *mob) {
	defer h.wardenSyncAnger(players, m)
	// Darkness dread for everyone nearby (refreshed so it never lapses in range).
	// Warden.applyDarknessAround, every 120 ticks (offset by its id): 260
	// ticks of Darkness to each survival player within 20 blocks, unless the
	// one they have still runs past 200 (addEffectToPlayersAround).
	if (h.tick.Load()+uint64(m.eid))%120 < mobMoveInterval {
		for _, t := range players {
			if t.dim != m.dim || t.dead || !isSurvival(t.gamemode) ||
				dist3(t.x, t.y, t.z, m.x, m.y, m.z) >= wardenDarknessR {
				continue
			}
			if e := t.effects[effDarkness]; e != nil && e.left >= 200 {
				continue
			}
			h.applyEffectTicks(players, t, effDarkness, 0, 260)
		}
	}

	// Warden.doPush: a player it is touching riles it.
	if m.wardenPoseLeft == 0 || (m.wardenPose != poseEmerging && m.wardenPose != poseDigging) {
		h.wardenTouches(players, m)
	}

	// Emerging, roaring, sniffing or burrowing: the warden is rooted to the
	// spot for the length of the animation and does nothing else.
	if h.wardenPoseTick(players, m) {
		return
	}

	// AngerManagement: it goes for whoever it is angriest at (SetRoarTarget
	// takes getEntityAngryAt, which needs ANGRY). A player it has not got
	// angry at is only somebody to sniff for: Sniffing.stop, a touch or a
	// vibration raise the grudge, and nothing else makes it attack.
	t := h.wardenAngerTickOne(players, m)
	if t == nil {
		m.wardenTarget, m.sonicRun = 0, 0
		near := h.nearestHuntable(players, m.dim, m.x, m.z, 24)
		// Investigating sets SNIFF_COOLDOWN, and INVESTIGATE outranks SNIFF.
		if near != nil && !h.wardenInvestigating(m) && h.wardenSniffTry(players, m) {
			return
		}
	}
	if t == nil {
		if m.digClock++; m.digClock >= wardenDigAwayUpd {
			// Digging: it burrows for a hundred ticks and is then DISCARDED —
			// not a death, so no loot and no experience. Caught off the ground
			// it just grumbles and goes (Digging.checkExtraStartConditions).
			if !m.grounded() {
				h.playSoundDim(players, m.dim, "minecraft:entity.warden.agitated", sndHostile, m.x, m.y, m.z, 5, 1)
				h.removeMob(players, m)
				return
			}
			h.wardenPoseStart(players, m, poseDigging, wardenDigUpd, "minecraft:entity.warden.dig", 5)
			return
		}
		return
	}
	m.digClock = 0
	// Roar: a warden that has just fixed on someone rears up and bellows
	// before it comes for them — the roar target only becomes the attack
	// target when the roar ends (Roar.stop → setAttackTarget), which holds
	// the sonic boom off for TIME_TO_USE_MELEE_UNTIL_SONIC_BOOM.
	if m.wardenTarget != t.p.eid {
		m.wardenTarget, m.sonicRun = t.p.eid, 0
		h.wardenAngerAt(m, t.p.eid, 20) // Roar.ROAR_ANGER_INCREASE
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
		h.wardenPoseStart(players, m, poseRoaring, wardenRoarUpd, "", 0)
		m.sonicCD = wardenSonicFreshUpd // counts down once the roar is over
		return
	}
	h.wardenFight(players, m, t)
}

// wardenStruckBy is the rest of Warden.hurtServer for a direct blow: with no
// attack target yet, the attacker becomes it at once (setAttackTarget, which
// skips the roar) and the boom is held off for 200 ticks. A warden busy
// emerging or digging takes no notice; a sniff is cut short, as FIGHT
// outranks SNIFF.
func (h *hub) wardenStruckBy(players map[int32]*tracked, m *mob, t *tracked) {
	if m.wardenTarget != 0 || m.dying != 0 {
		return
	}
	switch {
	case m.wardenPoseLeft > 0 && m.wardenPose == poseSniffing:
		h.wardenStand(players, m)
	case m.wardenPoseLeft > 0:
		return
	}
	m.wardenTarget, m.sonicRun, m.sonicCD = t.p.eid, 0, wardenSonicFreshUpd
}

// wardenFight is WardenAi's FIGHT activity for one update: the sonic boom
// (listed first, so it wins a tie) and the 18-tick melee.
func (h *hub) wardenFight(players map[int32]*tracked, m *mob, t *tracked) {
	if m.sonicCD > 0 {
		m.sonicCD--
	}
	dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
	inBoomRange := dx*dx+dz*dz < wardenSonicR*wardenSonicR && dy*dy < wardenSonicRY*wardenSonicRY
	if m.sonicRun > 0 {
		// SonicBoom is running: it stares the target down, the blast goes off
		// 34 ticks in (if the target is still in reach), and at 60 the
		// behaviour stops and sets its cooldown. No melee meanwhile.
		m.sonicRun++
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		m.headYaw = m.yaw
		if m.sonicRun == wardenSonicHitUpd+1 && inBoomRange {
			h.wardenSonicBoom(players, m, t)
		}
		if m.sonicRun > wardenSonicRunUpd {
			m.sonicRun = 0
			m.sonicCD = wardenSonicCoolUpd
		}
		return
	}
	if m.sonicCD == 0 && inBoomRange {
		// SonicBoom.start: the charge animation (entity event 62) and sound.
		m.sonicRun = 1
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusSonicCharge))
		h.playSoundDim(players, m.dim, "minecraft:entity.warden.sonic_charge", sndHostile, m.x, m.y, m.z, 3, 1)
		return
	}
	h.mobMelee(players, m) // MeleeAttack(18); a blow puts the boom back 40 ticks
}

// wardenSonicBoom fires the piercing shriek: fixed damage that ignores armour
// and shields, plus knockback. It is the blast at the end of the charge, not
// a swing: the arm stays down.
func (h *hub) wardenSonicBoom(players map[int32]*tracked, m *mob, t *tracked) {
	h.playSoundDim(players, m.dim, "minecraft:entity.warden.sonic_boom", sndHostile, m.x, m.y, m.z, 3, 1)
	h.damageOf(players, t, wardenSonicDmg, dtSonicBoom)
	h.wardenSonicKnock(m, t) // SonicBoom's own push: 2.5 along the beam, 0.5 up
	if t.dead {
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
		h.incStat(t, attachproto.StatKilledBy, int32(m.etype), 1)
	}
}
