package server

import "math"

// Thrown potions: splash and lingering. A splash potion shatters on any impact
// (block or entity) and applies its effects to every survival player in range,
// with duration scaled by proximity — vanilla ThrownPotion.applySplash. A
// lingering potion instead leaves an area-effect cloud that re-applies its
// (weaker) effects to whoever stands in it as it shrinks away.
//
// Effects are player-only, matching the rest of the engine's status-effect
// system (mobs have no effect map). The potion's effect table lives with the
// brewing code (potionEffects) so drink/splash/lingering never drift.

const (
	splashRadius = 4.0  // vanilla applySplash inflate(4,2,4) → a 4-block reach
	splashFactor = 0.75 // splash potions carry 3/4 of the drink duration
	lingerFactor = 0.25 // a lingering cloud's effects are 1/4 duration
	cloudTicks   = 600  // ~30 s cloud lifetime (vanilla AreaEffectCloud)
	cloudRadius0 = 3.0  // starting cloud radius
	cloudReapply = 20   // re-apply the effect to occupants once a second
	cloudPuff    = 5    // emit the visible cloud particles every N ticks

	// The dragon's breath (AreaEffectCloud in vanilla's sitting-flame phase).
	breathRadius = 5.0
	breathTicks  = 200
	breathDamage = 6 // the instant damage its cloud carries
)

// effectCloud is one lingering-potion cloud resting on the ground.
type effectCloud struct {
	eid       int32
	dim       int
	x, y, z   float64
	kind      int8
	radius    float64
	ttl       int    // ticks of life left
	reapplyAt uint64 // next tick it doses whoever stands in it
	// A dragon's breath is an area-effect cloud too, but not a potion one: it
	// carries instant damage rather than a brewed effect, and unlike a
	// lingering potion it does NOT shrink — it sits at full radius for its
	// whole life, which is what makes perching on the portal so punishing.
	breath bool
}

// splashPotion resolves a thrown potion at its impact point.
func (h *hub) splashPotion(players map[int32]*tracked, dim int, x, y, z float64, kind int8, lingering bool) {
	// The burst is a level event carrying the brew's colour, not a generic
	// water splash: the client draws the bottle shards and a hundred effect
	// particles tinted like the liquid (AbstractThrownPotion.onHit). Instant
	// potions get the other of the pair, which uses the sharper particle.
	// The breaking-glass sound is a level event of its own that follows it;
	// the particle events are silent.
	ev, snd := int32(worldEventPotionSplash), int32(worldEventSplashSound)
	if potionIsInstant(kind) {
		ev, snd = worldEventInstantSplash, worldEventInstantSound
	}
	bx, by, bz := floorInt(x), floorInt(y), floorInt(z)
	h.levelEvent(players, dim, ev, bx, by, bz, potionColor(kind))
	h.levelEvent(players, dim, snd, bx, by, bz, 0)
	if kind == potWater {
		h.splashWater(players, dim, x, y, z) // splash and lingering both
	}
	if lingering {
		h.spawnPotionCloud(dim, x, y, z, kind)
		return
	}
	effs := potionEffects(kind)
	if len(effs) == 0 {
		return
	}
	for _, t := range players {
		if t.dim != dim || !isSurvival(t.gamemode) || t.dead {
			continue
		}
		d := dist3(t.x, t.y+1, t.z, x, y, z)
		if d > splashRadius {
			continue
		}
		h.applyPotionAoE(players, t, effs, 1-d/splashRadius, splashFactor)
	}
	// Vanilla doses every LivingEntity in the cloud, not just players — a
	// splash of Harming is how you clear a cave, and Healing is how you patch
	// up your horse.
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 {
			continue
		}
		d := dist3(m.x, m.y+1, m.z, x, y, z)
		if d > splashRadius {
			continue
		}
		h.applyThrownPotionMob(players, m, effs, 1-d/splashRadius, splashFactor)
	}
}

