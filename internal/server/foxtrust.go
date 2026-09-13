package server

// Fox trust (Fox.addTrustedEntity, the breed goal, DefendTrustedTargetGoal):
// a cub born of two fed foxes trusts whoever fed each parent; a fox never
// runs from a player it trusts, and goes for whatever last hurt one of
// them within sixteen blocks — a defending fox, which also stops running
// from wolves.

const foxDefendRange = 16.0

// foxTrusts reports a trusted player (by name: it survives relogs and
// restarts, as vanilla's UUID list does).
func foxTrusts(m *mob, t *tracked) bool {
	if t == nil {
		return false
	}
	return m.trusted[0] == t.p.name || m.trusted[1] == t.p.name
}

// foxAddTrusted is addTrustedEntity: two slots, the older forgotten.
func foxAddTrusted(m *mob, name string) {
	if name == "" || m.trusted[0] == name || m.trusted[1] == name {
		return
	}
	if m.trusted[0] == "" {
		m.trusted[0] = name
		return
	}
	m.trusted[1] = name
}

// avoidPlayerExempt is the per-player exemption for AvoidEntityGoal<Player>:
// a fox and one it trusts.
func avoidPlayerExempt(m *mob, t *tracked) bool {
	return m.etype == entityFox && foxTrusts(m, t)
}

// foxDefendTarget is DefendTrustedTargetGoal: whatever last hurt a trusted
// player within sixteen, if it is alive and within sixteen itself.
func (h *hub) foxDefendTarget(players map[int32]*tracked, m *mob) *mob {
	for _, t := range players {
		if !foxTrusts(m, t) || t.dim != m.dim || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > foxDefendRange {
			continue
		}
		if o := h.mobs[t.lastHurtByMob]; o != nil && o != m && o.dying == 0 && o.etype != entityFox &&
			dist3(o.x, o.y, o.z, m.x, m.y, m.z) <= foxDefendRange {
			return o
		}
	}
	return nil
}

// trustedList is the persisted form: the names it holds, in order.
func trustedList(m *mob) []string {
	var out []string
	for _, n := range m.trusted {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}
