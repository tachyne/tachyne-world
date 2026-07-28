package server

import (
	"math"

	"github.com/tachyne/tachyne-world/plugin"
)

// Player versus player.
//
// Until now attacks aimed at another player fell straight through: attackMob
// looked the target up in the mob registry, found nothing, and returned. Two
// players could swing at each other all day and neither would take a scratch.
//
// This runs the same swing the mobs get — weapon, attributes, cooldown
// scaling, crit, mace smash, Knockback and Fire Aspect — through the player
// damage pipeline, so armour, protection enchantments, shields, absorption
// and Thorns all apply exactly as they do against a mob's bite.

// attackPlayer is the player-target branch of a melee swing. Reports whether
// the target was a player at all, so the caller can fall through to mobs.
func (h *hub) attackPlayer(players map[int32]*tracked, attacker, target int32) bool {
	v := players[target]
	if v == nil {
		return false
	}
	t := players[attacker]
	if t == nil || t == v || v.dead || t.dead {
		return true // a player target, just not a hittable one
	}
	if !h.rules.PvP {
		return true // gamerule pvp
	}
	if t.dim != v.dim {
		return true
	}
	if t.gamemode != gmSurvival && t.gamemode != gmAdventure {
		return true // creative/spectator swings do not hurt (vanilla: creative does, but
	} //           tachyne has no creative-damage path and silently no-ops rather than guessing)
	if v.gamemode == gmCreative || v.gamemode == gmSpectator {
		return true // an invulnerable victim
	}
	dx, dy, dz := t.x-v.x, t.y-v.y, t.z-v.z
	if dx*dx+dy*dy+dz*dz > maxMeleeReach*maxMeleeReach {
		return true // claimed from further than an arm's reach
	}

	// Players are never undead or arthropods, so Smite and Bane contribute
	// nothing — the family bonus is always zero here.
	sw := h.meleeSwing(t, 0)
	dmg := float32(sw.dmg)

	if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
		dev := &plugin.EntityDamageByEntityEvent{AttackerEID: attacker, VictimEID: target,
			AttackerIsPlayer: true, Damage: float64(dmg)}
		if !h.plugins.Fire(dev) {
			return true
		}
		dmg = float32(math.Max(0, dev.Damage))
	}

	h.toNearbyEv(players, t.dim, t.x, t.z, swingArm(t.p.eid))
	switch {
	case sw.crit:
		h.spawnParticles(players, particleCrit, v.x, v.y+1, v.z, 0.4, 0.2, 8)
		h.playSound(players, "minecraft:entity.player.attack.crit", sndPlayer, v.x, v.y, v.z, 1, 1)
	case sw.charge >= 0.9:
		h.playSound(players, "minecraft:entity.player.attack.strong", sndPlayer, v.x, v.y, v.z, 1, 1)
	default:
		h.playSound(players, "minecraft:entity.player.attack.weak", sndPlayer, v.x, v.y, v.z, 1, 1)
	}

	// A raised shield facing the attacker eats the hit but not the shove —
	// the same rule a mob's bite obeys.
	if h.shieldBlocks(v, t.x, t.z) {
		h.shieldBlockFX(players, v)
		h.knockbackPvP(t, v)
		return true
	}

	h.hurtBy(players, v, dmg, dtPlayerAttack, deathCause{key: causePlayer, by: t.p.name})
	// Fire Aspect sets the victim alight.
	if lvl := heldStack(t).enchLvl(enchFireAspect); lvl > 0 && v.hasEffect(effFireRes) == 0 {
		v.fireSecs = max(v.fireSecs, 4*lvl)
	}
	// …and the victim's Thorns bites back. This sits AFTER the hurt because
	// vanilla only runs post-attack effects when the blow actually landed —
	// the shield branch above returns before ever reaching here.
	h.thornsRetaliatePlayer(players, v, t)
	h.knockbackPvP(t, v)

	if v.dead {
		h.incCustom(t, "player_kills", 1)
	}
	return true
}

// knockbackPvP shoves the victim away from the attacker, with the sprint and
// Knockback-enchantment bonuses vanilla adds.
func (h *hub) knockbackPvP(t, v *tracked) {
	power := 0.4
	if t.sprinting {
		power += 0.5
	}
	power += 0.5 * float64(heldStack(t).enchLvl(enchKnockback))
	h.knockbackScaled(v, t.x, t.z, power/0.4)
}
