package server

import "github.com/tachyne/tachyne-common/protocol"

// Mob.setAggressive — the raised arms. Vanilla's ZombieAttackGoal turns the
// flag on when the zombie starts chasing and off when it stops, and every
// client renders the zombie family with its arms up while it is set. Nothing
// else in vanilla uses it, so nothing else sets it here.

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
	if !zombieKind(m.etype) {
		return
	}
	// Only a real chase raises the arms — walking through a village at night
	// is not ZombieAttackGoal.
	h.setAggressive(players, m, m.hasTarget && !m.drifting && !m.drownedGoal && m.dying == 0)
}
