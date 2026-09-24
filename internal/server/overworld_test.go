package server

// Overworld shorthands for test fixtures. Production code names the
// dimension at every call (see spawnMobIn): these were once production
// helpers, and a caller in the Nether that used one acted in the overworld.

func (h *hub) spawnMob(players map[int32]*tracked, etype int, x, y, z float64) *mob {
	return h.spawnMobIn(players, etype, dimOverworld, x, y, z)
}

func (h *hub) spawnItem(players map[int32]*tracked, item int32, count int, x, y, z float64) *itemEntity {
	return h.spawnItemIn(players, dimOverworld, item, count, x, y, z)
}

func (h *hub) spawnXPOrb(players map[int32]*tracked, value int, x, y, z float64) {
	h.spawnXPOrbIn(players, dimOverworld, value, x, y, z)
}

func (h *hub) setBlock(players map[int32]*tracked, pos blockPos, state uint32) {
	h.setBlockAt(players, 0, pos, state)
}

func (h *hub) spawnHostileY(players map[int32]*tracked, etype int, x, y, z float64) *mob {
	return h.spawnHostileYIn(players, etype, 0, x, y, z)
}

func (h *hub) spawnHostile(players map[int32]*tracked, etype, x, z int) *mob {
	return h.spawnHostileY(players, etype, float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
}

func (h *hub) scheduleAround(pos blockPos, delay uint64) { h.scheduleAroundIn(0, pos, delay) }

func (h *hub) schedule(pos blockPos, delay uint64) { h.scheduleIn(0, pos, delay) }

func (h *hub) explodeAt(players map[int32]*tracked, cx, cy, cz float64, radius int, power float64, kind blastKind) {
	h.explodeIn(players, 0, cx, cy, cz, radius, power, kind)
}

func (h *hub) launchProjectile(players map[int32]*tracked, etype int, x, y, z, vx, vy, vz float64) *arrowEntity {
	return h.launchProjectileIn(players, etype, 0, x, y, z, vx, vy, vz)
}
