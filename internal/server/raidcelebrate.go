package server

// Raider.RaiderCelebration: once a raid is lost — the village it came for
// is gone — every raider of it with nothing to fight stands and cheers.
// The IS_CELEBRATING flag puts the illagers' arms up (a ravager and a witch
// have no pose for it), the goal's tick jumps it now and then unless it is
// riding, and sounds its celebration. The goal ticks every other tick, the
// same cadence as the mob update, so its adjustedTickDelay(100) and
// adjustedTickDelay(50) are one-in-fifty and one-in-twenty-five a turn.

const (
	metaIndexRaiderCelebrating = 16 // Raider IS_CELEBRATING (bool)
	celebrateSoundOdds         = 50 // nextInt(adjustedTickDelay(100))
	celebrateJumpOdds          = 25 // nextInt(adjustedTickDelay(50))
	celebrateJumpPower         = 0.42
)

// raiderCelebrateSound is Raider.getCelebrateSound per species.
func raiderCelebrateSound(etype int) string {
	switch etype {
	case entityPillager:
		return "minecraft:entity.pillager.celebrate"
	case entityVindicator:
		return "minecraft:entity.vindicator.celebrate"
	case entityEvoker:
		return "minecraft:entity.evoker.celebrate"
	case entityIllusioner:
		return "minecraft:entity.illusioner.ambient"
	case entityRavager:
		return "minecraft:entity.ravager.celebrate"
	case entityWitch:
		return "minecraft:entity.witch.celebrate"
	}
	return ""
}

// updateCelebration runs with the mob step: RaiderCelebration.canUse is a
// living raider with no target whose raid is a loss, and its stop clears the
// flag the moment any of that stops holding.
func (h *hub) updateCelebration(players map[int32]*tracked, m *mob) {
	on := false
	if isRaider(m) && !m.hasTarget && m.dying == 0 {
		if r := h.raids[m.raidCenter]; r != nil && r.lostLeft > 0 {
			on = true
		}
	}
	if on == m.celebrating {
		return
	}
	m.celebrating = on
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexRaiderCelebrating, on)))
}

// celebrateStep is RaiderCelebration.tick: it holds the raider's movement
// (the goal takes MOVE, and nothing else walks it), cheers and jumps.
func (h *hub) celebrateStep(players map[int32]*tracked, m *mob) bool {
	if h.rng.Intn(celebrateSoundOdds) == 0 {
		if s := raiderCelebrateSound(m.etype); s != "" {
			h.playSoundDim(players, m.dim, s, sndHostile, m.x, m.y, m.z, 1, h.voicePitch(m))
		}
	}
	if m.mount == 0 && !m.leaping && h.rng.Intn(celebrateJumpOdds) == 0 {
		m.leaping, m.leapVX, m.leapVY, m.leapVZ = true, 0, celebrateJumpPower, 0
	}
	m.vx, m.vz = 0, 0
	return true
}
