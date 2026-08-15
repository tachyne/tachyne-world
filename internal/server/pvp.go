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

	landed := h.hurtFrom(players, v, dmg, dtPlayerAttack,
		deathCause{key: causePlayer, by: t.p.name}, from(t.x, t.z))
	h.knockbackPvP(t, v) // a raised shield eats the hit but not the shove
	if !landed {
		return true
	}
	// A mace smash lands its shockwave in PvP too. meleeSwing already folded the
	// fall bonus into the damage, but the rest of the smash — the wave, the
	// fall-damage reset, the sound, wind_burst — only ever ran against mobs.
	// After the landed check, because vanilla runs post-attack effects only
	// when the blow actually connected.
	if smash, fall := maceSmashing(t); smash {
		h.smashAround(players, t, v.x, v.y, v.z, v.p.eid, fall)
	}
	// Fire Aspect sets the victim alight.
	if lvl := heldStack(t).enchLvl(enchFireAspect); lvl > 0 && v.hasEffect(effFireRes) == 0 {
		v.fireSecs = max(v.fireSecs, 4*lvl)
	}
	// …and the victim's Thorns bites back. Both sit AFTER the hurt because
	// vanilla only runs post-attack effects when the blow actually landed.
	h.thornsRetaliatePlayer(players, v, t)

	if v.dead {
		h.incCustom(t, "player_kills", 1)
		// Player.awardKillScore: a player kill counts on BOTH criteria —
		// playerKillCount for player victims, totalKillCount for any. Only the
		// statistic was being kept, so a scoreboard tracking either read zero
		// no matter how the fight went.
		h.sbCriteria(players, "playerKillCount", t.p.name, 1, false)
		h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
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
