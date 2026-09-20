package server

import "math"

// Shield disabling. Vanilla's axes carry a weapon component that disables
// blocking for five seconds: a blocked melee blow from an axe (a player's
// or a mob's, so long as the axe is what they attack with) puts the shield
// on a hundred-tick cooldown, lowers it, and plays the shield's disabled
// sound (Player.blockUsingItem → BlocksAttacks.disable). tachyne's shield
// took axe blows like any other.

const (
	axeDisableSeconds  = 5.0 // Item.Properties.axe: Weapon(2, 5.0)
	shieldDisableScale = 1.0 // the shield's blocks_attacks disable_cooldown_scale
)

// fromWeapon is a hit's source with what the attacker struck with, for the
// shield to judge.
func fromWeapon(x, z float64, weapon int32) dmgFrom {
	return dmgFrom{x: x, z: z, ok: true, weapon: weapon}
}

// fromMobWeapon is fromWeapon for a mob's swing: the difficulty may scale it.
func fromMobWeapon(x, z float64, weapon int32) dmgFrom {
	return dmgFrom{x: x, z: z, ok: true, weapon: weapon, byMob: true}
}

// weaponDisableSeconds is LivingEntity.getSecondsToDisableBlocking: the held
// weapon's disable_blocking_for_seconds (axes 5, everything else 0).
func weaponDisableSeconds(item int32) float64 {
	if item != 0 && axeItems[item] {
		return axeDisableSeconds
	}
	return 0
}

// disableShield is BlocksAttacks.disable: the cooldown (baseSeconds × the
// scale, in ticks), the shield lowered, the disabled sound.
func (h *hub) disableShield(players map[int32]*tracked, t *tracked, seconds float64) {
	ticks := int(math.Round(seconds * shieldDisableScale * 20))
	if ticks <= 0 {
		return
	}
	h.setCooldown(t, itemShield, ticks)
	h.lowerShield(t)
	h.playSound(players, "minecraft:item.shield.break", sndPlayer, t.x, t.y, t.z, 0.8, 0.8+h.rng.Float32()*0.4)
}
