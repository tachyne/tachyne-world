package server

import "github.com/tachyne/tachyne-common/protocol"

// Spellcasting illagers (SpellcasterIllager + Illusioner's two spells): a
// spell is announced with its prepare sound and the casting arms (the
// synced spell id), lands after a twenty-tick warm-up with the cast
// sound, and the arms drop when the casting time runs out. The
// illusioner's mirror spell makes it invisible for a minute (the client
// draws its four illusions from that) every three hundred and forty
// ticks; its blindness spell, on hard only and never twice on the same
// target, blinds its target for twenty seconds every hundred and eighty.
// Between spells it shoots its bow.

const (
	metaIndexSpell = 17 // SpellcasterIllager DATA_SPELL_CASTING_ID (byte; Raider's celebrating is 16)
	spellNone      = 0
	spellSummonVex = 1
	spellFangs     = 2
	spellDisappear = 4
	spellBlindness = 5
	spellWarmup    = 20  // SpellcasterUseSpellGoal.getCastWarmupTime
	spellCastAnim  = 20  // getCastingTime for both illusioner spells
	mirrorInterval = 340 // IllusionerMirrorSpellGoal.getCastingInterval
	mirrorTicks    = 1200
	blindInterval  = 180
	blindTicks     = 400
	fangsCastAnim  = 40  // EvokerAttackSpellGoal.getCastingTime
	summonCastAnim = 100 // EvokerSummonSpellGoal.getCastingTime
)

func spellMeta(eid int32, spell int8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexSpell)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, byte(spell))
	return protocol.AppendU8(b, itemMetaEnd)
}

// setSpell raises the casting arms for the spell, for ticks.
func (h *hub) setSpell(players map[int32]*tracked, m *mob, spell int8, ticks int) {
	m.castLeft = ticks
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(spellMeta(m.eid, spell)))
}

// spellAnimTick drops the arms when the casting time is up.
func (h *hub) spellAnimTick(players map[int32]*tracked, m *mob) {
	if m.castLeft <= 0 {
		return
	}
	m.castLeft -= mobMoveInterval
	if m.castLeft <= 0 {
		m.castLeft = 0
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(spellMeta(m.eid, spellNone)))
	}
}

// illusionerTick runs each mob update: a spell warming up, a new one, or
// the bow.
func (h *hub) illusionerTick(players map[int32]*tracked, m *mob) {
	h.spellAnimTick(players, m)
	now := h.tick.Load()
	if m.illWarmup > 0 {
		m.vx, m.vz = 0, 0 // SpellcasterCastingSpellGoal: it stands to cast
		m.illWarmup -= mobMoveInterval
		if m.illWarmup > 0 {
			return
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.illusioner.cast_spell", sndHostile, m.x, m.y, m.z, 1, 1)
		switch m.illSpell {
		case spellDisappear:
			h.applyMobEffect(players, m, effInvisibility, 0, mirrorTicks/20)
		case spellBlindness:
			if t := players[m.illBlindLast]; t != nil && !t.dead && t.dim == m.dim {
				h.applyEffect(players, t, effBlindness, 0, blindTicks/20)
			}
		}
		m.illSpell = spellNone
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange())
	if t != nil && m.castLeft == 0 {
		switch {
		case now >= m.illMirrorNext && m.hasEffect(effInvisibility) == 0:
			m.illSpell, m.illWarmup, m.illMirrorNext = spellDisappear, spellWarmup, now+mirrorInterval
			h.setSpell(players, m, spellDisappear, spellCastAnim)
			h.playSoundDim(players, m.dim, "minecraft:entity.illusioner.prepare_mirror", sndHostile, m.x, m.y, m.z, 1, 1)
			return
		case now >= m.illBlindNext && t.p.eid != m.illBlindLast && h.rules.Difficulty > diffNormal:
			m.illSpell, m.illWarmup, m.illBlindNext, m.illBlindLast = spellBlindness, spellWarmup, now+blindInterval, t.p.eid
			h.setSpell(players, m, spellBlindness, spellCastAnim)
			h.playSoundDim(players, m.dim, "minecraft:entity.illusioner.prepare_blindness", sndHostile, m.x, m.y, m.z, 1, 1)
			return
		}
	}
	h.skeletonShoot(players, m) // RangedBowAttackGoal
}
