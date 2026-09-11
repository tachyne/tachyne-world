package server

// Strider.finalizeSpawn: one strider in thirty spawns saddled with a
// zombified piglin on its back holding a warped fungus on a stick; else
// one in ten carries a baby strider. Both riders sit glued to the walker.

func (h *hub) rollStriderRider(players map[int32]*tracked, m *mob) {
	if m.etype != entityStrider || m.baby {
		return
	}
	if h.rng.Intn(30) == 0 {
		rider := h.spawnMobIn(players, entityZombifiedPiglin, m.dim, m.x, m.y, m.z)
		if rider == nil {
			return
		}
		h.configureNetherMob(players, rider)
		rider.held = int32(itemWarpedFungusStick)
		h.toNearbyEv(players, rider.dim, rider.x, rider.z, mobEquip(rider.eid, rider.held))
		m.saddled = true
		h.toNearbyEv(players, m.dim, m.x, m.z, saddleEquip(m.eid))
		h.mountMobOn(players, rider, m, false)
		return
	}
	if h.rng.Intn(10) == 0 {
		calf := h.spawnMobIn(players, entityStrider, m.dim, m.x, m.y, m.z)
		if calf == nil {
			return
		}
		h.applySpecies(players, calf)
		calf.baby = true
		calf.setBabySpeed(true)
		h.toNearbyEv(players, calf.dim, calf.x, calf.z, metaEv(babyMeta(calf.eid, true)))
		h.mountMobOn(players, calf, m, false)
	}
}
