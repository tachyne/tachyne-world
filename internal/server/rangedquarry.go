package server

// Who a ranged attacker shoots at. Vanilla's target goals pick one LivingEntity
// (a player first, then the classes further down: a skeleton's baby turtles
// and iron golems, a raider's villagers), and every attack goal then aims at
// that target. The engine's ranged paths looked only for players, so a
// pillager walked up to a villager, a guardian to a squid, and stood there.

// quarry is a ranged attacker's target: a player, or the creature its target
// goal picked (preyTarget).
type quarry struct {
	t       *tracked
	o       *mob
	x, y, z float64 // feet
	aimY    float64 // getY(1/3): where a projectile is aimed
}

// rangedQuarry is the nearest huntable player within r, or failing one the
// mob's prey if it is still valid and within r.
func (h *hub) rangedQuarry(players map[int32]*tracked, m *mob, r float64) (quarry, bool) {
	if t := h.nearestTargetable(players, m, r); t != nil {
		return quarry{t: t, x: t.x, y: t.y, z: t.z, aimY: t.y + 0.6}, true // a player is 1.8 tall
	}
	if m.preyTarget == 0 {
		return quarry{}, false
	}
	o := h.mobs[m.preyTarget]
	if o == nil || o.dying > 0 || !h.preyOf(m, o) || dist3(o.x, o.y, o.z, m.x, m.y, m.z) > r {
		return quarry{}, false
	}
	// Eye height is 0.85 of the body by default (EntityDimensions), so a
	// third of the body is eye/0.85/3.
	return quarry{o: o, x: o.x, y: o.y, z: o.z, aimY: o.y + mobEyeHeight(o)/0.85/3}, true
}

// eid is the quarry's entity id.
func (q quarry) eid() int32 {
	if q.t != nil {
		return q.t.p.eid
	}
	return q.o.eid
}

// seesQuarry is the line-of-sight test, with a bow's see-time bookkeeping.
func (h *hub) seesQuarry(m *mob, q quarry, bow bool) bool {
	if q.t != nil {
		return h.seeTimeTick(m, q.t, bow)
	}
	return h.seeTimeTickLOS(m, h.mobSeesMob(m, q.o), bow)
}
