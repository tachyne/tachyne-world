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
	// The damage is a CONTINUOUS roll across [1,5) — vanilla samples a float,
	// not one of five whole numbers, so 2.7 is as ordinary a Thorns hit as 3.
	thornsChancePerLvl = 0.15
	thornsMinDamage    = 1.0
	thornsMaxDamage    = 5.0
	thornsWear         = 2
	// The thorns damage type's food exhaustion.
	thornsExhaustion = 0.1
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
	before := m.fireSecs
	m.ignite(fireAspectSecsPerLvl * lvl) // …unless it is fire-resistant
	if m.fireSecs > before {
		m.burning = true
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(mobEntityFlagsMeta(m)))
	}
}

// thornsHit is one armour piece firing: which slot, and for how much.
type thornsHit struct {
	slot int
	dmg  float64
}

// thornsRolls is the POST_ATTACK effect Thorns hangs on the VICTIM, resolved
// down to "which pieces fired". Vanilla hangs it on the victim's equipment and
// applies it to whoever landed the blow, WITHOUT caring what the attacker is —
// so the roll is shared and only the delivery differs between a mob attacker
// and a player one.
//
// Vanilla rolls per piece, so a full Thorns set retaliates far more often than
// a single chestplate — that is the whole appeal, and averaging it into one
// roll would flatten it.
func (h *hub) thornsRolls(t *tracked) []thornsHit {
	var hits []thornsHit
	for i := range t.armor {
		lvl := t.armor[i].enchLvl(enchThorns)
		if lvl == 0 || t.armor[i].count == 0 {
			continue
		}
		if h.rng.Float64() >= thornsChancePerLvl*float64(lvl) {
			continue
		}
		hits = append(hits, thornsHit{slot: i,
			dmg: thornsMinDamage + h.rng.Float64()*(thornsMaxDamage-thornsMinDamage)})
	}
	return hits
}

// thornsRetaliate spits the rolled damage back at a MOB attacker.
func (h *hub) thornsRetaliate(players map[int32]*tracked, t *tracked, m *mob) {
	if m == nil || m.dying > 0 {
		return
	}
	for _, hit := range h.thornsRolls(t) {
		m.hurtKind(hit.dmg, dtThorns)
		m.lastAttacker = t.p.eid
		m.hitByPlayer = true // the kill still pays experience
		h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: m.yaw})
		h.wearArmorSlot(players, t, hit.slot, thornsWear, dtThorns)
		if m.health <= 0 {
			h.killMob(players, m)
			return
		}
	}
}

// thornsRetaliatePlayer spits it back at a PLAYER attacker. The armour still
// wears even when the damage lands on someone it cannot hurt: vanilla runs the
// durability cost and the damage as one all_of effect, gated only by the
// chance roll, so a creative opponent still blunts your chestplate.
func (h *hub) thornsRetaliatePlayer(players map[int32]*tracked, victim, attacker *tracked) {
	if attacker == nil || attacker.dead {
		return
	}
	// Every caller today already sits behind the pvp gate, but the gate belongs
	// with the damage as well: gating one route and leaving another open is how
	// bows kept working after fists stopped.
	if attacker != victim && !h.rules.PvP {
		return
	}
	for _, hit := range h.thornsRolls(victim) {
		dmg := float32(hit.dmg)
		h.hurtFrom(players, attacker, dmg, dtThorns,
			deathCause{key: causeThorns, by: victim.p.name}, from(victim.x, victim.z))
		h.wearArmorSlot(players, victim, hit.slot, thornsWear, dtThorns)
		if attacker.dead {
			return
		}
	}
}

// thornsAgainstShooter fires the victim's Thorns at whoever loosed a
// projectile. Vanilla runs the same post-attack effects for an arrow as for a
// fist and resolves the "attacker" to the SHOOTER rather than to the arrow, so
// an archer takes the spikes from right across the field.
func (h *hub) thornsAgainstShooter(players map[int32]*tracked, victim *tracked, shooter int32) {
	if s := players[shooter]; s != nil {
		h.thornsRetaliatePlayer(players, victim, s)
	} else if m := h.mobs[shooter]; m != nil {
		h.thornsRetaliate(players, victim, m)
	}
}
