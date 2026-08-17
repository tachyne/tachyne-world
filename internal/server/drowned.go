package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Drowned trident: a fraction of drowned spawn holding a trident and hurl it at
// range instead of biting (vanilla Drowned + ThrownTrident). The projectile
// reuses the arrow-physics entity as a fast, hard-hitting shot.

var (
	entityTrident = entityID("trident")
	itemTrident   = int32(itemByName["trident"])
)

// waterConvert maps a mob to what it becomes after long submersion: a zombie
// drowns into a drowned; a husk first reverts to a plain zombie (which may then
// convert again). Vanilla Zombie.startUnderWaterConversion / Husk.
var waterConvert = map[int]int{
	entityZombie: entityDrowned,
	entityHusk:   entityZombie,
}

const (
	// Zombie.tick: 600 ticks with its EYES in water before the conversion even
	// starts…
	drownConvertSecs = 600 / 20
	// …and then startUnderWaterConversion(300) — fifteen more seconds during
	// which the zombie shakes, and only then does it turn. We used to convert
	// the moment the first timer ran out: fifteen seconds early, and with no
	// warning shudder at all.
	drownShakeSecs = 300 / 20
	// Zombie's DATA_DROWNED_CONVERSION_ID. Zombie's own synced fields start at
	// 16 (Entity 8 + LivingEntity 7 + Mob 1) and run baby, special_type,
	// conversion — so 18, and identically so in 1.21.5 and 26.2, which is worth
	// stating because a wrong metadata index disconnects a 26.2 client outright.
	metaIndexConverting = 18
)

// convertingMeta is the shaking-zombie flag the client renders while a drowning
// conversion counts down.
func convertingMeta(eid int32, on bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexConverting)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, on)
	return protocol.AppendU8(b, itemMetaEnd)
}

// convertMob swaps a mob for its converted type in place, preserving baby state
// and not healing it. The client sees the old entity vanish and the new appear.
func (h *hub) convertMob(players map[int32]*tracked, m *mob, target int) {
	h.toNearbyEv(players, m.dim, m.x, m.z, entGone(m.eid))
	delete(h.mobs, m.eid)
	nm := h.spawnHostileY(players, target, m.x, m.y, m.z)
	if nm == nil {
		return
	}
	nm.baby, nm.growLeft = m.baby, m.growLeft
	nm.refreshBabySpeed()
	if m.health < nm.health {
		nm.health = m.health // carry damage across; never heal on conversion
	}
	if target == entityDrowned {
		h.playSoundDim(players, m.dim, "minecraft:entity.zombie.converted_to_drowned", sndHostile, m.x, m.y, m.z, 1, 1)
	}
}

// drownedThrow hurls a trident at the nearest huntable player on a cooldown
// (mirrors skeletonShoot's cadence but a heavier, straighter shot).
func (h *hub) drownedThrow(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, shootRange)
	if t == nil {
		return
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // face the throw
	ox, oy, oz := m.x, m.y+1.4, m.z
	dx, dy, dz := t.x-ox, (t.y+0.6)-oy, t.z-oz
	dy += math.Hypot(dx, dz) * 0.2 // gravity lob, like an arrow
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	a := h.launchProjectileIn(players, entityTrident, m.dim, ox, oy, oz,
		dx/d*arrowSpeed, dy/d*arrowSpeed, dz/d*arrowSpeed)
	a.shooter, a.dmg = m.eid, 9 // vanilla thrown-trident damage
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:item.trident.throw", sndHostile, m.x, m.y, m.z, 1, 1)
	m.attackCD = 19 // ≈40-tick cadence (mob-update counts 2 ticks each)
}
