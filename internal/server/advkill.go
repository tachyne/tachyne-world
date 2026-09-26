package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// playerKilledEntity fires PLAYER_KILLED_ENTITY for t's kill of m with the
// facts its criteria test (KilledTrigger.TriggerInstance): the victim's type,
// age, dimension and horizontal distance from the killer, whether it wore the
// ominous banner, and the killing blow's direct entity and damage-type tags.
// Sniper Duel, Return to Sender, Blowback, Uneasy Alliance and Voluntary
// Exile each ask for one of these beyond the victim's type.
func (h *hub) playerKilledEntity(players map[int32]*tracked, t *tracked, m *mob) {
	h.advance(players, t, "player_killed_entity", killMatch(t, m))
}

// awardKillScore is ServerPlayer.awardKillScore for a mob t is credited
// with however it died (LivingEntity.die hands the kill to getKillCredit):
// the trigger, mob_kills and the totalKillCount scoreboard criterion.
func (h *hub) awardKillScore(players map[int32]*tracked, t *tracked, m *mob) {
	h.playerKilledEntity(players, t, m)
	h.incCustom(t, "mob_kills", 1)
	h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
}

// hurtByPlayerMemory is how long a mob remembers the player who hurt it
// (resolvePlayerResponsibleForDamage's timeToRemember): a death inside it —
// from a fall, fire, lava or another mob — is still that player's kill.
const hurtByPlayerMemory = 100

// hurtByPlayerOn records a player's hurt on m (resolvePlayerResponsibleForDamage).
func (h *hub) hurtByPlayerOn(m *mob, t *tracked) {
	if t == nil {
		return
	}
	h.hurtByEID(m, t.p.eid)
}

// hurtByEID is hurtByPlayerOn by eid; 0 is a tamed wolf's offline owner, which
// still makes the death a player kill but credits nobody.
func (h *hub) hurtByEID(m *mob, eid int32) {
	m.hitByPlayer = true
	m.hurtByPlayer, m.hurtByPlayerTil = eid, h.tick.Load()+hurtByPlayerMemory
}

// hurtByShot records a projectile's hurt when a player owns it (a
// dispensed arrow has no owner, however it flies).
func (h *hub) hurtByShot(players map[int32]*tracked, m *mob, a *arrowEntity) {
	if a.playerShot {
		h.hurtByPlayerOn(m, players[a.shooter])
	}
}

// hurtByPet records a tamed wolf's bite as its owner's hurt.
func (h *hub) hurtByPet(m, pet *mob) {
	if pet.etype == entityWolf && pet.tamed {
		h.hurtByEID(m, pet.owner)
	}
}

// settleKillCredit is LivingEntity.die's getKillCredit and dropAllDeathLoot
// flag: a mob that dies while it remembers a player's hurt is that
// player's kill, however it died, and drops as one.
func (h *hub) settleKillCredit(players map[int32]*tracked, m *mob) {
	m.hitByPlayer = m.hurtByPlayerTil != 0 && h.tick.Load() < m.hurtByPlayerTil
	if !m.hitByPlayer {
		return
	}
	if t := players[m.hurtByPlayer]; t != nil {
		h.awardKillScore(players, t, m)
		if m.hurtByPlayerTil == h.tick.Load()+hurtByPlayerMemory {
			// Player.killedEntity: the killed statistic is the killing
			// blow's attacker's, and t's hurt landed this very tick.
			h.incStat(t, attachproto.StatKilled, int32(m.etype), 1)
		}
	}
}

// entityPlayerType is the player's own entity type.
var entityPlayerType = entityID("player")

// creditPlayerDeath is ServerPlayer.die's kill credit for a player victim:
// the player it remembers hurting it in the last 100 ticks — by a blow, an
// arrow, a blast or their Thorns — is awarded the kill (awardKillScore; never
// for killing yourself), and the victim's entity_killed_player fires.
func (h *hub) creditPlayerDeath(players map[int32]*tracked, v *tracked) {
	now := h.tick.Load()
	til := v.pvpByTil
	v.pvpByTil = 0 // the respawned player remembers nobody
	if til == 0 || now >= til {
		return
	}
	k := players[v.pvpBy]
	if k == nil || k == v {
		return
	}
	// Player.killedEntity: the killed statistic goes to the killing blow's
	// own attacker, which is k when k's hurt landed this very tick.
	direct := til == now+hurtByPlayerMemory
	h.advance(players, v, "entity_killed_player", advMatch{entity: "player"})
	h.incStat(v, attachproto.StatKilledBy, int32(entityPlayerType), 1)
	if direct {
		h.incStat(k, attachproto.StatKilled, int32(entityPlayerType), 1)
	}
	h.incCustom(k, "player_kills", 1)
	h.sbCriteria(players, "playerKillCount", k.p.name, 1, false)
	h.sbCriteria(players, "totalKillCount", k.p.name, 1, false)
	km := advMatch{entity: "player", dim: int32(v.dim), distH: math.Hypot(v.x-k.x, v.z-k.z),
		damageTags: map[string]bool{}}
	for name, tag := range dmgTagByName {
		if v.lastCause.dt.has(tag) {
			km.damageTags[name] = true
		}
	}
	h.advance(players, k, "player_killed_entity", km)
}

// killMatch is the trigger payload for t's kill of m. The killing blow is
// the mob's last hurt (hurtOf records its damage type; a projectile hit adds
// the projectile as the direct entity).
func killMatch(t *tracked, m *mob) advMatch {
	km := advMatch{entity: advEntityName[m.etype], baby: m.baby, dim: int32(m.dim),
		distH: math.Hypot(m.x-t.x, m.z-t.z), ominousBanner: isOminousBanner(m.gear[0]),
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

// entityBreezeWindCharge is the breeze's own wind charge (BreezeWindCharge),
// a type of its own; it flies marked breezeBorn.
var entityBreezeWindCharge = entityID("breeze_wind_charge")

// windChargeDirect is the entity type a wind charge strikes as: the breeze's
// charge keeps its type when a player bats it back (Blowback asks for it).
func windChargeDirect(a *arrowEntity) int {
	return a.etype
}
