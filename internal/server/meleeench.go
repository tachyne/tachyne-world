package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// The melee enchantments that were missing: Smite and Bane of Arthropods (both
// family-specific damage), Fire Aspect (ignite on hit) and Thorns (hurt whoever
// hurt you).
//
// Smite and Bane are Sharpness's siblings in vanilla — the same DAMAGE effect
// component with a conditional on an entity-type tag — so they sit alongside it
// in the swing, added after the crit multiplier like Sharpness is.

const (
	// Enchantments.SMITE / BANE_OF_ARTHROPODS: AddValue(perLevel 2.5).
	familyDamagePerLvl = 2.5
	// FIRE_ASPECT: Ignite(perLevel 4.0) seconds.
	fireAspectSecsPerLvl = 4
	// THORNS: a 0.15/level chance, then 1..5 damage and 2 durability.
	thornsChancePerLvl = 0.15
	thornsMinDamage    = 1
	thornsMaxDamage    = 5
	thornsWear         = 2
)

// undeadTypes is #minecraft:undead — what Smite bites. Built once from the
// species names so a renamed or added entity type cannot silently fall out.
var undeadTypes = entityTypeSet(
	"skeleton", "stray", "wither_skeleton", "skeleton_horse", "bogged", "parched",
	"zombie_horse", "camel_husk", "zombie", "zombie_villager", "zombified_piglin",
	"zoglin", "drowned", "husk", "zombie_nautilus", "wither", "phantom",
)

// arthropodTypes is #minecraft:arthropod — what Bane of Arthropods bites.
var arthropodTypes = entityTypeSet("bee", "endermite", "silverfish", "spider", "cave_spider")

// entityTypeSet resolves registry names to the numeric types this engine uses.
func entityTypeSet(names ...string) map[int]bool {
	set := make(map[int]bool, len(names))
	for _, n := range names {
		set[entityID(n)] = true
	}
	return set
}

// familyMeleeBonus is the Smite / Bane of Arthropods contribution against one
// target: 2.5 per level, and only against the family the enchantment names.
func familyMeleeBonus(weapon invStack, etype int) float64 {
	lvl := 0
	if undeadTypes[etype] {
		lvl = weapon.enchLvl(enchSmite)
	} else if arthropodTypes[etype] {
		lvl = weapon.enchLvl(enchBaneOfArthropods)
	}
	return familyDamagePerLvl * float64(lvl)
}

// applyFireAspect sets a struck mob alight for 4 seconds per level. Vanilla
// gates this on the damage being direct — a melee swing always is.
func (h *hub) applyFireAspect(players map[int32]*tracked, t *tracked, m *mob) {
	lvl := heldStack(t).enchLvl(enchFireAspect)
	if lvl == 0 {
		return
	}
	if secs := fireAspectSecsPerLvl * lvl; secs > m.fireSecs {
		m.fireSecs = secs
		m.burning = true
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, true)))
	}
}

// thornsRetaliate is the POST_ATTACK effect Thorns hangs on the VICTIM: when a
// mob hits an armoured player, each worn piece rolls its own 15%-per-level
// chance to spit 1-5 damage back and wear itself by 2.
//
// Vanilla rolls per piece, so a full Thorns set retaliates far more often than
// a single chestplate — that is the whole appeal, and averaging it into one
// roll would flatten it.
func (h *hub) thornsRetaliate(players map[int32]*tracked, t *tracked, m *mob) {
	if m == nil || m.dying > 0 {
		return
	}
	for i := range t.armor {
		lvl := t.armor[i].enchLvl(enchThorns)
		if lvl == 0 || t.armor[i].count == 0 {
			continue
		}
		if h.rng.Float64() >= thornsChancePerLvl*float64(lvl) {
			continue
		}
		dmg := thornsMinDamage + h.rng.Intn(thornsMaxDamage-thornsMinDamage+1)
		m.hurt(float64(dmg))
		m.lastAttacker = t.p.eid
		m.hitByPlayer = true // the kill still pays experience
		h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
		h.wearArmorSlot(players, t, i, thornsWear)
		if m.health <= 0 {
			h.killMob(players, m)
			return
		}
	}
}
