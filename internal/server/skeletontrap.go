package server

import "math"

// The skeleton trap (SkeletonTrapGoal). A skeleton horse born of a
// lightning strike waits as a trap; when a player comes within ten blocks
// it turns tame and grown, a bolt flashes over it, a skeleton mounts it,
// and three more skeleton horses with skeleton riders burst out beside it
// — every skeleton in an iron helmet (kept if it spawned with better),
// helmet enchanted from the mob-spawn set, persistent, its bow in hand.

const skeletonTrapRange = 10.0

// tickSkeletonTraps is the goal's canUse + tick, once a second.
func (h *hub) tickSkeletonTraps(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if !m.trap || m.etype != entitySkeletonHorse || m.dying > 0 {
			continue
		}
		near := false
		for _, t := range players {
			if t.dim == m.dim && !t.dead && t.gamemode != gmSpectator &&
				dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= skeletonTrapRange {
				near = true
				break
			}
		}
		if near {
			h.springSkeletonTrap(players, m)
		}
	}
}

func (h *hub) springSkeletonTrap(players map[int32]*tracked, horse *mob) {
	horse.trap, horse.tamed, horse.baby = false, true, false
	horse.refreshBabySpeed()
	h.strikeLightning(players, horse.x, horse.y, horse.z, true) // visual only
	if sk := h.trapSkeleton(players, horse); sk != nil {
		h.mountMobOn(players, sk, horse, false)
	}
	for i := 0; i < 3; i++ {
		other := h.spawnMob(players, entitySkeletonHorse, horse.x, horse.y, horse.z)
		if other == nil {
			continue
		}
		other.tamed, other.persistent = true, true
		// push(triangle(0, 1.1485), 0, triangle(0, 1.1485)): a shove sideways
		other.vx = (h.rng.Float64() - h.rng.Float64()) * 1.1485
		other.vz = (h.rng.Float64() - h.rng.Float64()) * 1.1485
		if sk := h.trapSkeleton(players, other); sk != nil {
			h.mountMobOn(players, sk, other, false)
		}
	}
}

// trapSkeleton is createSkeleton: a finalized skeleton, persistent, in an
// iron helmet unless it already wears one, the helmet enchanted.
func (h *hub) trapSkeleton(players map[int32]*tracked, horse *mob) *mob {
	sk := h.spawnHostileY(players, entitySkeleton, horse.x, horse.y, horse.z)
	if sk == nil {
		return nil
	}
	sk.persistent = true
	if sk.gear[0].item == 0 {
		sk.gear[0] = invStack{item: armourTiers[3][0], count: 1} // iron helmet
	}
	f := h.specialMultiplier()
	cost := 5 + h.rng.Intn(int(math.Floor(f*17))+1)
	sk.gear[0].ench = enchApplyList(enchSelect(h.rng, sk.gear[0].item, cost, enchMobAllowed))
	sk.spawnGear, sk.gearDrop = true, spawnGearDropChance
	sk.refreshGearArmor()
	h.toNearbyEv(players, sk.dim, sk.x, sk.z, equipEv(sk.eid, invStack{item: sk.held, count: b2i(sk.held != 0)}, invStack{}, sk.gear))
	return sk
}
