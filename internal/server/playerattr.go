package server

import (
	"github.com/tachyne/tachyne-world/internal/attribute"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Player attributes. Same pipeline the mobs use — vanilla makes no distinction
// between a player and any other living entity here, and neither should we:
// health boost, armour and the movement modifiers all want one place to live.

// newPlayerAttributes is Player.createAttributes: the starting point for a
// freshly joined player.
func newPlayerAttributes() *attribute.Map {
	a := attribute.NewMap()
	a.SetBase(attr.MaxHealth, maxHealth)
	a.SetBase(attr.MovementSpeed, 0.1) // vanilla's walking base
	a.SetBase(attr.AttackDamage, 1)    // a bare fist; the held weapon replaces it
	a.SetBase(attr.Luck, 0)
	return a
}

// playerAttrs returns the player's attribute map, creating it on first use —
// tests build a tracked player by hand and never go through the join path.
func (t *tracked) playerAttrs() *attribute.Map {
	if t.attrs == nil {
		t.attrs = newPlayerAttributes()
	}
	return t.attrs
}

// maxHP is the player's MAX_HEALTH — the ceiling every heal clamps to. Health
// itself is a float32 on the wire, so callers convert at the edge.
func (t *tracked) maxHP() float32 { return float32(t.playerAttrs().Value(attr.MaxHealth)) }

// refreshArmorAttrs re-derives ARMOR and ARMOR_TOUGHNESS from the worn pieces.
// This is vanilla's updateEquipmentAttributes, which likewise runs on the tick
// and re-asserts modifiers rather than trying to catch every place a slot can
// change — of which there are many (crafting, death drops, /clear, a relog).
//
// Re-applying the same source does not stack, so calling this every tick costs
// an arithmetic pass and nothing else.
func (t *tracked) refreshArmorAttrs() {
	points, tough := 0.0, 0.0
	for _, a := range t.armor {
		if a.count == 0 {
			continue
		}
		if p, ok := armorInfo[a.item]; ok {
			points += float64(p.Points)
			tough += p.Toughness
		}
	}
	a := t.playerAttrs()
	setEquip(a.Get(attr.Armor), points)
	setEquip(a.Get(attr.ArmorToughness), tough)
}

// setEquip applies (or clears) an equipment modifier of the given size.
func setEquip(in *attribute.Instance, amount float64) {
	if amount == 0 {
		in.RemoveModifier(gearArmorSource)
		return
	}
	in.AddModifier(attr.Modifier{Source: gearArmorSource, Amount: amount, Op: attr.AddValue})
}

// armorPoints is the player's ARMOR: worn pieces plus anything else modifying
// it. armorToughness is the matching ARMOR_TOUGHNESS.
func (t *tracked) armorPoints() float64    { return t.playerAttrs().Value(attr.Armor) }
func (t *tracked) armorToughness() float64 { return t.playerAttrs().Value(attr.ArmorToughness) }

// movementFactor is how much faster or slower than baseline the player moves —
// MOVEMENT_SPEED over its own base. Speed and Slowness are modifiers on that
// attribute, so this is the single number the movement code needs rather than
// a branch per effect.
func (t *tracked) movementFactor() float64 {
	in := t.playerAttrs().Get(attr.MovementSpeed)
	if in.Base() == 0 {
		return 1
	}
	return in.Value() / in.Base()
}

// luck is the player's LUCK, which shifts weighted loot-table rolls.
func (t *tracked) luck() float64 { return t.playerAttrs().Value(attr.Luck) }

// breathesUnderwater reports whether the player's drowning clock is suspended.
// Vanilla treats Conduit Power as Water Breathing for the air supply
// (LivingEntity.decreaseAirSupply), on top of what it does for mining speed.
func (t *tracked) breathesUnderwater() bool {
	return t.hasEffect(effWaterBreathing) > 0 || t.hasEffect(effConduitPower) > 0
}
