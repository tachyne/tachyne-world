package server

import "github.com/tachyne/tachyne-common/protocol"

// The drawn bow (LivingEntity's hand-active flag, RangedBowAttackGoal's
// startUsingItem): a skeleton, stray, bogged or illusioner draws its bow
// for the twenty ticks before each shot — the client shows the pull —
// and a pillager's hand is busy while its crossbow loads.

const (
	metaIndexLivingFlags = 8  // LivingEntity DATA_LIVING_ENTITY_FLAGS (byte; stable across versions)
	livingFlagUsing      = 1  // LIVING_ENTITY_FLAG_IS_USING
	bowDrawUpdates       = 10 // twenty ticks of pull before the release
)

func livingFlagsMeta(eid int32, using bool) []byte {
	var f byte
	if using {
		f = livingFlagUsing
	}
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexLivingFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, f)
	return protocol.AppendU8(b, itemMetaEnd)
}

// setHandActive flips the flag on change.
func (h *hub) setHandActive(players map[int32]*tracked, m *mob, on bool) {
	if m.handActive == on {
		return
	}
	m.handActive = on
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(livingFlagsMeta(m.eid, on)))
}

// bowDrawTick keeps a bow-user's pull in step with its shot clock: drawn
// while a target is in range and the shot is twenty ticks or less away.
func (h *hub) bowDrawTick(players map[int32]*tracked, m *mob) {
	drawing := m.attackCD <= bowDrawUpdates && h.nearestHuntable(players, m.dim, m.x, m.z, shootRange) != nil
	h.setHandActive(players, m, drawing)
}
