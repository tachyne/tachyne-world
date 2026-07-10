package server

// Environmental damage for mobs — reimplemented from the vanilla 1.21.5
// LivingEntity/Entity (checkFallDamage/calculateFallDamage, baseTick lava/fire/
// air handling). Mobs used to be immune to all of it: they walked through lava,
// stood in fire, and never drowned.

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"tachyne/internal/worldgen"
)

const (
	mobSafeFall    = 3.0 // SAFE_FALL_DISTANCE — blocks of fall tolerated before damage
	lavaDmgPerSec  = 4   // Entity.lavaHurt is 4.0 per hit
	drownDmgPerSec = 2   // LivingEntity drowning damage
	lavaAfterburn  = 15  // setSecondsOnFire(15) on leaving lava
	fireAfterburn  = 8   // fire block / daylight
)

// fallDamageImmune are the entity types in minecraft:fall_damage_immune
// (1.21.5 data): golems, flyers, bouncers, and a few others.
var fallDamageImmune = map[int]bool{
	entityIronGolem: true, entitySnowGolem: true, entityShulker: true,
	entityAllay: true, entityBat: true, entityBee: true, entityBlaze: true,
	entityCat: true, entityChicken: true, entityGhast: true, entityPhantom: true,
	entityMagmaCube: true, entityOcelot: true, entityParrot: true,
	entityWither: true, entityBreeze: true,
}

// waterBreathers never drown (fish, squid, guardians, tadpole, axolotl).
var waterBreathers = map[int]bool{
	entityCod: true, entitySalmon: true, entityTropicalFish: true, entityPufferfish: true,
	entitySquid: true, entityGlowSquid: true, entityGuardian: true, entityElderGuardian: true,
	entityTadpole: true, entityAxolotl: true,
}

// fireImmune are the entity types with EntityType.fireImmune() — no lava/fire
// damage, never catch fire. Their pathfinding likewise tolerates lava/fire (see
// malusFor); keep the two lists consistent.
var fireImmune = map[int]bool{
	entityStrider: true, entityBlaze: true, entityMagmaCube: true,
	entityWitherSkeleton: true, entityWither: true, entityZombifiedPiglin: true,
	entityGhast: true, entityZoglin: true, entityEnderDragon: true,
}

// ignite (re)lights a mob's afterburn clock to at least secs seconds.
func (m *mob) ignite(secs int) {
	if secs > m.fireSecs {
		m.fireSecs = secs
	}
}

// hurtMob applies environmental damage to a mob, shows the hurt flash, and
// handles death. Fall/fire/lava/drowning BYPASS armor in vanilla, so this
// decrements health directly (accumulating sub-1 amounts) rather than going
// through mob.hurt's armor reduction.
func (h *hub) hurtMob(players map[int32]*tracked, m *mob, dmg float64) {
	if m.spawnInvuln > 0 {
		return
	}
	dmg += m.dmgFrac
	whole := math.Floor(dmg)
	m.dmgFrac = dmg - whole
	m.health -= int(whole)
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
	if m.health <= 0 {
		h.killMob(players, m)
	}
}

// mobFall applies fall damage when the block under a mob is removed and it drops
// `fell` blocks (calculateFallDamage: floor(fell - safeFall), immune types
// excepted). tachyne walkers refuse steps >1 block, so this fires on dug-out ground
// / craters, not on ordinary descents.
func (h *hub) mobFall(players map[int32]*tracked, m *mob, fell float64) {
	if fallDamageImmune[m.etype] {
		return
	}
	if dmg := math.Floor(fell - mobSafeFall); dmg >= 1 {
		h.hurtMob(players, m, dmg)
	}
}

// mobEnvironment is the 1 Hz per-mob hazard pass: lava/fire contact, the
// afterburn clock (flame visual + 1 HP/s; water or rain douses it), and drowning
// for land mobs whose eyes stay underwater past their breath.
func (h *hub) mobEnvironment(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.health <= 0 {
			continue
		}
		w := h.worldFor(m.dim)
		fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
		feet, head := w.At(fx, fy, fz), w.At(fx, fy+1, fz)
		inLava := worldgen.IsLava(feet) || worldgen.IsLava(head)
		inFire := isFire(feet) || isFire(head)
		if fireImmune[m.etype] { // striders/blazes/etc. bathe unharmed
			inLava, inFire = false, false
		}

		if inLava {
			m.ignite(lavaAfterburn)
			h.hurtMob(players, m, lavaDmgPerSec)
			if m.health <= 0 {
				continue
			}
		} else if inFire {
			m.ignite(fireAfterburn)
			h.hurtMob(players, m, fireDamagePerSec)
			if m.health <= 0 {
				continue
			}
		}

		// Afterburn clock (lava/fire/daylight all feed it). Water or rain douses.
		doused := worldgen.IsWater(feet) || worldgen.IsWater(head) ||
			(m.dim == 0 && h.raining && h.skyExposed(m))
		if doused {
			m.fireSecs = 0
		}
		switch {
		case m.fireSecs > 0:
			if !m.burning {
				m.burning = true
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, true)))
			}
			m.fireSecs--
			if !inLava && !inFire { // lava/fire already dealt this second's damage
				h.hurtMob(players, m, burnDamagePerSec)
				if m.health <= 0 {
					continue
				}
			}
		case m.burning:
			m.burning = false
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
		}

		// Drowning: a land mob whose eye level (head) is underwater past maxAir.
		if worldgen.IsWater(head) && !waterBreathers[m.etype] {
			m.submerged++
			if m.submerged > maxAir/20 { // maxAir is in ticks; /20 = seconds
				h.hurtMob(players, m, drownDmgPerSec)
			}
		} else {
			m.submerged = 0
		}
	}
}
