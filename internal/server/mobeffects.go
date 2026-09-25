package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Status effects on MOBS. Vanilla applies effects to any LivingEntity, so a
// splash potion of Harming hurts a zombie, a tipped arrow of Slowness slows a
// skeleton, and a poisoned spider takes damage. tachyne had effects on players
// only, which left half of vanilla's potion surface with nowhere to land.
//
// Most of the work was already done by moving mob stats onto the attribute
// pipeline: Strength, Weakness, Speed, Slowness and Health Boost on a mob need
// no code here at all, because the mob's damage, pace and health ceiling read
// their attributes. What is left is the PERIODIC effects and the immunities.

// applyMobEffect starts (or refreshes) an effect on a mob and tells the
// watching clients so the particle colours show.
// applyMobEffectTicks is applyMobEffect in vanilla's own unit — a potion's
// duration, or Bane of Arthropods' rolled Slowness, is a tick count that
// rounding through seconds would move.
func (h *hub) applyMobEffectTicks(players map[int32]*tracked, m *mob, id int32, amp, ticks int) {
	h.applyMobEffectDur(players, m, id, amp, ticks, true)
}

func (h *hub) applyMobEffect(players map[int32]*tracked, m *mob, id int32, amp, secs int) {
	h.applyMobEffectDur(players, m, id, amp, secs, false)
}

func (h *hub) applyMobEffectDur(players map[int32]*tracked, m *mob, id int32, amp, dur int, inTicks bool) {
	ticks := dur * 20
	if inTicks || dur == effInfinite {
		ticks = dur
	}
	h.addMobEffect(players, m, id, activeEffect{amp: amp, left: ticks})
}

// mobCanBeAffected is LivingEntity.canBeAffected with its overrides: the
// silverfish shrug off Infested and the slime Oozing (their tags), the
// undead poison and regeneration, spiders and nautiluses poison, the wither
// skeleton and the Wither itself Wither, and the parched Weakness.
func mobCanBeAffected(etype int, id int32) bool {
	switch {
	case etype == entitySilverfish:
		return id != effInfested
	case etype == entitySlime:
		return id != effOozing
	case (etype == entitySpider || etype == entityCaveSpider || etype == entityNautilus || etype == entityZombieNautilus) && id == effPoison:
		return false
	case (etype == entityWitherSkeleton || etype == entityWither) && id == effWither:
		return false
	case etype == entityParched && id == effWeakness:
		return false
	}
	return !ignoresPoisonAndRegen(etype) || (id != effPoison && id != effRegen)
}

// addMobEffect is LivingEntity.addEffect for a mob. Reports whether it took:
// the undead shrug off poison and regeneration, and a stronger or longer
// instance already running keeps its place.
func (h *hub) addMobEffect(players map[int32]*tracked, m *mob, id int32, in activeEffect) bool {
	amp := in.amp
	if m == nil || m.dying > 0 {
		return false
	}
	switch id {
	case effInstantHealth:
		// HealOrHarmMobEffect: the undead have it backwards — healing harms them.
		if ignoresPoisonAndRegen(m.etype) {
			h.hurtMobEffect(players, m, float64(6*(int(1)<<amp)))
		} else {
			h.healMob(m, 4*(int(1)<<amp))
		}
		return true
	case effInstantDamage:
		if ignoresPoisonAndRegen(m.etype) {
			h.healMob(m, 6*(int(1)<<amp))
		} else {
			h.hurtMobEffect(players, m, float64(6*(int(1)<<amp)))
		}
		return true
	}
	if !mobCanBeAffected(m.etype, id) {
		return false
	}
	if !m.startEffectInstance(id, in) {
		return false // a stronger or longer instance is already running
	}
	m.installEffectModifiers(id, amp)
	if id == effInvisibility || id == effGlowing {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(mobEntityFlagsMeta(m)))
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, effectEv(m.eid, id, m.effects[id]))
	h.syncMobSwirls(players, m)
	return true
}

// removeMobEffect ends one effect on a mob.
func (h *hub) removeMobEffect(players map[int32]*tracked, m *mob, id int32) {
	delete(m.effects, id)
	m.dropEffectModifiers(id)
	if id == effHealthBoost && m.health > m.maxHP() {
		m.health = m.maxHP() // the extra hearts go with the effect
	}
	if id == effInvisibility || id == effGlowing {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(mobEntityFlagsMeta(m)))
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, attachproto.Effect{EID: m.eid, ID: id, Remove: true})
	h.syncMobSwirls(players, m)
}

