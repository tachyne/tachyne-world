package server

import "math"

// The Warden's four set-piece animations (WardenAi's EMERGE, ROAR, SNIFF and
// DIG activities). Each is a pose the client renders and a span the warden
// spends rooted to the spot: it rises out of the ground when a shrieker calls
// it, roars before it comes for you, casts about when it has lost you, and
// burrows away when it has been left alone long enough. Without them a warden
// simply appeared, chased, and blinked out of existence.

// Pose ids (Pose.java). poseSniffing/poseDigging come from sniffer.go.
const (
	poseRoaring  = 11
	poseEmerging = 13
)

// Durations, in mob updates (these count in TICKS in vanilla; the warden is
// stepped once every mobMoveInterval).
const (
	wardenEmergeUpd = 134 / mobMoveInterval // WardenAi.EMERGE_DURATION (133.6, rounded up)
	wardenRoarUpd   = 84 / mobMoveInterval  // ROAR_DURATION
	wardenSniffUpd  = 84 / mobMoveInterval  // SNIFFING_DURATION (83.2, rounded up)
	wardenDigUpd    = 100 / mobMoveInterval // DIGGING_DURATION
	// Roar.TICKS_BEFORE_PLAYING_ROAR_SOUND: the sound lands a quarter of the
	// way in, not at the start.
	wardenRoarSoundUpd = 25 / mobMoveInterval
	// TryToSniff.SNIFF_COOLDOWN, uniform 100..200.
	wardenSniffCDMin  = 100 / mobMoveInterval
	wardenSniffCDSpan = 100 / mobMoveInterval
	// Sniffing.stop: what it finds within this range makes it angry.
	wardenSniffFindXZ = 6.0
	wardenSniffFindY  = 20.0
)

// wardenPoseStart puts the warden into a pose for the given span.
func (h *hub) wardenPoseStart(players map[int32]*tracked, m *mob, pose int32, upd int, sound string, vol float32) {
	m.wardenPose, m.wardenPoseLeft = pose, upd
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, pose)))
	if sound != "" {
		h.playSoundDim(players, m.dim, sound, sndHostile, m.x, m.y, m.z, vol, 1)
	}
}

// wardenStand returns the warden to the standing pose.
func (h *hub) wardenStand(players map[int32]*tracked, m *mob) {
	m.wardenPose, m.wardenPoseLeft = 0, 0
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
}

// wardenEmerge is Warden.finalizeSpawn's TRIGGERED branch: a warden a shrieker
// called rises out of the ground before it does anything else.
func (h *hub) wardenEmerge(players map[int32]*tracked, m *mob) {
	h.playSoundDim(players, m.dim, "minecraft:entity.warden.agitated", sndHostile, m.x, m.y, m.z, 5, 1)
	h.wardenPoseStart(players, m, poseEmerging, wardenEmergeUpd, "minecraft:entity.warden.emerge", 5)
}

// wardenPoseTick advances whatever animation is playing. Reports whether the
// warden is mid-animation, in which case it neither moves nor attacks.
func (h *hub) wardenPoseTick(players map[int32]*tracked, m *mob) bool {
	if m.wardenPoseLeft <= 0 {
		return false
	}
	m.wardenPoseLeft--
	if m.wardenPose == poseRoaring && m.wardenPoseLeft == wardenRoarUpd-wardenRoarSoundUpd {
		h.playSoundDim(players, m.dim, "minecraft:entity.warden.roar", sndHostile, m.x, m.y, m.z, 3, 1)
	}
	if m.wardenPoseLeft > 0 {
		return true
	}
	switch m.wardenPose {
	case poseDigging:
		// Digging.stop: RemovalReason.DISCARDED — not a death, so no loot and
		// no experience.
		h.removeMob(players, m)
		return true
	case poseSniffing:
		// Sniffing.stop: whatever it turned up close by is what it gets angry
		// at. The engine has no NEAREST_ATTACKABLE memory, so it looks now.
		if t := h.nearestHuntable(players, m.dim, m.x, m.z, wardenSniffFindXZ); t != nil &&
			math.Abs(t.y-m.y) <= wardenSniffFindY {
			h.wardenAngerAt(m, t.p.eid, wardenAngerHeard) // increaseAngerAt: DEFAULT_ANGER
		}
	}
	h.wardenStand(players, m)
	return true
}

// wardenStep holds a warden still while an animation plays. Reports whether it
// took the mob's movement this tick.
func (h *hub) wardenStep(players map[int32]*tracked, m *mob) bool {
	if m.wardenPoseLeft <= 0 {
		return h.wardenInvestigateStep(m) // INVESTIGATE: off to what it heard
	}
	m.vx, m.vz = 0, 0
	return true
}

// wardenSniffTry is TryToSniff: with somebody in sight but nothing it is
// angry at, the warden casts about for a scent on its own cooldown. The
// caller has already established that there is someone to smell
// (NEAREST_ATTACKABLE present). Reports whether it started sniffing.
func (h *hub) wardenSniffTry(players map[int32]*tracked, m *mob) bool {
	if m.wardenSniffCD > 0 {
		m.wardenSniffCD--
		return false
	}
	m.wardenSniffCD = wardenSniffCDMin + h.rng.Intn(wardenSniffCDSpan+1)
	h.wardenPoseStart(players, m, poseSniffing, wardenSniffUpd, "minecraft:entity.warden.sniff", 5)
	return true
}
