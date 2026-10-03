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
//
// mod is the engine goal's own modifier (for a villager, against its brain's
// 0.5); the speed is what wantedMove makes of it, through the species' land
// move control.
func walkPerUpdate(m *mob, mod float64) float64 {
	s := m.moveSpeed() * mod / (attrToStep * stepScaleFor(m.etype)) * walkBaseMod(m.etype)
	switch m.etype {
	case entityFrog:
		s *= 0.1 // SmoothSwimmingMoveControl's outsideWaterSpeedModifier, facing its way
	case entityTurtle:
		// TurtleMoveControl's fixed point on the ground: s = 7/8·max(s/2,
		// 0.06) + s/8 of the asked speed.
		if f := 0.875*0.06 + 0.125*s; f/2 <= 0.06 {
			s = f
		} else {
			s = 0.125 * s / (1 - 0.4375)
		}
	}
	return mobGoalInterval * mobInputDrag * s * s / (1 - defaultFriction*mobAirDrag)
}
