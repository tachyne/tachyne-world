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

// convertMob swaps a mob for its converted type in place — Mob.convertTo with
// ConversionParams.single: baby state, equipment, damage taken, name and
// persistence all come across, and nothing heals. The client sees the old
// entity vanish and the new appear.
func (h *hub) convertMob(players map[int32]*tracked, m *mob, target int) {
	h.entityGone(players, m.dim, m.eid)
	delete(h.mobs, m.eid)
	h.gridDirty()
	nm := h.spawnHostileYIn(players, target, m.dim, m.x, m.y, m.z)
	if nm == nil {
		return
	}
	nm.baby, nm.growLeft = m.baby, m.growLeft
	nm.refreshBabySpeed()
	// convertTo carries the equipment over rather than rolling it afresh.
	nm.gear, nm.held, nm.heldEnch, nm.spawnGear, nm.gearDrop = m.gear, m.held, m.heldEnch, m.spawnGear, m.gearDrop
	nm.heldDmg, nm.heldCount, nm.gearSure = m.heldDmg, m.heldCount, m.gearSure
	nm.refreshGearArmor()
	if nm.wearsAnything() {
		h.toTracking(players, nm.eid, nm.dim, nm.x, nm.z, equipEv(nm.eid, nm.heldStack(), invStack{}, nm.gear))
	}
	if m.health < nm.health {
		nm.health = m.health // carry damage across; never heal on conversion
	}
	// ConversionType.convertCommon copies the mob's IDENTITY over too: a
	// name-tagged mob keeps its name, and one flagged never to despawn stays
	// flagged. Without this a named zombie came out of the water anonymous and
	// back on the despawn clock — the tag was spent for nothing.
	nm.customName, nm.persistent = m.customName, m.persistent
	if nm.customName != "" {
		h.toTracking(players, nm.eid, nm.dim, nm.x, nm.z, metaEv(nameMeta(nm.eid, nm.customName)))
	}
	if target == entityDrowned {
		h.playSoundDim(players, m.dim, "minecraft:entity.zombie.converted_to_drowned", sndHostile, m.x, m.y, m.z, 1, 1)
	}
}

// drownedThrow is DrownedTridentAttackGoal (RangedAttackGoal(this, 1.0, 40,
// 10)) with Drowned.performRangedAttack: a trident at the drowned's target
// within 10 blocks — the player it hunts, or the villager, golem, axolotl
// or baby turtle its other target goals picked — every 40 ticks it has
// sight of it. The throw leaves 0.1 below the eyes, aims at a third of the
// target's height with the 0.2 lob, and flies at 1.6 with the difficulty's
// spread; it strikes for the thrown trident's 8.
func (h *hub) drownedThrow(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	q, ok := h.rangedQuarry(players, m, tridentRange)
	if !ok {
		return
	}
	m.yaw = float32(math.Atan2(-(q.x-m.x), q.z-m.z) * 180 / math.Pi) // face the throw
	if !h.seesQuarry(m, q, false) {
		return // RangedAttackGoal: no throw without line of sight
	}
	ox, oy, oz := m.x, m.y+mobEyeHeight(m)-0.1, m.z
	dx, dy, dz := q.x-ox, q.aimY-oy, q.z-oz
	dy += math.Hypot(dx, dz) * 0.2 // gravity lob, like an arrow
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	dev := 0.0172275 * float64(14-4*h.rules.Difficulty) // rangedAttackUncertainty
	tri := func() float64 { return dev * (h.rng.Float64() - h.rng.Float64()) }
	a := h.launchProjectileIn(players, entityTrident, m.dim, ox, oy, oz,
		(dx/d+tri())*arrowSpeed, (dy/d+tri())*arrowSpeed, (dz/d+tri())*arrowSpeed)
	a.shooter, a.dmg, a.mobShot = m.eid, tridentDamage, true // it can strike the mob it was thrown at
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:entity.drowned.shoot", sndHostile, m.x, m.y, m.z, 1, 1/(h.rng.Float32()*0.4+0.8))
	m.attackCD = 19 // 40 ticks: 19 mob-updates of cooldown plus the throwing one
}
