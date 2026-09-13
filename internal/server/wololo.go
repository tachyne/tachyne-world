package server

// Wololo. An evoker with nobody to fight looks for a blue sheep within
// sixteen blocks (four up or down) and, given mob griefing, turns it red:
// forty ticks of warm-up under the wololo casting arms, then the sheep's
// fleece changes, and a hundred and forty ticks before it tries again.
// That is vanilla's EvokerWololoSpellGoal, its oldest joke, and the reason
// a blue sheep near a woodland mansion does not stay blue.

const (
	spellWololo    = 3   // SpellcasterIllager.IllagerSpell.WOLOLO
	wololoWarmup   = 40  // getCastWarmupTime
	wololoCastAnim = 60  // getCastingTime
	wololoInterval = 140 // getCastingInterval
	wololoRange    = 16.0
	wololoRangeY   = 4.0
	fleeceBlue     = 11 // DyeColor.BLUE in registry order
	fleeceRed      = 14 // DyeColor.RED
)

// wololoTick runs the spell's warm-up (a spell already begun lands even if
// a fight starts meanwhile, as vanilla's does), and returns whether one is
// in progress.
func (h *hub) wololoTick(players map[int32]*tracked, m *mob) bool {
	if m.wololoWarm <= 0 {
		return false
	}
	m.wololoWarm -= mobMoveInterval
	if m.wololoWarm > 0 {
		return true
	}
	m.wololoWarm = 0
	if s := h.mobs[m.wololoTarget]; s != nil && s.etype == entitySheep && s.dying == 0 && s.color != fleeceRed {
		s.color = fleeceRed
		h.toNearbyEv(players, s.dim, s.x, s.z, metaEv(sheepFleeceMeta(s.eid, s.color, s.sheared)))
	}
	m.wololoTarget = 0
	return false
}

// wololoStart is the goal's canUse + start: no target, not casting, off
// cooldown, mob griefing on, and a blue sheep in range picked at random.
func (h *hub) wololoStart(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if m.castLeft > 0 || now < m.wololoNextAt || !h.rules.MobGriefing {
		return false
	}
	var sheep []*mob
	h.grid().nearby(m.dim, m.x, m.z, wololoRange, func(o *mob) {
		if o.etype != entitySheep || o.color != fleeceBlue || o.dying > 0 {
			return
		}
		dy := o.y - m.y
		if dy < -wololoRangeY || dy > wololoRangeY {
			return
		}
		sheep = append(sheep, o)
	})
	if len(sheep) == 0 {
		return false
	}
	s := sheep[h.rng.Intn(len(sheep))]
	m.wololoTarget, m.wololoWarm, m.wololoNextAt = s.eid, wololoWarmup, now+wololoInterval
	h.playSoundDim(players, m.dim, "minecraft:entity.evoker.prepare_wololo", sndHostile, m.x, m.y, m.z, 1, 1)
	h.setSpell(players, m, spellWololo, wololoCastAnim)
	return true
}
