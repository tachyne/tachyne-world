package server

import "math"

// Guardian + Elder Guardian behaviour, ported from Guardian.GuardianAttackGoal
// and ElderGuardian: the beam attack (charge then indirect-magic damage that
// pierces armour, plus the melee) and the elder's periodic Mining Fatigue curse.
// Numbers are taken from the decompiled 26.2 source.

const (
	effMiningFatigue = 3 // mob_effect id

	// Guardian.getAttackDuration()==80; the goal's attackTime starts at -10, so a
	// beam lands ~90 ticks after lock-on. Our mob update runs every 2 ticks.
	guardianBeamUpd = 45   // ~90 ticks
	guardianBeamR   = 16.0 // beam reach (target must also be > 3 blocks: d2 > 9)

	// ElderGuardian: every 1200 ticks apply MINING_FATIGUE (amp 2, 6000-tick =
	// 300 s) to players within 50 blocks.
	elderAuraUpd = 600 // 1200 ticks / 2
	elderAuraR   = 50.0
)

// guardianTick drives one guardian/elder-guardian mob update.
func (h *hub) guardianTick(players map[int32]*tracked, m *mob) {
	elder := m.etype == entityElderGuardian
	if elder {
		if m.digClock++; m.digClock >= elderAuraUpd {
			m.digClock = 0
			for _, t := range players {
				if t.dim != m.dim {
					continue
				}
				dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
				if dx*dx+dy*dy+dz*dz <= elderAuraR*elderAuraR {
					h.applyEffect(players, t, effMiningFatigue, 2, 300) // level 3, 5 minutes
				}
			}
		}
	}
	if m.sonicCD > 0 {
		m.sonicCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, guardianBeamR)
	if t == nil {
		return
	}
	dx, dz := t.x-m.x, t.z-m.z
	if dx*dx+dz*dz <= 9 { // the beam only fires past 3 blocks (distanceToSqr > 9)
		return
	}
	m.sonicCD = guardianBeamUpd
	m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	magic := float32(1) // indirect-magic base, pierces armour
	if h.rules.Difficulty == diffHard {
		magic += 2
	}
	if elder {
		magic += 2
	}
	melee := hostileMelee(m) * h.diffMult() // doHurtTarget: normal armour-reduced melee
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSound(players, "minecraft:entity.guardian.attack", sndHostile, m.x, m.y, m.z, 1, 1)
	h.damage(players, t, magic+t.armorReduce(melee))
	if t.dead {
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
	}
}

// populateMonuments seeds a monument's 3 elder guardians and a guardian patrol
// the first time a player comes near it — vanilla monuments spawn a fixed elder
// trio via structure spawn overrides; here the server seeds them, and the
// "elder already present" guard keeps it a one-time event (mobs persist, so a
// reload does not duplicate them).
func (h *hub) populateMonuments(players map[int32]*tracked) {
	g := h.world.Gen()
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		mn := g.MonumentIn(int(t.x), int(t.z))
		if !mn.Exists {
			continue
		}
		dx, dz := t.x-float64(mn.X), t.z-float64(mn.Z)
		if dx*dx+dz*dz > 48*48 {
			continue // only when the player is actually at the monument
		}
		if h.countMobNear(entityElderGuardian, 0, float64(mn.X), float64(mn.Z), 64) > 0 {
			continue // already populated
		}
		cy := float64(mn.Y + 7) // inside the hall
		for i, off := range [][2]float64{{0, 0}, {6, 6}, {-6, -6}} {
			_ = i
			h.spawnMob(players, entityElderGuardian, float64(mn.X)+off[0], cy, float64(mn.Z)+off[1])
		}
		for i := 0; i < 8; i++ {
			ox := float64((i%4)*3 - 4)
			oz := float64((i/4)*6 - 3)
			h.spawnMob(players, entityGuardian, float64(mn.X)+ox, cy, float64(mn.Z)+oz)
		}
	}
}

// countMobNear counts live mobs of a type within radius r (horizontal) of a
// point in a dimension.
func (h *hub) countMobNear(etype, dim int, x, z, r float64) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype != etype || m.dim != dim || m.dying > 0 {
			continue
		}
		if dx, dz := m.x-x, m.z-z; dx*dx+dz*dz <= r*r {
			n++
		}
	}
	return n
}
