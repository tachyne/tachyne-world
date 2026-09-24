package server

import (
	"math"

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
// refreshGearIfChanged is vanilla's updateEquipmentAttributes at vanilla's
// cadence — when equipment changes — instead of twenty times a second per
// player. Both refreshes below read only t.armor, so comparing the four worn
// stacks against the last-synced copy is complete: no writer can be missed,
// whichever path changed the slot (equip, break, durability, creative set,
// inventory load).
func (t *tracked) refreshGearIfChanged() {
	held := t.mainHand()
	if t.gearSynced && t.armor == t.lastArmor && held == t.lastHeld {
		return
	}
	t.lastArmor, t.lastHeld, t.gearSynced = t.armor, held, true
	t.refreshArmorAttrs()
	t.refreshEnchantAttrs()
}

// mainHand is the stack in the selected hotbar slot.
func (t *tracked) mainHand() invStack {
	if t.p == nil {
		return invStack{}
	}
	return t.inv.slots[t.p.heldSlot()]
}

func (t *tracked) refreshArmorAttrs() {
	points, tough, kb := 0.0, 0.0, 0.0
	for _, a := range t.armor {
		if a.count == 0 {
			continue
		}
		if p, ok := armorInfo[a.item]; ok {
			points += float64(p.Points)
			tough += p.Toughness
		}
		// Netherite carries KNOCKBACK_RESISTANCE 0.1 a piece — the whole set
		// is why netherite players barely move when they are hit.
		if netheritePiece[a.item] {
			kb += netheriteKnockbackResist
		}
	}
	a := t.playerAttrs()
	setEquip(a.Get(attr.Armor), points)
	setEquip(a.Get(attr.ArmorToughness), tough)
	setEquipKB(a.Get(attr.KnockbackResistance), kb)
}

// netheriteKnockbackResist is each netherite piece's KNOCKBACK_RESISTANCE.
const netheriteKnockbackResist = 0.1

// netheritePiece is the four-piece set.
var netheritePiece = func() map[int32]bool {
	out := map[int32]bool{}
	for _, n := range []string{"netherite_helmet", "netherite_chestplate", "netherite_leggings", "netherite_boots"} {
		if id := itemByName[n]; id != 0 {
			out[id] = true
		}
	}
	return out
}()

// setEquipKB is setEquip for the knockback modifier, which has its own source
// so it does not fight the armour one.
func setEquipKB(in *attribute.Instance, amount float64) {
	if amount == 0 {
		in.RemoveModifier(gearKnockbackSource)
		return
	}
	in.AddModifier(attr.Modifier{Source: gearKnockbackSource, Amount: amount, Op: attr.AddValue})
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

// scale is the player's SCALE (LivingEntity.getScale): what their box, their
// eye height and so their reach are multiplied by. Read without adding it to
// the attribute sync, where it only goes once something sets it.
func (t *tracked) scale() float64 { return t.playerAttrs().Peek(attr.Scale) }

// eyeHeight is the standing eye height the player sees and reaches from,
// scaled with the rest of them.
func (t *tracked) eyeHeight() float64 { return playerEyeHeightStand * t.scale() }

// halfWidth is half the player's box width, scaled.
func (t *tracked) halfWidth() float64 { return playerHalfWidth * t.scale() }

// luck is the player's LUCK, which shifts weighted loot-table rolls.
func (t *tracked) luck() float64 { return t.playerAttrs().Value(attr.Luck) }

// breathesUnderwater reports whether the player's drowning clock is suspended.
// Vanilla treats Conduit Power as Water Breathing for the air supply
// (LivingEntity.decreaseAirSupply), on top of what it does for mining speed.
func (t *tracked) breathesUnderwater() bool {
	return t.hasEffect(effWaterBreathing) > 0 || t.hasEffect(effConduitPower) > 0 || t.hasEffect(effBreathOfTheNautilus) > 0
}

// attackPeriodTicks scales a weapon's cooldown by ATTACK_SPEED. The attribute
// is a rate, so a higher value means a SHORTER recovery: Haste (+10%/level)
// shortens it, Mining Fatigue (−10%/level) drags it out. The registry default
// is 4.0, which is the unmodified case.
func (t *tracked) attackPeriodTicks(base int) int {
	// The weapon's own share of the attribute is already in base (the
	// weapon's period), so only the effects scale it: take the weapon's
	// modifier back out of the ratio.
	def := attr.Defs[attr.AttackSpeed].Default + t.heldAttackMod
	speed := t.playerAttrs().Value(attr.AttackSpeed)
	if def <= 0 || speed <= 0 {
		return base
	}
	out := int(math.Round(float64(base) * def / speed))
	if out < 1 {
		return 1
	}
	return out
}
