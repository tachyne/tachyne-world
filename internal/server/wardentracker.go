package server

// WardenSpawnTracker: the counter that decides when a shrieker calls a
// Warden. It lives on the PLAYER, not on the shrieker — every shrieker in
// the deep dark reads the same tally, so four shrieks anywhere in a city
// summon one, and a player who has been warned carries that warning with
// them. Nearby players share whichever tally is highest, a warning cannot be
// raised twice within ten seconds, and one level fades every ten minutes
// without a warning.

const (
	wardenWarnMax      = 4     // MAX_WARNING_LEVEL
	wardenWarnCooldown = 200   // WARNING_LEVEL_INCREASE_COOLDOWN
	wardenWarnDecay    = 12000 // DECREASE_WARNING_LEVEL_EVERY_INTERVAL
	wardenWarnShare    = 16.0  // PLAYER_SEARCH_RADIUS: who shares the tally
	wardenWarnCheck    = 48.0  // WARNING_CHECK_DIAMETER: a Warden already about
	wardenDarknessSecs = 13    // MobEffectInstance(DARKNESS, 260 ticks)
	wardenDarknessKeep = 200   // …only refreshed when less than this is left
	wardenDarknessRing = 40.0  // applyDarknessAround's radius on a response
)

// tickWardenTrackers runs on the one-second survival cadence: the cooldown
// counts down and a long quiet spell walks the warning level back.
func (h *hub) tickWardenTrackers(players map[int32]*tracked) {
	for _, t := range players {
		if t.wardenSince >= wardenWarnDecay {
			t.wardenSince = 0
			if t.wardenWarn > 0 {
				t.wardenWarn--
			}
		} else {
			t.wardenSince += survivalTickN
		}
		if t.wardenCool > 0 {
			t.wardenCool -= survivalTickN
			if t.wardenCool < 0 {
				t.wardenCool = 0
			}
		}
	}
}

// tryWarnWarden is WardenSpawnTracker.tryWarn: the shrieker asks the players
// around it to take a warning. Returns the new warning level and whether the
// warning landed at all (a Warden already nearby, or anyone still on
// cooldown, and the shrieker shrieks for nothing).
func (h *hub) tryWarnWarden(players map[int32]*tracked, pos blockPos, dim int, by int32) (int, bool) {
	for _, m := range h.mobs {
		if m.etype == entityWarden && m.dim == dim &&
			dist3(m.x, m.y, m.z, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5) < wardenWarnCheck {
			return 0, false // one Warden about is enough
		}
	}
	var near []*tracked
	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if t.p.eid == by || dist3(t.x, t.y, t.z, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5) <= wardenWarnShare {
			near = append(near, t)
		}
	}
	if len(near) == 0 {
		return 0, false
	}
	var best *tracked
	for _, t := range near {
		if t.wardenCool > 0 {
			return 0, false // somebody here was warned moments ago
		}
		if best == nil || t.wardenWarn > best.wardenWarn {
			best = t
		}
	}
	best.wardenSince, best.wardenCool = 0, wardenWarnCooldown
	if best.wardenWarn < wardenWarnMax {
		best.wardenWarn++
	}
	// copyData: everyone who heard it carries the same tally away.
	for _, t := range near {
		t.wardenWarn, t.wardenCool, t.wardenSince = best.wardenWarn, best.wardenCool, best.wardenSince
	}
	return best.wardenWarn, true
}

// darknessAround is Warden.applyDarknessAround: the dread that falls on
// everyone near a responding shrieker (and on the Warden's own arrival).
func (h *hub) darknessAround(players map[int32]*tracked, dim int, x, y, z float64, radius float64) {
	for _, t := range players {
		if t.dim != dim || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			continue
		}
		if dist3(t.x, t.y, t.z, x, y, z) > radius {
			continue
		}
		if e, on := t.effects[effDarkness]; on && e.left > wardenDarknessKeep {
			continue // MobEffectUtil: a fresh one is left alone
		}
		h.applyEffect(players, t, effDarkness, 0, wardenDarknessSecs)
	}
}
