package server

// Environmental damage for mobs — reimplemented from the vanilla 1.21.5
// LivingEntity/Entity (checkFallDamage/calculateFallDamage, baseTick lava/fire/
// air handling). Mobs used to be immune to all of it: they walked through lava,
// stood in fire, and never drowned.

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

const (
	mobSafeFall    = 3.0 // SAFE_FALL_DISTANCE's registry default — the base for most mobs
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
// Resistance does not stop it catching — the mob burns, flames and all, and
// only the fire's damage is turned away (hurtMobOf), so it can still be
// alight when the effect runs out.
func (m *mob) ignite(secs int) {
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
	if dt.has(tagIsFire) && m.resistsFire() {
		return // LivingEntity.hurtServer: Fire Resistance refuses #is_fire outright
	}
	if m == h.dragon && !dt.has(tagAlwaysHurtsEnderDragons) {
		return // EnderDragon.hurt: only a player or #always_hurts_ender_dragons (explosions) harms it
	}
	h.vibAt(m.dim, freqEntityDamage, m.x, m.y, m.z, m.eid)
	if m.spawnInvuln > 0 {
		return
	}
	before := m.health
	m.hurtKind(dmg, dt) // armour, resistance and protection, or not, per the tag
	if m.health == before && m.dmgFrac > 0 {
		return // soaked into the fractional carry — no flash for a scratch
	}
	h.mobDamageEv(players, m, dt, 0)
	h.infestOnMobHurt(players, m) // Infested: silverfish burst out of a mob too
	if m.health <= 0 {
		h.killMob(players, m)
		return
	}
	// EnderMan.hurtServer: hurt by anything that is not a living thing — fire,
	// a cactus, a fall — it blinks away nine times in ten.
	if m.etype == entityEnderman && dt != dtDrown && !livingSourced(dt) && h.rng.Intn(10) != 0 { // the wet rolls its own (watersensitive.go)
		h.endermanTeleport(players, m)
	}
	// PanicGoal: the environment sets an animal running too — out of the
	// fire, off the cactus, away from the lava — not only a blow from
	// something. On fire it makes for water if there is any within five.
	if !m.hostile && m.panic > 0 && panicsAt(m, dt) {
		m.panic = h.panicFor(m) // a fresh hurt: a PanicGoal runs on from the newest one
	} else if !m.hostile && panicsAt(m, dt) {
		if m.etype == entityArmadillo && m.armState != armIdle {
			h.armadilloSetState(players, m, armIdle) // ArmadilloPanic.start: Armadillo.rollOut
		}
		if x, z, ok := h.panicWaterNear(m); ok {
			m.panic, m.fleeX, m.fleeZ, m.reroute = h.panicFor(m), 2*m.x-x, 2*m.z-z, 0
		} else {
			m.panic, m.fleeX, m.fleeZ, m.reroute = h.panicFor(m), m.x+h.rng.Float64()*2-1, m.z+h.rng.Float64()*2-1, 0
		}
	}
}

// panicWaterNear is PanicGoal.lookForWater: a burning animal runs for water
// within five blocks rather than anywhere at all. The flee point is mirrored
// through the mob by the caller, since panic steering runs AWAY from it.
func (h *hub) panicWaterNear(m *mob) (float64, float64, bool) {
	if !m.burning {
		return 0, 0, false
	}
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for dx := -5; dx <= 5; dx++ {
		for dz := -5; dz <= 5; dz++ {
			for dy := -1; dy <= 1; dy++ {
				if worldgen.IsWater(w.At(bx+dx, by+dy, bz+dz)) {
					return float64(bx+dx) + 0.5, float64(bz+dz) + 0.5, true
				}
			}
		}
	}
	return 0, 0, false
}

// mobFall applies fall damage when the block under a mob is removed and it drops
// `fell` blocks (calculateFallDamage: floor(fell - safeFall), immune types
// excepted). tachyne walkers refuse steps >1 block, so this fires on dug-out ground
// / craters, not on ordinary descents.
func (h *hub) mobFall(players map[int32]*tracked, m *mob, fell float64) {
	if fallDamageImmune[m.etype] {
		return
	}
	// Slow Falling (and Levitation) reset the fall distance every tick of
	// travel, so a mob under either lands without a scratch.
	if m.hasEffect(effSlowFalling) > 0 || m.hasEffect(effLevitation) > 0 {
		return
	}
	// calculateFallDamage: (fall − SAFE_FALL_DISTANCE) × FALL_DAMAGE_MULTIPLIER.
	// Both were fixed constants here, so a horse took a fox's fall and a fox a
	// zombie's.
	// The block it lands on has its say too (Block.fallOn): hay and honey
	// take the damage down to a fifth, a bed halves the drop, slime and
	// powder snow catch it whole.
	landed := h.worldFor(m.dim).At(floorInt(m.x), floorInt(m.y-0.2), floorInt(m.z))
	if dmg := fallDamageOn(landed, fell, m.safeFallDistance(), m.fallDamageMultiplier(), false); dmg >= 1 {
		h.hurtMobOf(players, m, dmg, dtFall)
		h.playFallDamageSound(players, m.dim, m.x, m.y, m.z, dmg)
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
		h.waterAnimalDry(players, m) // a fish out of water, a dolphin drying out
		if m.health <= 0 {
			continue
		}
		w := h.worldFor(m.dim)
		fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
		feet, head := w.At(fx, fy, fz), w.At(fx, int(math.Floor(m.y+mobEyeHeight(m))), fz) // the head is where the eyes are: a floater's clear the water
		inLava := worldgen.IsLava(feet) || worldgen.IsLava(head)
		b := m.box()
		inFire := h.fireInBox(m.dim, m.x, m.y, m.z, b.w/2, b.h) > 0
		if fireImmune[m.etype] { // striders/blazes/etc. bathe unharmed
			inLava, inFire = false, false
		}

		// Lava, fire, campfire and cactus contact hurt in mobContactTick.

		// Afterburn clock (lava/fire/daylight all feed it). Water or rain douses.
		doused := worldgen.HoldsWater(feet) || worldgen.HoldsWater(head) ||
			h.inRain(m.dim, m.x, m.y, m.z, m.box().h)
		if doused {
			m.fireSecs = 0
		}
		switch {
		case m.fireSecs > 0:
			if !m.burning {
				m.burning = true
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, true)))
			}
			m.fireSecs--
			if !inLava && !inFire && m.hasEffect(effFireRes) == 0 { // lava/fire already dealt this second's damage; Fire Resistance turns it away
				h.hurtMobOf(players, m, burnDamagePerSec, dtOnFire)
				if m.health <= 0 {
					continue
				}
			}
		case m.burning:
			m.burning = false
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
		}

		h.mobPickupScan(players, m) // grab a dropped weapon/armour piece nearby

		// A skeleton standing in powder snow freezes into a stray.
		if h.strayFreezeStep(players, m) {
			continue // it turned: the skeleton is gone
		}

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
		if worldgen.HoldsWater(head) && !waterBreathers[m.etype] && !m.hasBody() { // a cube carrying a block breathes underwater
			m.submerged++
			if _, ok := waterConvert[m.etype]; ok {
				if m.submerged >= drownConvertSecs {
					// Not the conversion itself — the START of it. The mob now
					// shakes for drownShakeSecs before it turns.
					m.convertIn = drownShakeSecs
					h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(convertingMeta(m.eid, true)))
				}
			} else if m.submerged > mobMaxAir(m)/20 && m.hasEffect(effWaterBreathing) == 0 && m.hasEffect(effConduitPower) == 0 {
				// air is in ticks; /20 = seconds. MobEffectUtil.hasWaterBreathing:
				// Water Breathing or Conduit Power keeps a mob's air topped up.
				h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusDrown))
				h.hurtMobOf(players, m, drownDmgPerSec, dtDrown)
			}
		} else {
			m.submerged = 0
		}
	}
}

