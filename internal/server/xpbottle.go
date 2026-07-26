package server

// Bottles o' Enchanting: throw one and it shatters into experience.
//
// Vanilla's ThrownExperienceBottle awards 3 + rand(5) + rand(5) — 3 to 11, and
// weighted toward the middle, so a stack is worth roughly seven levels' worth
// of orbs rather than a flat rate.

var itemXPBottle = itemByName["experience_bottle"]

const (
	xpBottleBase = 3 // 3 + rand(5) + rand(5)
	xpBottleRoll = 5
)

type evThrowXPBottle struct{ eid int32 }

func (evThrowXPBottle) isHubEvent() {}

// throwXPBottle flings one on the throw arc. It is a shattering projectile
// like a snowball; the payout happens where it lands.
func (h *hub) throwXPBottle(players map[int32]*tracked, t *tracked) {
	if t.dead || t.inv == nil {
		return
	}
	slot := -1
	for i := range t.inv.slots {
		if s := &t.inv.slots[i]; s.item == itemXPBottle && s.count > 0 {
			slot = i
			break
		}
	}
	if t.gamemode == gmSurvival {
		if slot < 0 {
			return
		}
		s := &t.inv.slots[slot]
		if s.count--; s.count == 0 {
			*s = invStack{}
		}
		h.sendSlot(t, slot)
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	a := h.launchProjectileIn(players, entityXPBottle, t.dim, t.x, t.y+1.5, t.z,
		dx*throwSpeed, dy*throwSpeed, dz*throwSpeed)
	a.shooter, a.dmg, a.noHitUntil = t.p.eid, 0, h.tick.Load()+arrowNoSelfHT
	a.playerShot, a.breaks, a.xpBottle = true, true, true
	h.playSound(players, "minecraft:entity.experience_bottle.throw", sndNeutral,
		t.x, t.y, t.z, 0.5, 0.4/(h.rng.Float32()*0.4+0.8))
}

// breakXPBottle is what a shattered bottle pays out.
func (h *hub) breakXPBottle(players map[int32]*tracked, a *arrowEntity) {
	xp := xpBottleBase + h.rng.Intn(xpBottleRoll) + h.rng.Intn(xpBottleRoll)
	h.spawnXPOrbIn(players, a.dim, xp, a.x, a.y, a.z)
	h.playSound(players, "minecraft:entity.splash_potion.break", sndNeutral, a.x, a.y, a.z, 1, 1)
}
