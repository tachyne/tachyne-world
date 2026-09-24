package server

import "math"

// playerKilledEntity fires PLAYER_KILLED_ENTITY for t's kill of m with the
// facts its criteria test (KilledTrigger.TriggerInstance): the victim's type,
// age, dimension and horizontal distance from the killer, whether it wore the
// ominous banner, and the killing blow's direct entity and damage-type tags.
// Sniper Duel, Return to Sender, Blowback, Uneasy Alliance and Voluntary
// Exile each ask for one of these beyond the victim's type.
func (h *hub) playerKilledEntity(players map[int32]*tracked, t *tracked, m *mob) {
	h.advance(players, t, "player_killed_entity", killMatch(t, m))
}

// killMatch is the trigger payload for t's kill of m. The killing blow is
// the mob's last hurt (hurtOf records its damage type; a projectile hit adds
// the projectile as the direct entity).
func killMatch(t *tracked, m *mob) advMatch {
	km := advMatch{entity: advEntityName[m.etype], baby: m.baby, dim: int32(m.dim),
		distH: math.Hypot(m.x-t.x, m.z-t.z), ominousBanner: m.patrolCaptain,
		damageTags: map[string]bool{}}
	if m.lastDirect != 0 {
		km.damageDirect = advEntityName[m.lastDirect]
	}
	for name, tag := range dmgTagByName {
		if m.lastDT.has(tag) {
			km.damageTags[name] = true
		}
	}
	return km
}

// entityBreezeWindCharge is the breeze's own wind charge, a type of its own
// in vanilla; here it flies as a wind charge marked breezeBorn.
var entityBreezeWindCharge = entityID("breeze_wind_charge")

// windChargeDirect is the entity type a wind charge strikes as: the breeze's
// charge keeps its type when a player bats it back (Blowback asks for it).
func windChargeDirect(a *arrowEntity) int {
	if a.breezeBorn {
		return entityBreezeWindCharge
	}
	return a.etype
}
