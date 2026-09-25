package server

import "sort"

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
//
// A camel has two seats: a second rider takes the back one, and only the
// front one (the first passenger) can drive.
func (h *hub) mountMobOn(players map[int32]*tracked, rider, vehicle *mob, drives bool) {
	rider.mount, rider.mountDrives, rider.navMount = vehicle.eid, drives, nil
	if vehicle.mobRider == 0 {
		vehicle.mobRider = rider.eid
		if drives {
			rider.navMount = vehicle
		}
	} else {
		vehicle.mobRider2 = rider.eid
		rider.mountDrives = false
	}
	rider.x, rider.z = vehicle.x, vehicle.z
	h.toTracking(players, vehicle.eid, vehicle.dim, vehicle.x, vehicle.z, passengersBody(vehicle.eid, vehicle.mobPassengers()...))
}

// mobSeats is how many mobs the vehicle carries: a camel's two, one for the
// rest.
func mobSeats(etype int) int {
	if isCamelKind(etype) {
		return 2
	}
	return 1
}

// seatFree reports whether one more mob can board v.
func (v *mob) seatFree() bool {
	if v.mobRider == 0 {
		return true
	}
	return v.mobRider2 == 0 && mobSeats(v.etype) > 1
}

// mobPassengers is the vehicle's mob riders, front seat first.
func (m *mob) mobPassengers() []int32 {
	switch {
	case m.mobRider == 0:
		return nil
	case m.mobRider2 == 0:
		return []int32{m.mobRider}
	}
	return []int32{m.mobRider, m.mobRider2}
}

// freeMobSeat takes rider off v's seats. A back-seat rider moves up to the
// front when the front one leaves, as vanilla's passenger list closes up, and
// the front seat's mob is the controlling passenger: the parched left on a
// camel husk whose husk died takes the reins.
func (h *hub) freeMobSeat(players map[int32]*tracked, v *mob, rider int32) {
	switch rider {
	case v.mobRider:
		v.mobRider, v.mobRider2 = v.mobRider2, 0
		if r := h.mobs[v.mobRider]; r != nil && r.mount == v.eid {
			r.mountDrives, r.navMount = true, v
		}
	case v.mobRider2:
		v.mobRider2 = 0
	default:
		return
	}
	h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.mobPassengers()...))
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
	if sk := h.spawnHostileYIn(players, entitySkeleton, m.dim, m.x, m.y, m.z); sk != nil {
		h.mountMobOn(players, sk, m, false)
	}
}

// rollSpiderEffect is Spider.finalizeSpawn's SpiderEffectsGroupData: on
// hard, with probability 0.1 × the special multiplier, the spider spawns
// with a lasting effect — speed (two chances in five), strength,
// regeneration or invisibility.
func (h *hub) rollSpiderEffect(players map[int32]*tracked, m *mob) {
	if h.rules.Difficulty != diffHard || h.rng.Float64() >= 0.1*h.specialMultiplier() {
		return
	}
	var eff int32
	switch h.rng.Intn(5) {
	case 0, 1:
		eff = effSpeed
	case 2:
		eff = effStrength
	case 3:
		eff = effRegen
	default:
		eff = effInvisibility
	}
	h.applyMobEffect(players, m, eff, 0, 1<<20) // vanilla: infinite
}

// relinkMounts seats reloaded riders back on their vehicles, matched by the
// eids both had when saved; a vehicle that did not come back (or came back
// in another batch) leaves its rider on foot.
func (h *hub) relinkMounts(players map[int32]*tracked, riders []*mob, byOld map[int32]*mob) {
	// Drivers board first: they sat in the front seat.
	sort.SliceStable(riders, func(i, j int) bool { return riders[i].mountDrives && !riders[j].mountDrives })
	for _, r := range riders {
		old := r.savedMount
		r.savedMount = 0
		v := byOld[old]
		if v == nil || v == r || !v.seatFree() {
			r.mountDrives = false
			continue
		}
		h.mountMobOn(players, r, v, r.mountDrives)
	}
}

// rollDrownedNautilus is Drowned.finalizeSpawn's jockey: a grown drowned
// spawned naturally or by a structure with a trident rides a zombie nautilus
// half the time, except in the #more_frequent_drowned_spawns biomes (the
// rivers). A structure's nautilus is persistent, as the drowned is. The
// drowned is the controlling passenger (Mob.getControllingPassenger).
func (h *hub) rollDrownedNautilus(players map[int32]*tracked, m *mob, structure bool) {
	if m == nil || m.etype != entityDrowned || !m.trident || m.baby || m.mount != 0 {
		return
	}
	if h.rng.Float64() >= 0.5 {
		return
	}
	if w := h.worldFor(m.dim); w == nil || isRiverBiome(w.BiomeAt3D(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
		return
	}
	n := h.spawnSpecies(players, entityZombieNautilus, m.dim, m.x, m.y, m.z)
	if n == nil {
		return
	}
	n.yaw = m.yaw
	if structure {
		n.persistent = true
	}
	h.mountMobOn(players, m, n, true)
}
