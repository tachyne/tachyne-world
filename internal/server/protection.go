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
// damage has to travel from the source to the armour. It does: every hit names
// its dmgType, and the tag table (damagetags_gen.go) answers what guards it.
// Guessing at the armour instead of naming it at the source is how you end up
// protecting against the wrong things.

// has reports whether this damage type carries a tag.
func (d dmgType) has(t dmgTag) bool {
	if int(d) >= len(dmgTypeTags) {
		return false
	}
	return dmgTypeTags[d]&t != 0
}

// name is the damage type's registry name, for diagnostics.
func (d dmgType) name() string {
	if int(d) >= len(dmgTypeNames) {
		return "unknown"
	}
	return dmgTypeNames[d]
}

// exhaustion is the food cost of being hit by this sort of damage. Vanilla
// hangs it off the damage type, so a hit that costs hunger cannot be dealt
// without charging for it.
func (d dmgType) exhaustion() float32 {
	if int(d) >= len(dmgTypeExhaustion) {
		return 0
	}
	return dmgTypeExhaustion[d]
}

// enchantProtect applies vanilla's protection points: each matching
// enchantment on each worn piece contributes, the total is capped at 20, and
// the damage is scaled by 1 − points/25 — so 20 points is the 80% ceiling.
//
// CombatRules.getDamageAfterMagicAbsorb. Note this is SEPARATE from the armour
// points in armorReduce: vanilla runs armour absorption first and enchantment
// absorption second, and some damage (falling) skips the armour but not the
// enchantment.
func (t *tracked) enchantProtect(dmg float32, dt dmgType) float32 {
	return applyProtection(dmg, protectionPoints(t.armor[:], dt))
}

// protectionPoints totals the protection an entity's worn pieces give against
// one sort of damage. Takes the pieces rather than the wearer so mobs — which
// pick up dropped gear, enchantments and all — go through the same arithmetic.
//
// Each enchantment tests its OWN tag and they stack, which is why these are
// independent ifs rather than a switch: a ghast's fireball is both is_fire and
// is_projectile, so Fire Protection and Projectile Protection both count
// against it. Nothing protects against damage that bypasses invulnerability.
func protectionPoints(pieces []invStack, dt dmgType) int {
	if dt.has(tagBypassesInvulnerability) {
		return 0
	}
	points := 0
	for _, a := range pieces {
		if a.count == 0 {
			continue
		}
		points += a.enchLvl(enchProtection) // +1/level against everything
		if dt.has(tagIsFire) {
			points += 2 * a.enchLvl(enchFireProtection)
		}
		if dt.has(tagIsExplosion) {
			points += 2 * a.enchLvl(enchBlastProtection)
		}
		if dt.has(tagIsProjectile) {
			points += 2 * a.enchLvl(enchProjectileProtection)
		}
		if dt.has(tagIsFall) {
			points += 3 * a.enchLvl(enchFeatherFalling)
		}
	}
	return points
}

// applyProtection scales damage by protection points, capped at 20 (80%).
func applyProtection(dmg float32, points int) float32 {
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
