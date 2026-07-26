package server

import (
	"math"
	"strconv"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The protection enchantments. Only generic Protection existed before, so the
// specialised armour players actually build for — fire, blast, projectile,
// feather falling — did nothing at all.
//
// Vanilla keys each of them on a damage-type TAG, which means the sort of
// damage has to travel from the source to the armour. dmgKind is exactly the
// four tags the protection family reads (is_fire, is_explosion, is_projectile,
// is_fall) and nothing more — a full damage-type registry is a separate job,
// and guessing the kind at the armour instead of naming it at the source is
// how you end up protecting against the wrong things.
type dmgKind uint8

const (
	dmgGeneric    dmgKind = iota // anything with no specialised guard: melee, magic, starvation
	dmgFire                      // in_fire / on_fire / lava / hot_floor
	dmgExplosion                 // explosion / player_explosion
	dmgProjectile                // arrow / trident / thrown / fireball
	dmgFall                      // fall / stalagmite
)

// enchantProtect applies vanilla's protection points: each matching
// enchantment on each worn piece contributes, the total is capped at 20, and
// the damage is scaled by 1 − points/25 — so 20 points is the 80% ceiling.
//
// CombatRules.getDamageAfterMagicAbsorb. Note this is SEPARATE from the armour
// points in armorReduce: vanilla runs armour absorption first and enchantment
// absorption second, and some damage (falling) skips the armour but not the
// enchantment.
func (t *tracked) enchantProtect(dmg float32, kind dmgKind) float32 {
	points := 0
	for _, a := range t.armor {
		if a.count == 0 {
			continue
		}
		points += a.enchLvl(enchProtection) // +1/level against everything
		switch kind {
		case dmgFire:
			points += 2 * a.enchLvl(enchFireProtection)
		case dmgExplosion:
			points += 2 * a.enchLvl(enchBlastProtection)
		case dmgProjectile:
			points += 2 * a.enchLvl(enchProjectileProtection)
		case dmgFall:
			points += 3 * a.enchLvl(enchFeatherFalling)
		}
	}
	if points <= 0 {
		return dmg
	}
	return dmg * float32(1-math.Min(float64(points), 20)/25)
}

// enchantAttribute is one attribute modifier a worn enchantment contributes —
// vanilla's EnchantmentEffectComponents.ATTRIBUTES, whose amounts are all
// per-level.
type enchantAttribute struct {
	id     attr.ID
	perLvl float64
	op     attr.Op
}

// enchantAttributes is the armour half of that component set. Like the effect
// table, this IS the implementation for the enchantments in it: nothing else
// mentions Respiration or Swift Sneak anywhere in the engine.
var enchantAttributes = map[int][]enchantAttribute{
	enchFireProtection:  {{attr.BurningTime, -0.15, attr.AddMultipliedBase}},
	enchBlastProtection: {{attr.ExplosionKnockbackResistance, 0.15, attr.AddValue}},
	enchRespiration:     {{attr.OxygenBonus, 1, attr.AddValue}},
	enchAquaAffinity:    {{attr.SubmergedMiningSpeed, 4, attr.AddMultipliedTotal}},
	enchSwiftSneak:      {{attr.SneakingSpeed, 0.15, attr.AddValue}},
	enchDepthStrider:    {{attr.WaterMovementEfficiency, 1.0 / 3, attr.AddValue}},
}

// enchantSource is the modifier source one enchantment owns.
func enchantSource(id int) string { return "enchantment:" + strconv.Itoa(id) }

// refreshEnchantAttrs re-derives the worn enchantments' attribute modifiers,
// summing levels across pieces the way vanilla's per-slot modifiers add up.
// Called from the same tick pass as the armour points.
func (t *tracked) refreshEnchantAttrs() {
	a := t.playerAttrs()
	for id, mods := range enchantAttributes {
		lvl := 0
		for _, piece := range t.armor {
			if piece.count > 0 {
				lvl += piece.enchLvl(int8(id))
			}
		}
		src := enchantSource(id)
		for _, m := range mods {
			in := a.Get(m.id)
			if lvl == 0 {
				in.RemoveModifier(src)
				continue
			}
			in.AddModifier(attr.Modifier{Source: src, Amount: m.perLvl * float64(lvl), Op: m.op})
		}
	}
}

// explosionKnockScale is the fraction of a blast's shove that gets through —
// Blast Protection braces you against being thrown as well as burned.
func (t *tracked) explosionKnockScale() float64 {
	return math.Max(0, 1-t.playerAttrs().Value(attr.ExplosionKnockbackResistance))
}

// burnSeconds scales an ignition's duration by BURNING_TIME, which is what
// Fire Protection shortens (−15% per level).
func (t *tracked) burnSeconds(secs int) int {
	out := int(math.Round(float64(secs) * t.playerAttrs().Value(attr.BurningTime)))
	if out < 0 {
		return 0
	}
	return out
}

// keepsAirThisTick ports decreaseAirSupply's Respiration roll: with an oxygen
// bonus of n, a 1-in-(n+1) chance of losing a breath — so Respiration III
// makes your air last four times as long on average.
func (h *hub) keepsAirThisTick(t *tracked) bool {
	bonus := t.playerAttrs().Value(attr.OxygenBonus)
	return bonus > 0 && h.rng.Float64() >= 1/(bonus+1)
}