// safeFallDistance and fallDamageMultiplier are the two attributes vanilla's
// calculateFallDamage reads. Only the fox and the equines move off the
// registry defaults, but they are attributes rather than constants so an
// effect, a plugin or /attribute can move them too.
func (m *mob) safeFallDistance() float64 {
	return m.mobAttrs().Value(attr.SafeFallDistance)
}

func (m *mob) fallDamageMultiplier() float64 {
	return m.mobAttrs().Value(attr.FallDamageMultiplier)
}

// gravity is the mob's GRAVITY (LivingEntity.getDefaultGravity): 0.08 a
// tick unless something changed it. Read without adding it to the mob's
// attribute sync.
func (m *mob) gravity() float64 {
	if m.attrs == nil {
		return mobGravity
	}
	return m.attrs.Peek(attr.Gravity)
}

// effectiveGravity is getEffectiveGravity for a mob whose vertical speed is
// vy: Slow Falling caps it at 0.01 while it is coming down (vy ≤ 0), and
// leaves it alone on the way up.
func (m *mob) effectiveGravity(vy float64) float64 {
	g := m.gravity()
	if vy <= 0 && m.hasEffect(effSlowFalling) > 0 {
		return math.Min(g, slowFallingGravity)
	}
	return g
}

// slowFallingGravity is Slow Falling's ceiling on a falling entity's gravity.
const slowFallingGravity = 0.01