// splashWater is AbstractThrownPotion.affectEntitiesAround for a bottle of
// water — the one potion in #hurts_water_sensitive_entities and
// #extinguishes_entities. Every living thing within four blocks of the
// shatter (the bottle's box grown 4, 2, 4, then a true distance under four)
// that is water-sensitive takes a point of indirect magic, and every one
// on fire is put out. It runs for a lingering bottle as much as a splash
// one: it is the hit that does it, not a cloud, and water leaves none.
func (h *hub) splashWater(players map[int32]*tracked, dim int, x, y, z float64) {
	near := func(ex, ey, ez, hw, ht float64) bool {
		if ex+hw < x-splashRadius-0.125 || ex-hw > x+splashRadius+0.125 ||
			ez+hw < z-splashRadius-0.125 || ez-hw > z+splashRadius+0.125 ||
			ey+ht < y-2 || ey > y+0.25+2 {
			return false
		}
		dx, dy, dz := ex-x, ey-y, ez-z
		return dx*dx+dy*dy+dz*dz < splashRadius*splashRadius
	}
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || m.health <= 0 {
			continue
		}
		b := m.box()
		if !near(m.x, m.y, m.z, b.w/2, b.h) {
			continue
		}
		if waterSensitive(m.etype) {
			if m.etype == entityEnderman {
				// Enderman.hurtServer: a thrown potion's hurt lands only when
				// it is clean water, and either way the enderman is gone —
				// repeatedlyTryToTeleport, not the one-in-ten blink.
				h.hurtMobNoBlink(players, m, 1, dtIndirectMagic)
				if h.mobs[m.eid] != nil && m.health > 0 {
					h.endermanTeleportHard(players, m)
				}
			} else {
				h.hurtMobOf(players, m, 1, dtIndirectMagic)
			}
		}
		if h.mobs[m.eid] != nil && m.health > 0 && (m.fireSecs > 0 || m.burning) {
			m.fireSecs = 0 // extinguishFire
			if m.burning {
				m.burning = false
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
			}
		}
	}
	for _, t := range players {
		if t.dim == dim && !t.dead && t.fireSecs > 0 && near(t.x, t.y, t.z, 0.3, 1.8*t.scale()) {
			t.fireSecs = 0
		}
	}
	for _, st := range h.armorStands {
		if st.dim == dim && st.fire > 0 && near(st.x, st.y, st.z, 0.25, 1.975) {
			st.fire = 0
			h.toTracking(players, st.eid, st.dim, st.x, st.z, metaEv(fireMetadata(st.eid, false)))
		}
	}
}

// applyThrownPotionMob is applyPotionAoEMob for a thrown bottle. The one
// difference is the enderman: Harming reaches it as a hurt whose direct
// entity is the bottle, and Enderman.hurtServer turns any such hurt that is
// not clean water into no damage and a repeatedlyTryToTeleport. A cloud's
// Harming is the cloud's, not a bottle's, and lands as it does on anything.
func (h *hub) applyThrownPotionMob(players map[int32]*tracked, m *mob, effs []potEffect, prox, factor float64) {
	if m.etype != entityEnderman {
		h.applyPotionAoEMob(players, m, effs, prox, factor)
		return
	}
	rest := make([]potEffect, 0, len(effs))
	harmed := false
	for _, e := range effs {
		if e.id == effInstantDamage {
			harmed = true
			continue
		}
		rest = append(rest, e)
	}
	h.applyPotionAoEMob(players, m, rest, prox, factor)
	if harmed && h.mobs[m.eid] != nil && m.health > 0 {
		h.endermanTeleportHard(players, m)
	}
}

// potionIsInstant is Potion.hasInstantEffects: a brew whose effects land all
// at once (Healing, Harming) rather than over time. It picks the splash
// particle and, in vanilla, how the AoE scales.
func potionIsInstant(kind int8) bool {
	for _, e := range potionEffects(kind) {
		if effectIsInstant(e.id) {
			return true
		}
	}
	return false
}

// applyPotionAoEMob is applyPotionAoE for a mob: timed effects scale with
// proximity, instant ones scale their magnitude instead.
func (h *hub) applyPotionAoEMob(players map[int32]*tracked, m *mob, effs []potEffect, prox, factor float64) {
	if prox < 0 {
		prox = 0
	}
	for _, e := range effs {
		if effectIsInstant(e.id) { // instant: the magnitude is what proximity scales
			mag := prox * float64(int(1)<<e.amp)
			switch e.id {
			case effInstantHealth:
				if ignoresPoisonAndRegen(m.etype) {
					h.hurtMobEffect(players, m, float64(int(6*mag)))
				} else {
					h.healMob(m, int(4*mag))
				}
			case effInstantDamage:
				if ignoresPoisonAndRegen(m.etype) {
					h.healMob(m, int(4*mag)) // an undead is healed by Harming: 4 << amp
				} else {
					h.hurtMobEffect(players, m, float64(int(6*mag)))
				}
			default:
				h.applyMobEffect(players, m, e.id, e.amp, 0)
			}
			continue
		}
		if ticks := int(float64(e.ticks) * factor * prox); ticks >= 1 {
			h.applyMobEffectTicks(players, m, e.id, e.amp, ticks)
		}
	}
}

// applyPotionAoE doses one player with a potion's effects, scaling timed effects
// by proximity×factor and instant effects by proximity (vanilla applySplash).
func (h *hub) applyPotionAoE(players map[int32]*tracked, t *tracked, effs []potEffect, prox, factor float64) {
	if prox < 0 {
		prox = 0
	}
	for _, e := range effs {
		if effectIsInstant(e.id) {
			// HealOrHarmMobEffect.applyInstantenousEffect: both scale with
			// proximity and truncate to whole points — heal (int)(scale ×
			// (4 << amp)), harm (int)(scale × (6 << amp)) as indirect magic.
			if e.id == effInstantHealth {
				heal := float32(int(prox * float64(int(4)<<e.amp)))
				t.health = float32(math.Min(float64(t.maxHP()), float64(t.health)+float64(heal)))
				h.sendHealth(t)
			} else if dmg := int(prox * float64(int(6)<<e.amp)); dmg > 0 {
				h.damageOf(players, t, float32(dmg), dtIndirectMagic)
			}
			continue
		}
		if ticks := int(float64(e.ticks) * factor * prox); ticks >= 1 {
			h.applyEffectTicks(players, t, e.id, e.amp, ticks)
		}
	}
}

