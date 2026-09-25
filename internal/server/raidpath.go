package server

import "math"

// PathfindToRaidGoal (Raider): a raider that belongs to a live raid and has
// nothing to fight walks toward the raid's centre, recruiting any idle raider
// it passes on the way. Without it a raider that spawned wide of the village,
// or wandered off after losing its target, never came back — and since a wave
// only advances when every raider of it is dead, one stray could stall the
// raid until the no-player timeout killed it.

const (
	raidRecruitEvery = 20 // RECRUITMENT_SEARCH_TICK_DELAY
	raidRecruitRange = 16 // getBoundingBox().inflate(16)
	// ServerLevel.isVillage(blockPosition()) is what stops the goal: the
	// raider has arrived. The engine has no POI store, so "in the village"
	// is the raid centre's own radius.
	raidArrived = 24
)

// isRaider is Raider: the illagers, the ravager and the witch, once one
// belongs to a raid.
func isRaider(m *mob) bool {
	return (isIllager(m.etype) || m.etype == entityRavager || m.etype == entityWitch) &&
		m.raidCenter != (blockPos{})
}

// raidPathStep steers a raider back to its raid. Reports whether it took the
// mob's movement for this tick.
func (h *hub) raidPathStep(players map[int32]*tracked, m *mob) bool {
	if !isRaider(m) || m.hasTarget || m.mount != 0 || m.rider != 0 {
		return false // a passenger is steered by its mount; a hunter hunts
	}
	r := h.raids[m.raidCenter]
	if r == nil {
		return false // the raid is over: the goal stops (canContinueToUse)
	}
	c := m.raidCenter
	if math.Hypot(float64(c.x)+0.5-m.x, float64(c.z)+0.5-m.z) <= raidArrived {
		return false // isVillage: arrived, let the ordinary goals run
	}
	if now := h.tick.Load(); now >= m.raidRecruitAt {
		m.raidRecruitAt = now + raidRecruitEvery
		h.raidRecruitNearby(r, m)
	}
	m.vx, m.vz = h.pathSteer(m, float64(c.x)+0.5, float64(c.z)+0.5)
	return true
}

// raidRecruitNearby is PathfindToRaidGoal.recruitNearby: idle raiders within
// sixteen blocks join the raid this one is walking to, counted into the wave
// that is already out so the bar and the wave-cleared check stay honest.
func (h *hub) raidRecruitNearby(r *raid, m *mob) {
	for _, o := range h.mobs {
		if o == m || o.dim != m.dim || o.dying > 0 || o.raidCenter != (blockPos{}) {
			continue
		}
		// Raider.finalizeSpawn sets canJoinRaid for every raider EXCEPT a
		// naturally-spawned witch, and the engine cannot tell a swamp witch
		// from a raid one after the fact — so witches are left out and
		// everything else in reach is fair game, outpost garrison included,
		// exactly as vanilla recruits.
		if !isIllager(o.etype) && o.etype != entityRavager {
			continue
		}
		if math.Abs(o.x-m.x) > raidRecruitRange ||
			math.Abs(o.y-m.y) > raidRecruitRange || math.Abs(o.z-m.z) > raidRecruitRange {
			continue
		}
		o.raidCenter, o.raidWave = r.center, r.wave
		r.alive[o.eid] = true
	}
}
