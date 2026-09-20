package server

// What a player's death does to those after them, by game rule:
// forgive_dead_players calms the neutral mobs angry at them (NeutralMob.
// playerDied → stopBeingAngry when the dead player was the target), and
// ender_pearls_vanish_on_death discards the pearls they had in flight
// (ServerPlayer.die), which would otherwise carry the respawned player off
// when they land.
func (h *hub) deathForgiveness(players map[int32]*tracked, t *tracked) {
	if h.rules.ForgiveDead && !h.rules.UniversalAnger { // universal anger holds the grudge past a death
		for _, m := range h.mobs {
			if m.hostile && m.targetEID == t.p.eid && m.dim == t.dim && neutralMob(m) {
				m.hostile, m.behavior, m.hasTarget = false, Behavior(wanderBehavior{}), false
				m.anger, m.targetEID = 0, 0
			}
		}
	}
	if h.rules.PearlsVanish {
		for eid, a := range h.arrows {
			if a.pearl && a.shooter == t.p.eid {
				delete(h.arrows, eid)
				h.entityGone(players, a.dim, eid)
			}
		}
	}
}

// neutralMob is vanilla's NeutralMob set as the engine spawns it: the
// species that are peaceful until provoked (never the monsters, except
// the two neutral ones among them).
func neutralMob(m *mob) bool {
	switch m.etype {
	case entityZombifiedPiglin, entityEnderman:
		return true
	}
	d := speciesTable[m.etype]
	return d != nil && d.arch != archHostile
}