// spawnBreathCloud lays the dragon's breath: full radius for its whole life,
// dealing the instant damage vanilla's cloud carries rather than an effect.
func (h *hub) spawnBreathCloud(dim int, x, y, z float64) int32 {
	eid := h.allocEID()
	h.clouds[eid] = &effectCloud{eid: eid, dim: dim, x: x, y: y, z: z,
		radius: breathRadius, ttl: breathTicks, reapplyAt: h.tick.Load(), breath: true}
	return eid
}

// spawnPotionCloud drops a lingering-potion cloud at the impact.
func (h *hub) spawnPotionCloud(dim int, x, y, z float64, kind int8) {
	if len(potionEffects(kind)) == 0 {
		return // water/awkward: nothing to linger
	}
	eid := h.allocEID()
	h.clouds[eid] = &effectCloud{eid: eid, dim: dim, x: x, y: y, z: z, kind: kind,
		radius: cloudRadius0, ttl: cloudTicks, reapplyAt: h.tick.Load()}
}

// updateClouds shrinks every lingering cloud, doses occupants on its cadence,
// and puffs its particles; a cloud expires when its life or radius runs out.
func (h *hub) updateClouds(players map[int32]*tracked) {
	if len(h.clouds) == 0 {
		return
	}
	now := h.tick.Load()
	for eid, c := range h.clouds {
		c.ttl--
		if !c.breath {
			c.radius -= cloudRadius0 / float64(cloudTicks) // linear shrink to nothing
		}
		if c.ttl <= 0 || c.radius <= 0.3 {
			delete(h.clouds, eid)
			continue
		}
		if now%cloudPuff == 0 {
			h.spawnParticles(players, c.dim, particleSplash, c.x, c.y+0.1, c.z, float32(c.radius), 0, int32(c.radius*6))
		}
		if now < c.reapplyAt {
			continue
		}
		c.reapplyAt = now + cloudReapply
		effs := potionEffects(c.kind)
		// AreaEffectCloud doses every LivingEntity inside it, mobs included: a
		// lingering potion of Harming thrown at a zombie horde works.
		if !c.breath {
			h.grid().nearby(c.dim, c.x, c.z, c.radius+1, func(m *mob) {
				if m.dying > 0 || m.dim != c.dim {
					return
				}
				if dist3(m.x, m.y, m.z, c.x, c.y, c.z) > c.radius {
					return
				}
				h.applyPotionAoEMob(players, m, effs, 1, lingerFactor)
			})
		}
		for _, t := range players {
			if t.dim != c.dim || !isSurvival(t.gamemode) || t.dead {
				continue
			}
			if dist3(t.x, t.y+1, t.z, c.x, c.y, c.z) > c.radius {
				continue
			}
			if c.breath {
				h.hurtBy(players, t, breathDamage, dtDragonBreath,
					deathCause{by: "the dragon's breath"})
				continue
			}
			h.applyPotionAoE(players, t, effs, 1, lingerFactor)
		}
	}
}

// A thrown potion's throw (ThrowablePotionItem.use): a −20° lift on the
// pitch and power 0.5.
const (
	potionThrowLift  = -20.0
	potionThrowPower = 0.5
)

// thrownPotionType is the entity a thrown potion flies as: a lingering
// potion is its own type (ThrownLingeringPotion), not a splash potion.
func thrownPotionType(lingering bool) int {
	if lingering {
		return entityLingerProj
	}
	return entitySplashProj
}

// throwSplashPotion launches a thrown-potion projectile from a player (use_item
// on a splash/lingering potion), consuming one from the slot.
func (h *hub) throwSplashPotion(players map[int32]*tracked, t *tracked, slot int) {
	s := t.handStack(slot) // a hotbar slot or the offhand
	if s == nil {
		return
	}
	if s.item != itemSplashPotion && s.item != itemLingerPotion {
		return
	}
	lingering := s.item == itemLingerPotion
	kind := s.potion
	// ThrowablePotionItem.use: from the eyes (less 0.1), shootFromRotation
	// with a −20° lift on the pitch at power 0.5, the thrower's motion added.
	vx, vy, vz := h.throwFromRotation(t, potionThrowLift, potionThrowPower, throwUncertainty)
	a := h.launchProjectileIn(players, thrownPotionType(lingering), t.dim, t.x, t.y+1.5, t.z, vx, vy, vz)
	a.shooter, a.splash, a.breaks, a.potion, a.lingering = t.p.eid, true, true, kind, lingering
	a.playerShot, a.noHitUntil = true, h.tick.Load()+2 // don't shatter on the thrower at launch
	h.playSoundDim(players, t.dim, "minecraft:entity.splash_potion.throw", sndPlayer, t.x, t.y, t.z, 0.5, 1)
	if t.gamemode != gmCreative {
		if s.count--; s.count <= 0 {
			*s = invStack{}
		}
		h.sendHandSlot(t, slot)
	}
}
