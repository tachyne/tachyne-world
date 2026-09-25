package server

import "math"

// The Warden's INVESTIGATE activity and its touch. Something it hears, or
// somebody who bumps into it, becomes a DISTURBANCE_LOCATION for a hundred
// ticks — so long as it is not already angry at anyone or fighting — and
// the warden walks over to it at 0.7 of its pace (GoToTargetLocation, near
// enough within two blocks), sniffing held off meanwhile. A touch
// (Warden.doPush) also raises its grudge against whoever it was, 35 at a
// time and at most once a second, with the listening grumble.

const (
	wardenDisturbTicks    = 100 // DISTURBANCE_LOCATION_EXPIRY_TIME (and the SNIFF_COOLDOWN it sets)
	wardenInvestigateMod  = 0.7 // SPEED_MULTIPLIER_WHEN_INVESTIGATING
	wardenInvestigateNear = 2.0 // GoToTargetLocation closeEnoughDist
	wardenTouchCooldown   = 20  // TOUCH_COOLDOWN
	wardenFightMod        = 1.2 // SPEED_MULTIPLIER_WHEN_FIGHTING
)

// wardenMaxAnger is AngerManagement's active anger: the strongest grudge
// against somebody it can still go after.
func (h *hub) wardenMaxAnger(players map[int32]*tracked, m *mob) (int32, int) {
	var best int32
	bestN := 0
	for eid, n := range m.wardenAnger {
		if t := players[eid]; t == nil || t.dead || t.dim != m.dim || !isSurvival(t.gamemode) {
			continue
		}
		if n > bestN {
			best, bestN = eid, n
		}
	}
	return best, bestN
}

// wardenDisturbed is WardenAi.setDisturbanceLocation: ignored while it is
// angry at somebody or has an attack target.
func (h *hub) wardenDisturbed(players map[int32]*tracked, m *mob, x, y, z float64) {
	if _, n := h.wardenMaxAnger(players, m); n >= wardenAngerAngry || m.wardenTarget != 0 {
		return
	}
	m.digClock = 0 // setDigCooldown
	// GoToTargetLocation walks to a block beside it, one either way at random.
	m.wardenDisturb = blockPos{int(math.Floor(x)) + h.rng.Intn(3) - 1, int(math.Floor(y)), int(math.Floor(z)) + h.rng.Intn(3) - 1}
	m.wardenDisturbTil = h.tick.Load() + wardenDisturbTicks
}

// wardenInvestigating reports a live DISTURBANCE_LOCATION memory.
func (h *hub) wardenInvestigating(m *mob) bool {
	return m.wardenDisturbTil != 0 && h.tick.Load() < m.wardenDisturbTil
}

// wardenInvestigateStep walks the warden to its disturbance. Reports
// whether the activity held its movement.
func (h *hub) wardenInvestigateStep(m *mob) bool {
	if m.hasTarget || !h.wardenInvestigating(m) {
		return false
	}
	gx, gz := float64(m.wardenDisturb.x)+0.5, float64(m.wardenDisturb.z)+0.5
	if dist3(m.x, m.y, m.z, gx, float64(m.wardenDisturb.y), gz) < wardenInvestigateNear {
		m.vx, m.vz = 0, 0 // there: it stands and listens until the memory runs out
		return true
	}
	vx, vz := h.pathSteer(m, gx, gz)
	m.vx, m.vz = vx*wardenInvestigateMod, vz*wardenInvestigateMod
	m.rest = 0
	return true
}

// wardenTouches is Warden.doPush for the players its box is touching.
func (h *hub) wardenTouches(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	if now < m.wardenTouchTil || m.dying != 0 {
		return
	}
	b := m.box()
	for _, t := range players {
		if t.dead || t.dim != m.dim || !isSurvival(t.gamemode) {
			continue
		}
		reach := b.w/2 + playerWidth(t)/2
		if math.Abs(t.x-m.x) >= reach || math.Abs(t.z-m.z) >= reach ||
			t.y >= m.y+b.h || t.y+playerHeight(t) <= m.y {
			continue
		}
		m.wardenTouchTil = now + wardenTouchCooldown
		h.wardenAngerAt(m, t.p.eid, wardenAngerHeard) // increaseAngerAt: DEFAULT_ANGER
		h.wardenListen(players, m)
		h.wardenDisturbed(players, m, t.x, t.y, t.z)
		return
	}
}

// wardenListen is playListeningSound: the grumble, angrier once its anger
// reaches AGITATED, and none while it roars.
func (h *hub) wardenListen(players map[int32]*tracked, m *mob) {
	if m.wardenPoseLeft > 0 && m.wardenPose == poseRoaring {
		return
	}
	snd := "minecraft:entity.warden.listening"
	if _, n := h.wardenMaxAnger(players, m); n >= wardenAngerAgitated {
		snd = "minecraft:entity.warden.listening_angry"
	}
	h.playSoundDim(players, m.dim, snd, sndHostile, m.x, m.y, m.z, 10, h.voicePitch(m))
}
