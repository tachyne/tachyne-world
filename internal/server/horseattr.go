package server

// Horse variation.
//
// Every horse in tachyne was a clone: the species table gives all of them 22
// health and one speed, and nothing rolled a jump at all. Vanilla randomises
// three attributes per horse, which is the entire reason players breed for a
// good one — a bad horse and a great horse differ by a factor of two in how
// high they jump and how fast they run.
//
// Each figure is the sum of several small independent rolls rather than one
// flat range, so the results cluster around the middle and an exceptional
// horse is genuinely rare.

// horseHealthRoll is 15 + rand(8) + rand(9): 15 to 30.
func (h *hub) horseHealthRoll() int { return 15 + h.rng.Intn(8) + h.rng.Intn(9) }

// horseSpeedRoll is (0.45 + 3 rolls of rand*0.3) x 0.25: about 0.1125 to 0.3375.
func (h *hub) horseSpeedRoll() float64 {
	return (0.45 + h.rng.Float64()*0.3 + h.rng.Float64()*0.3 + h.rng.Float64()*0.3) * 0.25
}

// horseJumpRoll is 0.4 + 3 rolls of rand*0.2: 0.4 to 1.0.
func (h *hub) horseJumpRoll() float64 {
	return 0.4 + h.rng.Float64()*0.2 + h.rng.Float64()*0.2 + h.rng.Float64()*0.2
}

// rollHorseAttributes is AbstractHorse.randomizeAttributes, which only some of
// the family override: a HORSE rolls all three, a SKELETON horse rolls its
// jump alone, and donkeys and mules roll nothing — they are the dependable,
// unexciting members of the family, and that is deliberate in vanilla.
func (h *hub) rollHorseAttributes(m *mob) {
	if m == nil {
		return
	}
	switch m.etype {
	case entityHorse:
		m.setMaxHP(h.horseHealthRoll())
		m.health = m.maxHP()
		m.setMoveSpeed(h.horseSpeedRoll())
		m.jumpStrength = h.horseJumpRoll()
	case entitySkeletonHorse, entityZombieHorse:
		m.jumpStrength = h.horseJumpRoll()
	}
}

// breedHorseAttributes is setOffspringAttribute: a foal lands between its
// parents with a little drift, so breeding two good horses tends to produce a
// better one without ever guaranteeing it.
func (h *hub) breedHorseAttributes(a, b, foal *mob) {
	if a == nil || b == nil || foal == nil || foal.etype != entityHorse {
		return
	}
	foal.setMaxHP(int(h.breedValue(float64(a.maxHP()), float64(b.maxHP()), 15, 30)))
	foal.health = foal.maxHP()
	foal.setMoveSpeed(h.breedValue(a.moveSpeed(), b.moveSpeed(), 0.1125, 0.3375))
	foal.jumpStrength = h.breedValue(a.jumpStrength, b.jumpStrength, 0.4, 1.0)
}

// breedValue averages the parents and adds a fresh roll across the range,
// then keeps the result inside it (vanilla's setOffspringAttribute shape).
func (h *hub) breedValue(a, b, lo, hi float64) float64 {
	v := (a + b + lo + h.rng.Float64()*(hi-lo)) / 3
	return min(max(v, lo), hi)
}
