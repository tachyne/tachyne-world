package server

// Jockeys. Zombie.finalizeSpawn: a baby zombie (husk, drowned, zombie
// villager too) has a one-in-twenty chance of a chicken jockey — it climbs
// onto a chicken within a 5×3×5 box that nobody rides, or, failing that
// and one more one-in-twenty, onto a chicken spawned for it. The chicken
// becomes a jockey chicken: lays no eggs, despawns like a monster, pays ten
// experience. Spider.finalizeSpawn: one spider in a hundred spawns with a
// skeleton on its back. A chicken jockey's zombie DRIVES (its own AI moves
// and attacks; the chicken is carried under it); a spider jockey's skeleton
// rides (the spider walks, the skeleton is glued on and shoots).

const (
	chickenJockeyOdds = 0.05
	spiderJockeyOdds  = 100 // one in
)

// mountMobOn seats rider on vehicle. drives: the rider's AI leads and the
// vehicle follows it (a chicken jockey), else the vehicle leads (a raid
// ravager, a spider jockey).
func (h *hub) mountMobOn(players map[int32]*tracked, rider, vehicle *mob, drives bool) {
	rider.mount, rider.mountDrives = vehicle.eid, drives
	vehicle.mobRider = rider.eid
	rider.x, rider.z = vehicle.x, vehicle.z
	h.toNearbyEv(players, vehicle.dim, vehicle.x, vehicle.z, passengersBody(vehicle.eid, rider.eid))
}

// rollChickenJockey is the baby zombie's jockey roll.
func (h *hub) rollChickenJockey(players map[int32]*tracked, m *mob) {
	if !m.baby {
		return
	}
	if h.rng.Float64() < chickenJockeyOdds {
		var chicken *mob
		for _, c := range h.mobs {
			if c.etype == entityChicken && c.dim == m.dim && c.dying == 0 && c.mobRider == 0 && c.rider == 0 &&
				abs64(c.x-m.x) <= 5 && abs64(c.y-m.y) <= 3 && abs64(c.z-m.z) <= 5 {
				chicken = c
				break
			}
		}
		if chicken != nil {
			chicken.jockey = true
			h.mountMobOn(players, m, chicken, true)
		}
		return
	}
	if h.rng.Float64() < chickenJockeyOdds {
		if chicken := h.spawnSpecies(players, entityChicken, m.dim, m.x, m.y, m.z); chicken != nil {
			chicken.jockey = true
			h.mountMobOn(players, m, chicken, true)
		}
	}
}

// rollSpiderJockey is the spider's skeleton-rider roll.
func (h *hub) rollSpiderJockey(players map[int32]*tracked, m *mob) {
	if h.rng.Intn(spiderJockeyOdds) != 0 {
		return
	}
	if sk := h.spawnHostileY(players, entitySkeleton, m.x, m.y, m.z); sk != nil {
		h.mountMobOn(players, sk, m, false)
	}
}