// updateMobEffects ticks every affected mob's effects at 20 Hz, on the same
// per-effect cadence the player path uses (Regeneration 50>>amp ticks, Poison
// 25>>amp, Wither 40>>amp — sub-second intervals a 1 Hz step cannot express).
func (h *hub) updateMobEffects(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if len(m.effects) == 0 || m.dying > 0 {
			continue
		}
		for id, e := range m.effects {
			switch id {
			case effRegen:
				if applyEffectTickNow(e.clock(h.tick.Load()), 50, e.amp) && m.health < m.maxHP() {
					h.healMob(m, 1)
				}
			case effPoison:
				// Poison never kills: it stops at one hit point.
				if applyEffectTickNow(e.clock(h.tick.Load()), 25, e.amp) && m.health > 1 {
					h.hurtMobEffect(players, m, 1)
				}
			case effWither:
				if applyEffectTickNow(e.clock(h.tick.Load()), 40, e.amp) {
					h.hurtMobEffect(players, m, 1)
				}
			}
			if m.dying > 0 { // a wither tick killed it — stop touching its effects
				break
			}
			if e.tickDown(); e.expired() {
				h.removeMobEffect(players, m, id)
			}
		}
	}
}

// hurtMobEffect deals effect damage. Vanilla deals it as magic, which bypasses
// armour — so unlike the environmental hazards this really is unarmoured, and
// naming the type is what says so rather than a comment promising it.
func (h *hub) hurtMobEffect(players map[int32]*tracked, m *mob, dmg float64) {
	h.hurtMobOf(players, m, dmg, dtMagic)
}

// healMob raises a mob's health, capped at its MAX_HEALTH.
func (h *hub) healMob(m *mob, hp int) {
	if m.health += hp; m.health > m.maxHP() {
		m.health = m.maxHP()
	}
}

// mobEntityFlagsMeta is the shared entity-flags byte for a mob — the same
// field the burning overlay uses, so it has to be written whole.
func mobEntityFlagsMeta(m *mob) []byte {
	var f byte
	if m.burning || m.fireSecs > 0 {
		f |= entFlagOnFire
	}
	if m.hasEffect(effInvisibility) > 0 {
		f |= entFlagInvisible
	}
	if m.hasEffect(effGlowing) > 0 {
		f |= entFlagGlowing
	}
	return entityFlagsMeta(m.eid, f)
}

// resistsFire reports whether Fire Resistance is keeping a mob from burning.
func (m *mob) resistsFire() bool { return m.hasEffect(effFireRes) > 0 }

// damageResistance is the Resistance effect's cut for a mob: −20% per level,
// immune at level 5 (MobEffects.DAMAGE_RESISTANCE).
func (m *mob) damageResistance() float64 {
	r := m.hasEffect(effResistance)
	if r == 0 {
		return 1
	}
	return math.Max(0, float64(25-r*5)) / 25
}

// arrowEffectsOnMob transfers an arrow's effects to the mob it struck — the
// same set the player path applies, which mobs were simply excluded from.
func (h *hub) arrowEffectsOnMob(players map[int32]*tracked, a *arrowEntity, m *mob) {
	if a.poison > 0 {
		h.applyMobEffect(players, m, effPoison, 0, a.poison)
	}
	if secs := h.witherSkullSecs(a); secs > 0 {
		h.applyMobEffect(players, m, effWither, 1, secs)
	}
	if a.weaken > 0 {
		h.applyMobEffect(players, m, effWeakness, 0, a.weaken)
	}
	if a.slow > 0 {
		h.applyMobEffect(players, m, effSlowness, 0, a.slow)
	}
	if a.levitate > 0 {
		h.applyMobEffect(players, m, effLevitation, 0, a.levitate)
	}
	if a.glow > 0 {
		h.applyMobEffect(players, m, effGlowing, 0, a.glow)
	}
	if a.tipped { // an eighth of the bottle's time, as for a player
		for _, e := range potionEffects(a.potion) {
			ticks := int(math.Round(float64(e.ticks) * tippedArrowScale))
			if ticks < 1 && e.ticks > 0 {
				ticks = 1
			}
			h.applyMobEffectTicks(players, m, e.id, e.amp, ticks)
		}
	}
}