// mobContactTick is the contact hazards' entityInside hits for mobs, run at
// the 10-tick cadence the damage cooldown allows — two hits a second, as in
// vanilla — at each hazard's per-hit amount: lava 4 (Entity.lavaHurt), fire
// 1 or soul fire 2, a lit campfire 1 or 2, cactus 1.
func (h *hub) mobContactTick(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.health <= 0 || m.dying > 0 {
			continue
		}
		w := h.worldFor(m.dim)
		if w == nil {
			continue
		}
		fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
		feet, head := w.At(fx, fy, fz), w.At(fx, int(math.Floor(m.y+mobEyeHeight(m))), fz)
		inLava := worldgen.IsLava(feet) || worldgen.IsLava(head)
		b := m.box()
		fireDmg := h.fireInBox(m.dim, m.x, m.y, m.z, b.w/2, b.h) // any fire the box touches
		inFire := fireDmg > 0
		if fireImmune[m.etype] {
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
			h.hurtMobOf(players, m, fireDmg, dtInFire) // soul fire burns 2
			if m.health <= 0 {
				continue
			}
		}
		// CampfireBlock.entityInside: a lit campfire burns any living thing
		// standing in it (1, soul 2) — fire, so the fire-immune walk over it.
		if feet != 0 && isCampfireBlock(feet) && boolProp(feet, "lit") && h.rules.FireDamage &&
			!fireImmune[m.etype] && !m.resistsFire() {
			dmg := 1.0
			if isSoulCampfire(feet) {
				dmg = 2
			}
			h.hurtMobOf(players, m, dmg, dtCampfire)
			if m.health <= 0 {
				continue
			}
		}
		// CactusBlock.entityInside: every entity whose box reaches a cactus
		// takes 1 — the cactus's collision is inset a sixteenth, so a mob
		// pressed against its side is inside its cell.
		if b := m.box(); h.boxTouchesCactus(m.dim, m.x, m.y, m.z, b.w/2, b.h) {
			h.hurtMobOf(players, m, cactusDamagePerSec, dtCactus)
			if m.health <= 0 {
				continue
			}
		}
	}
}

// livingSourced reports damage whose source entity is a living thing: a
// blow, thorns, a rocket its shooter set off.
func livingSourced(dt dmgType) bool {
	switch dt {
	case dtMobAttack, dtMobAttackNoAggro, dtPlayerAttack, dtMaceSmash, dtThorns, dtFireworks, dtSting:
		return true
	}
	return false
}
