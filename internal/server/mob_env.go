package server

// Environmental damage for mobs — reimplemented from the vanilla 1.21.5
// LivingEntity/Entity (checkFallDamage/calculateFallDamage, baseTick lava/fire/
// air handling). Mobs used to be immune to all of it: they walked through lava,
// stood in fire, and never drowned.

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

// ignite (re)lights a mob's afterburn clock to at least secs seconds. Fire
// Resistance stops it catching at all, the way it does for a player.
func (m *mob) ignite(secs int) {
	if m.resistsFire() {
		m.fireSecs = 0
		return
	}
	if secs > m.fireSecs {
		m.fireSecs = secs
	}
}

// hurtMobOf is the single way a mob takes damage that is not a melee blow:
// it names the damage TYPE and lets the type decide whether armour, Resistance
// and the protection enchantments get a say, then shows the hurt flash and
// handles death.
//
// It replaced a hurtMob that wrote to health directly on the premise that
// "fall/fire/lava/drowning bypass armor in vanilla". Half of that is true.
// Falling and drowning do; standing in LAVA or FIRE, on magma or in a berry
// bush does not, so a zombie in full diamond used to burn exactly as fast as a
// naked one — the same bug the player side had, found by fixing that one.
func (h *hub) hurtMobOf(players map[int32]*tracked, m *mob, dmg float64, dt dmgType) {
	if m.spawnInvuln > 0 {
		return
	}
	before := m.health
	m.hurtKind(dmg, dt) // armour, resistance and protection, or not, per the tag
	if m.health == before && m.dmgFrac > 0 {
		return // soaked into the fractional carry — no flash for a scratch
	}
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
		h.hurtMobOf(players, m, dmg, dtFall)
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
			h.hurtMobOf(players, m, lavaDmgPerSec, dtLava)
			if m.health <= 0 {
				continue
			}
		} else if inFire {
			m.ignite(fireAfterburn)
			h.hurtMobOf(players, m, fireDamagePerSec, dtInFire)
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
				h.hurtMobOf(players, m, burnDamagePerSec, dtOnFire)
				if m.health <= 0 {
					continue
				}
			}
		case m.burning:
			m.burning = false
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
		}

		h.mobPickupScan(players, m) // grab a dropped weapon/armour piece nearby

		// Drowning: a land mob whose eye level (head) is underwater past maxAir.
		// Zombies/husks don't drown — they convert (husk→zombie→drowned).
		// A conversion already under way runs to completion wherever the mob
		// is: Zombie.tick decrements it BEFORE it looks at the water, so
		// hauling a shaking zombie onto dry land does not save it.
		if m.convertIn > 0 {
			if m.convertIn--; m.convertIn <= 0 {
				if target, ok := waterConvert[m.etype]; ok {
					h.convertMob(players, m, target)
					continue // the old entity is gone
				}
			}
			continue
		}
		if worldgen.IsWater(head) && !waterBreathers[m.etype] {
			m.submerged++
			if _, ok := waterConvert[m.etype]; ok {
				if m.submerged >= drownConvertSecs {
					// Not the conversion itself — the START of it. The mob now
					// shakes for drownShakeSecs before it turns.
					m.convertIn = drownShakeSecs
					h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(convertingMeta(m.eid, true)))
				}
			} else if m.submerged > maxAir/20 { // maxAir is in ticks; /20 = seconds
				h.hurtMobOf(players, m, drownDmgPerSec, dtDrown)
			}
		} else {
			m.submerged = 0
		}
	}
}
