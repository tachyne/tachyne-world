package server

import (
	"math"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The witch (Witch.aiStep + performRangedAttack): she drinks for herself —
// water breathing with her head under water, fire resistance when burning,
// healing when hurt, swiftness when her target is far — thirty-two ticks
// a bottle at a quarter less speed, and never throws while drinking. What
// she throws depends on you: slowness if you are eight or more blocks off
// and not yet slowed, poison if you have eight health or more and are not
// poisoned, weakness now and then within three blocks, harming otherwise.
// The splash itself is the thrown-potion path (splashPotion): everyone
// within four blocks, weaker with distance.

const (
	metaIndexWitchUsing = 17    // Witch DATA_USING_ITEM (Raider's IS_CELEBRATING is 16)
	witchDrinkTicks     = 32    // Items.POTION use duration
	witchDrinkSlow      = -0.25 // SPEED_MODIFIER_DRINKING (ADD_VALUE, ×attrToStep here)
	witchDrinkSource    = "drinking"
	witchThrowSpeed     = 0.75
	witchThrowSpread    = 8.0 // Projectile.shoot inaccuracy
)

func witchUsingMeta(eid int32, on bool) []byte {
	return boolMeta(eid, metaIndexWitchUsing, on)
}

// witchTick runs each mob update: the drink in progress, a new one, or a
// throw at the nearest player within ten blocks.
func (h *hub) witchTick(players map[int32]*tracked, m *mob) {
	if m.drinkTicks > 0 {
		m.drinkTicks -= mobMoveInterval
		if m.drinkTicks > 0 {
			return
		}
		h.witchFinishDrink(players, m)
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, witchRange)
	if kind := h.witchWantsToDrink(players, m, t); kind != potNone {
		h.witchStartDrink(players, m, kind)
		return
	}
	h.witchThrow(players, m, t)
}

// witchWantsToDrink is the drink table, rolled per tick.
func (h *hub) witchWantsToDrink(players map[int32]*tracked, m *mob, t *tracked) int8 {
	for i := 0; i < mobMoveInterval; i++ {
		switch {
		case h.rng.Float64() < 0.15 && h.inWater(m.dim, m.x, m.y+1.62, m.z) && m.hasEffect(effWaterBreathing) == 0:
			return potWaterBreathing
		case h.rng.Float64() < 0.15 && (m.burning || m.fireSecs > 0) && m.hasEffect(effFireRes) == 0:
			return potFireRes
		case h.rng.Float64() < 0.05 && float64(m.health) < m.mobAttrs().Value(attr.MaxHealth):
			return potHealing
		case h.rng.Float64() < 0.5 && t != nil && m.hasEffect(effSpeed) == 0 && dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) > 121:
			return potSwiftness
		}
	}
	return potNone
}

func (h *hub) witchStartDrink(players map[int32]*tracked, m *mob, kind int8) {
	m.drinkTicks, m.drinkKind = witchDrinkTicks, kind
	m.held = itemPotion
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: itemPotion, count: 1, potion: kind}, invStack{}, m.gear))
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(witchUsingMeta(m.eid, true)))
	h.playSoundDim(players, m.dim, "minecraft:entity.witch.drink", sndHostile, m.x, m.y, m.z, 1, 0.8+h.rng.Float32()*0.4)
	in := m.mobAttrs().Get(attr.MovementSpeed)
	in.RemoveModifier(witchDrinkSource)
	in.AddModifier(attr.Modifier{Source: witchDrinkSource, Amount: witchDrinkSlow * attrToStep, Op: attr.AddValue})
}

func (h *hub) witchFinishDrink(players map[int32]*tracked, m *mob) {
	for _, e := range potionEffects(m.drinkKind) {
		h.applyMobEffect(players, m, e.id, e.amp, e.secs)
	}
	m.drinkTicks, m.drinkKind, m.held = 0, potNone, 0
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(witchUsingMeta(m.eid, false)))
	m.mobAttrs().Get(attr.MovementSpeed).RemoveModifier(witchDrinkSource)
}

// witchPotionFor is performRangedAttack's choice.
func (h *hub) witchPotionFor(m *mob, t *tracked) int8 {
	d4 := math.Hypot(t.x-m.x, t.z-m.z)
	switch {
	case d4 >= 8 && t.hasEffect(effSlowness) == 0:
		return potSlowness
	case t.health >= 8 && t.hasEffect(effPoison) == 0:
		return potPoison
	case d4 <= 3 && t.hasEffect(effWeakness) == 0 && h.rng.Float64() < 0.25:
		return potWeakness
	}
	return potHarming
}

// witchThrow lobs the chosen splash potion (RangedAttackGoal: every sixty
// ticks within ten blocks).
func (h *hub) witchThrow(players map[int32]*tracked, m *mob, t *tracked) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	if t == nil {
		return
	}
	kind := h.witchPotionFor(m, t)
	dx, dy, dz := t.x-m.x, (t.y+1.62-1.1)-m.y, t.z-m.z
	d4 := math.Hypot(dx, dz)
	dy += d4 * 0.2
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	v := witchThrowSpeed * 2 // per update
	spread := witchThrowSpread * 0.0075
	vx := (dx/d + h.rng.NormFloat64()*spread) * v
	vy := (dy/d + h.rng.NormFloat64()*spread) * v
	vz := (dz/d + h.rng.NormFloat64()*spread) * v
	a := h.launchProjectileIn(players, entitySplashProj, m.dim, m.x, m.y+1.2, m.z, vx, vy, vz)
	a.shooter, a.breaks, a.splash, a.potion = m.eid, true, true, kind
	h.playSoundDim(players, m.dim, "minecraft:entity.witch.throw", sndHostile, m.x, m.y, m.z, 1, 0.8+h.rng.Float32()*0.4)
	m.attackCD = witchCooldown
}
