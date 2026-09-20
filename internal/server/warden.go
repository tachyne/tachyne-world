package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Warden behaviour on top of generic hostile chase/melee: a Darkness aura, a
// piercing sonic boom, and digging away (despawn) after a spell with no target.
// The Warden is blind — it homes on the nearest huntable player as a stand-in
// for vibration/scent tracking (a shrieker summons it near whoever roused it).
// Full anger management, sniffing, and the emerge/dig animations are deferred.

const (
	wardenDarknessR = 20.0 // players this close get the Darkness effect
	wardenSonicR    = 15.0 // sonic-boom range
	wardenSonicDmg  = 10.0 // sonic boom bypasses armour AND shields
	// SonicBoom runs for DURATION 60 ticks and sets a 40-tick cooldown when it
	// stops, so vanilla booms land 100 ticks apart — not the 60 this used to
	// allow, which made a warden noticeably more punishing than the real one.
	wardenSonicCD = 100 / mobMoveInterval
	// WardenAi.DIGGING_COOLDOWN: 1200 ticks after the last interaction before
	// it burrows away. This counted 300 updates — 600 ticks, half of vanilla —
	// and the comment claiming ~60 s was reading updates as ticks.
	wardenDigAwayUpd = 1200 / mobMoveInterval
)

// wardenTick runs once per mob update (every mobMoveInterval ticks) for a Warden.
func (h *hub) wardenTick(players map[int32]*tracked, m *mob) {
	// Darkness dread for everyone nearby (refreshed so it never lapses in range).
	for _, t := range players {
		if t.dim != m.dim {
			continue
		}
		if dx, dz := t.x-m.x, t.z-m.z; dx*dx+dz*dz < wardenDarknessR*wardenDarknessR {
			h.applyEffect(players, t, effDarkness, 0, 12)
		}
	}

	// Emerging, roaring, sniffing or burrowing: the warden is rooted to the
	// spot for the length of the animation and does nothing else.
	if h.wardenPoseTick(players, m) {
		return
	}

	// AngerManagement: it goes for whoever it is angriest at, and only falls
	// back on what it can sense nearby when nobody has provoked it.
	t := h.wardenAngerTickOne(players, m)
	if t == nil {
		// Nothing has provoked it. Vanilla keeps "somebody is nearby"
		// (NEAREST_ATTACKABLE) apart from "I am coming for you"
		// (ATTACK_TARGET), and sniffs the air in between — so sniff first,
		// and only then fall back on hunting whoever is closest.
		near := h.nearestHuntable(players, m.dim, m.x, m.z, 24)
		if near != nil && h.wardenSniffTry(players, m) {
			return
		}
		t = near
	}
	if t == nil {
		m.wardenTarget = 0
		if m.digClock++; m.digClock >= wardenDigAwayUpd {
			// Digging: it burrows for a hundred ticks and is then DISCARDED —
			// not a death, so no loot and no experience. Caught off the ground
			// it just grumbles and goes (Digging.checkExtraStartConditions).
			if !m.grounded() {
				h.playSound(players, "minecraft:entity.warden.agitated", sndHostile, m.x, m.y, m.z, 5, 1)
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
	// target when the roar ends.
	if m.wardenTarget != t.p.eid {
		m.wardenTarget = t.p.eid
		h.wardenAngerAt(m, t.p.eid, 20) // Roar.ROAR_ANGER_INCREASE
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
		h.wardenPoseStart(players, m, poseRoaring, wardenRoarUpd, "", 0)
		return
	}
	if m.sonicCD > 0 {
		m.sonicCD--
		return
	}
	dx, dz := t.x-m.x, t.z-m.z
	if d2 := dx*dx + dz*dz; d2 > 4 && d2 < wardenSonicR*wardenSonicR { // 2..15 blocks
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		m.sonicCD = wardenSonicCD
		h.wardenSonicBoom(players, m, t)
	}
}

// wardenSonicBoom fires the piercing shriek: fixed damage that ignores armour
// and shields, plus knockback.
func (h *hub) wardenSonicBoom(players map[int32]*tracked, m *mob, t *tracked) {
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSound(players, "minecraft:entity.warden.sonic_boom", sndHostile, m.x, m.y, m.z, 3, 1)
	h.damageOf(players, t, wardenSonicDmg, dtSonicBoom)
	h.wardenSonicKnock(m, t) // SonicBoom's own push: 2.5 along the beam, 0.5 up
	if t.dead {
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
		h.incStat(t, attachproto.StatKilledBy, int32(m.etype), 1)
	}
}
