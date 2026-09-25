package server

import "github.com/tachyne/tachyne-common/protocol"

// Mob.setAggressive — the raised arms. Vanilla's attack goals turn the flag
// on when they start and off when they stop: ZombieAttackGoal (the zombie
// family's arms up), RangedBowAttackGoal and the skeleton's MeleeAttackGoal
// (a skeleton's bow held up to aim, a wither skeleton's raised sword, an
// illusioner's drawn bow), RangedCrossbowAttackGoal (the pillager's
// crossbow at the ready) and the vindicator's MeleeAttackGoal (its axe up).

// metaIndexMobFlags is Mob's flags byte: Entity's eight fields, LivingEntity's
// seven, then this. It predates the 26.x AgeableMob insertion, so the index is
// the same on every version we serve.
const (
	metaIndexMobFlags = 15
	mobFlagAggressive = 0x04
)

// mobFlagsMeta builds the flags byte for a mob.
func mobFlagsMeta(eid int32, aggressive bool) []byte {
	var flags byte
	if aggressive {
		flags |= mobFlagAggressive
	}
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexMobFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox) // serializer 0: byte
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

// setAggressive syncs the flag, and only when it changed.
func (h *hub) setAggressive(players map[int32]*tracked, m *mob, on bool) {
	if m.aggressive == on {
		return
	}
	m.aggressive = on
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(mobFlagsMeta(m.eid, on)))
}

// updateAggression runs with the mob step: the zombie family raises its arms
// while it is chasing something and drops them when it gives up.
func (h *hub) updateAggression(players map[int32]*tracked, m *mob) {
	switch {
	case zombieKind(m.etype):
		// Only a real chase raises the arms — walking through a village at
		// night is not ZombieAttackGoal.
		h.setAggressive(players, m, m.hasTarget && !m.drifting && !m.drownedGoal && m.dying == 0)
	case m.etype == entityPillager:
		// RangedCrossbowAttackGoal: a target and a crossbow in hand — the
		// crossbow held up at the ready — once any stand-off is over.
		h.setAggressive(players, m, m.hasTarget && m.held == itemCrossbow && !m.holdingGround && m.dying == 0)
	case m.etype == entityVindicator:
		// MeleeAttackGoal: the axe raised.
		h.setAggressive(players, m, m.hasTarget && !m.holdingGround && m.dying == 0)
	case skeletonKind(m.etype) || m.etype == entityWitherSkeleton || m.etype == entityIllusioner:
		// The bow goal or the melee goal runs for as long as there is a
		// target, whichever weapon the skeleton holds.
		h.setAggressive(players, m, m.hasTarget && m.dying == 0)
	}
}
