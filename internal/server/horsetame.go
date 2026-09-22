package server

// Taming a horse. The engine let anyone saddle and ride any horse, which
// skipped the one ritual every player knows: climb on bareback, get thrown
// off, climb on again until it settles.
//
// Vanilla keeps a TEMPER on the horse, 0 to 100. RunAroundLikeCrazyGoal rolls
// one chance in fifty each tick while an untamed horse is being ridden; on
// that roll it compares a fresh roll against the temper — under it and the
// horse is tamed, over it and the temper goes up by five and the rider is
// thrown. Feeding raises the temper too, which is why an apple or two makes
// the whole business shorter.
//
// Llamas are not ridden and camels need no taming, so this is the three that
// carry the ritual: the horse, the donkey and the mule. A skeleton or zombie
// horse is tamed by the trap that spawns it.

const (
	horseMaxTemper   = 100 // AbstractHorse.getMaxTemper
	horseBuckOdds    = 50  // one chance in fifty per tick while ridden untamed
	horseTemperPerGo = 5   // modifyTemper(5) on a failed attempt
)

// horseNeedsTaming reports the mounts that buck until they are tamed.
func horseNeedsTaming(etype int) bool {
	switch etype {
	case entityHorse, entityDonkey, entityMule:
		return true
	}
	return false
}

// horseRideTick is RunAroundLikeCrazyGoal: one roll in fifty, and on it either
// the horse gives in or the rider hits the ground. Returns true if the rider
// was thrown.
func (h *hub) horseRideTick(players map[int32]*tracked, m *mob) bool {
	if !horseNeedsTaming(m.etype) || m.tamed || m.rider == 0 {
		return false
	}
	if h.rng.Intn(horseBuckOdds) != 0 {
		return false
	}
	t := players[m.rider]
	if t == nil {
		return false
	}
	if h.rng.Intn(horseMaxTemper) < m.temper {
		h.tameHorse(players, m, t)
		return false
	}
	if m.temper += horseTemperPerGo; m.temper > horseMaxTemper {
		m.temper = horseMaxTemper
	}
	h.dismountMob(players, t)
	h.playSound(players, "minecraft:entity.horse.angry", sndNeutral, m.x, m.y, m.z, 1, 1)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameFail))
	return true
}

// tameHorse is tameWithName: the horse settles, and everyone nearby sees the
// hearts.
func (h *hub) tameHorse(players map[int32]*tracked, m *mob, t *tracked) {
	m.tamed, m.owner, m.ownerUUID = true, t.p.eid, t.p.uuid
	m.temper = horseMaxTemper
	h.playSound(players, "minecraft:entity.horse.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameOK))
}

// horseFeedTemper is the temper an item adds (AbstractHorse.handleEating).
func horseFeedTemper(item int32) int {
	switch item {
	case int32(itemByName["wheat"]), int32(itemByName["sugar"]),
		int32(itemByName["apple"]), int32(itemByName["carrot"]),
		int32(itemByName["red_mushroom"]):
		return 3
	case int32(itemByName["golden_carrot"]):
		return 5
	case int32(itemByName["golden_apple"]), int32(itemByName["enchanted_golden_apple"]):
		return 10
	case int32(itemByName["hay_block"]):
		return 0 // hay feeds but does not settle it
	}
	return 0
}
