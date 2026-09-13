package server

import "math"

// Polar bears (PolarBearAttackPlayersGoal, PolarBearHurtByTargetGoal,
// PolarBearMeleeAttackGoal): an adult with a cub within eight blocks turns
// on any player within ten; a hit bear rouses every adult bear around it
// (a hit cub only rouses the others and does not fight); and a bear about
// to bite rears up on its hind legs with a warning growl, dropping again
// as the bite lands or the target draws off.

const (
	metaIndexBearStanding = 17 // DATA_STANDING_ID (1.21.5; an Animal, so 18 on 26.2)
	bearCubRangeXZ        = 8.0
	bearCubRangeY         = 4.0
	bearGuardRange        = 10.0 // NearestAttackableTargetGoal follow distance × 0.5
	bearStandReach        = 3.0  // within (width + 3) of the target
	bearStandCD           = 10   // getTicksUntilNextAttack <= 10
)

func bearStandingMeta(m *mob) []byte { return boolMeta(m.eid, metaIndexBearStanding, m.bearStanding) }

func (h *hub) setBearStanding(players map[int32]*tracked, m *mob, on bool) {
	if m.bearStanding == on {
		return
	}
	m.bearStanding = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(bearStandingMeta(m)))
	if on {
		h.playSoundDim(players, m.dim, "minecraft:entity.polar_bear.warning", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// bearHasCubNear is the attack-players goal's check.
func (h *hub) bearHasCubNear(m *mob) bool {
	found := false
	h.grid().nearby(m.dim, m.x, m.z, bearCubRangeXZ, func(o *mob) {
		if o != m && o.etype == entityPolarBear && o.baby && o.dying == 0 && math.Abs(o.y-m.y) <= bearCubRangeY &&
			math.Abs(o.x-m.x) <= bearCubRangeXZ && math.Abs(o.z-m.z) <= bearCubRangeXZ {
			found = true
		}
	})
	return found
}

// polarBearStep runs each mob update; it never holds the bear (the hunt
// and the bite are the ordinary hostile paths once provoked).
func (h *hub) polarBearStep(players map[int32]*tracked, m *mob) {
	// A cub in sight makes any player within ten a target.
	if !m.hostile && !m.baby && h.bearHasCubNear(m) {
		if t := h.nearestHuntable(players, m.dim, m.x, m.z, bearGuardRange); t != nil {
			h.provoke(m, t)
		}
	}
	if !m.hostile || !m.hasTarget {
		h.setBearStanding(players, m, false)
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange())
	if t == nil {
		h.setBearStanding(players, m, false)
		return
	}
	reach := 0.6 + bearStandReach // the player's width plus three
	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) < reach*reach {
		if m.attackCD*mobMoveInterval <= bearStandCD {
			h.setBearStanding(players, m, true) // rearing up, the warning
		}
		return
	}
	h.setBearStanding(players, m, false)
}
