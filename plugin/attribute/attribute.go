// Package attribute is the shared vocabulary for entity attributes: the ids,
// the modifier operations, and the modifier itself.
//
// It is deliberately a PUBLIC leaf with no dependencies beyond the standard
// library, so the engine and plugins speak the same language rather than the
// engine owning a private type and the plugin API mirroring it. That mirror is
// what drifts: add an attribute on one side and the other silently lacks it.
//
// Because plugins compile against this package, it carries a compatibility
// obligation. Ids are STRINGS, not registry indices — index numbering shifts
// between Minecraft versions while the names do not, so a plugin written today
// keeps working after a version bump. Add to this package; avoid changing what
// is already here.
package attribute

// ID names an attribute, matching the vanilla registry ("minecraft:max_health").
type ID string

// Op is how a modifier folds into the total. The order matters and is fixed:
// every AddValue applies first, then every AddMultipliedBase against the
// post-addition base, then every AddMultipliedTotal against the running total.
type Op uint8

const (
	// AddValue adds a flat amount.
	AddValue Op = iota
	// AddMultipliedBase adds amount × base, where base already includes every
	// AddValue. Two of these stack additively, not compounding.
	AddMultipliedBase
	// AddMultipliedTotal multiplies the running total by (1 + amount), so
	// these DO compound with one another.
	AddMultipliedTotal
)

// Modifier is one contribution to an attribute.
//
// Source identifies who applied it, so it can be removed again — a piece of
// equipment taken off, a potion running out, an enchantment on a swapped item.
// Two modifiers with the same Source on the same attribute are the same
// modifier: applying one twice does not stack.
type Modifier struct {
	Source string
	Amount float64
	Op     Op
}

// The attribute ids. Generated from the vanilla registry — see
// attributes_gen.go for the full set with defaults and ranges.
const (
	MaxHealth                    ID = "minecraft:max_health"
	MovementSpeed                ID = "minecraft:movement_speed"
	AttackDamage                 ID = "minecraft:attack_damage"
	AttackSpeed                  ID = "minecraft:attack_speed"
	AttackKnockback              ID = "minecraft:attack_knockback"
	Armor                        ID = "minecraft:armor"
	ArmorToughness               ID = "minecraft:armor_toughness"
	KnockbackResistance          ID = "minecraft:knockback_resistance"
	FollowRange                  ID = "minecraft:follow_range"
	JumpStrength                 ID = "minecraft:jump_strength"
	Luck                         ID = "minecraft:luck"
	MaxAbsorption                ID = "minecraft:max_absorption"
	Scale                        ID = "minecraft:scale"
	StepHeight                   ID = "minecraft:step_height"
	Gravity                      ID = "minecraft:gravity"
	SafeFallDistance             ID = "minecraft:safe_fall_distance"
	FallDamageMultiplier         ID = "minecraft:fall_damage_multiplier"
	BlockBreakSpeed              ID = "minecraft:block_break_speed"
	BlockInteractionRange        ID = "minecraft:block_interaction_range"
	EntityInteractionRange       ID = "minecraft:entity_interaction_range"
	SweepingDamageRatio          ID = "minecraft:sweeping_damage_ratio"
	BurningTime                  ID = "minecraft:burning_time"
	ExplosionKnockbackResistance ID = "minecraft:explosion_knockback_resistance"
	MiningEfficiency             ID = "minecraft:mining_efficiency"
	MovementEfficiency           ID = "minecraft:movement_efficiency"
	OxygenBonus                  ID = "minecraft:oxygen_bonus"
	SneakingSpeed                ID = "minecraft:sneaking_speed"
	SubmergedMiningSpeed         ID = "minecraft:submerged_mining_speed"
	WaterMovementEfficiency      ID = "minecraft:water_movement_efficiency"
	SpawnReinforcements          ID = "minecraft:spawn_reinforcements"
	FlyingSpeed                  ID = "minecraft:flying_speed"
	TemptRange                   ID = "minecraft:tempt_range"
)
