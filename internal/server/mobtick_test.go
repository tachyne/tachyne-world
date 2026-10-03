package server

// mobUpdate runs one goal update of every mob: the mobGoalInterval ticks of
// updateMobs it covers. Each mob's goals run on one of those ticks and its
// body moves on every one — what a single updateMobs call did when the mob
// loop itself ran every other tick.
func (h *hub) mobUpdate(players map[int32]*tracked) {
	for i := 0; i < mobGoalInterval; i++ {
		h.updateMobs(players)
	}
}

// walkPerUpdate is how far a walker on an ordinary block covers in one goal
// update once it is up to speed, walking at mod of its MOVEMENT_SPEED:
// travel's steady state, 0.98 × speed² a tick against the ground's 0.546
// drag (speed = the attribute × mod, in vanilla's units).
func walkPerUpdate(m *mob, mod float64) float64 {
	if stepScaleFor(m.etype) != 1 {
		return m.moveSpeed() * mod // a tuned pace is walked at its tuned step
	}
	s := m.moveSpeed() / attrToStep * mod
	return mobGoalInterval * mobInputDrag * s * s / (1 - defaultFriction*mobAirDrag)
}
