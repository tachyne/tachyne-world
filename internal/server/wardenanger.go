package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// AngerManagement — what a Warden is actually doing when it turns its head.
// It does not simply chase the nearest player: it keeps a grudge per suspect,
// raises it by what it hears (a vibration nearby), by what hits it, and by
// what it is hit with, and goes for whoever it is angriest at. Anger ebbs a
// point every second, so standing still and staying quiet really does make it
// lose interest and burrow away.

const (
	wardenAngerMax      = 150 // AngerManagement.MAX_ANGER
	wardenAngerAngry    = 80  // AngerLevel.ANGRY
	wardenAngerAgitated = 40  // AngerLevel.AGITATED
	wardenAngerHeard    = 35  // Warden.DEFAULT_ANGER: a disturbance it hears
	wardenAngerShot     = 10  // PROJECTILE_ANGER
	wardenAngerHurt     = 100 // ANGRY + ON_HURT_ANGER_BOOST (80 + 20)
	wardenAngerDecay    = 1   // one point per anger tick…
	wardenAngerTick     = 20  // …which runs every second
	wardenHearRange     = 16  // how far a Warden hears a disturbance
	wardenSonicKBHoriz  = 2.5 // SonicBoom's knockback
	wardenSonicKBVert   = 0.5
)

// wardenAngerAt raises this Warden's grudge against a suspect.
func (h *hub) wardenAngerAt(m *mob, suspect int32, by int) {
	if m.etype != entityWarden || suspect == 0 || m.dying != 0 {
		return
	}
	if m.wardenAnger == nil {
		m.wardenAnger = map[int32]int{}
	}
	n := m.wardenAnger[suspect] + by
	if n > wardenAngerMax {
		n = wardenAngerMax
	}
	m.wardenAnger[suspect] = n
	m.digClock = 0 // WardenAi.setDigCooldown: anything it notices resets the burrow clock
}

// wardenHeard is the vibration path: every Warden within sixteen blocks of a
// disturbance takes it personally.
func (h *hub) wardenHeard(dim int, x, y, z float64, src int32) {
	if src == 0 || len(h.mobs) == 0 {
		return
	}
	for _, m := range h.mobs {
		if m.etype != entityWarden || m.dim != dim || m.dying != 0 || m.eid == src {
			continue
		}
		if dist3(m.x, m.y, m.z, x, y, z) > wardenHearRange {
			continue
		}
		// Wool between the sound and the Warden's head hides it.
		if vibOccluded(h.worldFor(dim), floorInt(x), floorInt(y), floorInt(z), floorInt(m.x), floorInt(m.y+mobEyeHeight(m)), floorInt(m.z)) {
			continue
		}
		h.wardenAngerAt(m, src, wardenAngerHeard)
		// onReceiveVibration: not yet angry, it goes to see — unless the
		// grudge it holds is against somebody else.
		if best, n := h.wardenMaxAnger(h.playersRef, m); n < wardenAngerAngry && (best == 0 || best == src) {
			h.wardenDisturbed(h.playersRef, m, x, y, z)
		}
	}
}

// wardenAngerTickOne ages one Warden's grudges and reports the player it is
// angriest at, if that grudge has reached ANGRY.
func (h *hub) wardenAngerTickOne(players map[int32]*tracked, m *mob) *tracked {
	if len(m.wardenAnger) == 0 {
		return nil
	}
	if m.angerClock += mobMoveInterval; m.angerClock >= wardenAngerTick {
		m.angerClock = 0
		for eid, n := range m.wardenAnger {
			if n -= wardenAngerDecay; n <= 1 {
				delete(m.wardenAnger, eid)
				continue
			}
			m.wardenAnger[eid] = n
		}
	}
	var best *tracked
	bestN := wardenAngerAngry - 1
	for eid, n := range m.wardenAnger {
		t := players[eid]
		if t == nil || t.dead || t.dim != m.dim || !isSurvival(t.gamemode) {
			continue
		}
		if n > bestN {
			best, bestN = t, n
		}
	}
	return best
}

// wardenSonicKnock is SonicBoom's push: hard along the beam, a little up.
func (h *hub) wardenSonicKnock(m *mob, t *tracked) {
	dx, dy, dz := t.x-m.x, (t.y+playerEyeStand)-(m.y+1.6), t.z-m.z
	n := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if n < 1e-6 {
		return
	}
	resist := t.playerAttrs().Value(attr.KnockbackResistance)
	hor := wardenSonicKBHoriz * (1 - resist)
	ver := wardenSonicKBVert * (1 - resist)
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: dx / n * hor, VY: dy / n * ver, VZ: dz / n * hor})
	t.spinUntil = h.tick.Load() + windBurstGrace // let the launch past the speed check
}

// metaIndexWardenAnger is Warden.CLIENT_ANGER_LEVEL (INT): after Mob's flags
// (15), with nothing from Monster or PathfinderMob between — the same index
// on 26.2 and 26.3, since a Warden is no AgeableMob.
const metaIndexWardenAnger = 16

func wardenAngerMeta(eid int32, anger int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexWardenAnger)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(anger))
	return protocol.AppendU8(b, itemMetaEnd)
}

// wardenActiveAnger is AngerManagement.getActiveAnger(getTarget()): the
// grudge against the one it has roared at, else its strongest grudge.
func (h *hub) wardenActiveAnger(players map[int32]*tracked, m *mob) int {
	if m.wardenTarget != 0 {
		return m.wardenAnger[m.wardenTarget]
	}
	_, n := h.wardenMaxAnger(players, m)
	return n
}

// wardenSyncAnger is Warden.syncClientAngerLevel: the client drives the
// heartbeat's pace and the tendrils' twitch from CLIENT_ANGER_LEVEL, so it
// is sent whenever it changes.
func (h *hub) wardenSyncAnger(players map[int32]*tracked, m *mob) {
	if h.mobs[m.eid] != m || m.dying != 0 {
		return
	}
	if n := h.wardenActiveAnger(players, m); n != m.wardenClientAnger {
		m.wardenClientAnger = n
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(wardenAngerMeta(m.eid, n)))
	}
}
