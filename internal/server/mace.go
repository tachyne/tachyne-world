package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The mace: a heavy melee weapon whose signature is the SMASH ATTACK. Striking a
// mob while falling more than 1.5 blocks adds fall-distance-scaled bonus damage
// (huge from height), negates the attacker's own fall damage, and sends a
// shockwave that knocks back everything nearby. Its enchants: density (more
// bonus per block fallen), breach (ignores some armour), wind_burst (launches
// the attacker back up to chain smashes). Base 6 damage, attack speed 0.6.

const (
	maceSmashThreshold = 1.5 // vanilla SMASH_ATTACK_FALL_THRESHOLD: fall > 1.5 blocks to smash
	maceHeavyThreshold = 5.0 // fall > 5 doubles the shockwave and plays the heavy sound
	maceKnockRadius    = 3.5 // shockwave reaches this far
	maceKnockPower     = 0.7 // vanilla SMASH_ATTACK_KNOCKBACK_POWER

	// Mace-only enchantment ids (our declared registry order).
	enchBreach    = 4
	enchDensity   = 6
	enchWindBurst = 41

	windBurstGrace = 12 // ticks the movement authority lets the wind-burst launch fly (reuses spinUntil)
)

var itemMace = int32(itemByName["mace"])

// maceFallBonus is vanilla MaceItem.getAttackDamageBonus: +4/block for the first
// 3, +2/block for 3–8, +1/block beyond 8 (continuous in fall distance).
func maceFallBonus(fall float64) float64 {
	switch {
	case fall <= 3:
		return 4 * fall
	case fall <= 8:
		return 12 + 2*(fall-3)
	default:
		return 22 + (fall - 8)
	}
}

// maceSmashing reports whether this attack is a smash and the fall distance, for
// a mace held while descending past the threshold.
func maceSmashing(t *tracked) (bool, float64) {
	if t == nil || t.p.heldItem() != itemMace || !t.airborne || t.y >= t.peakY {
		return false, 0
	}
	fall := t.peakY - t.y
	return fall > maceSmashThreshold, fall
}

// smashEffects applies the smash side effects after a successful hit: negate the
// attacker's fall damage, shove nearby mobs, play the smash fx, and (with
// wind_burst) launch the attacker back into the air to chain smashes.
func (h *hub) smashEffects(players map[int32]*tracked, t *tracked, target *mob, fall float64) {
	t.peakY = t.y // vanilla resetFallDistance: the smash negates the attacker's fall damage

	heavy := 1.0
	if fall > maceHeavyThreshold {
		heavy = 2.0
	}
	for _, o := range h.mobs {
		if o == target || o.dying > 0 || o.noKB || o.dim != t.dim {
			continue
		}
		dx, dz := o.x-t.x, o.z-t.z
		dist := math.Hypot(dx, dz)
		if dist > maceKnockRadius || dist < 1e-6 {
			continue
		}
		power := (maceKnockRadius - dist) * maceKnockPower * heavy
		o.vx, o.vz, o.kb, o.reroute = dx/dist*power, dz/dist*power, 3, 0
		h.mobKnockVelocity(players, o)
	}

	snd := "minecraft:item.mace.smash_ground"
	if fall > maceHeavyThreshold {
		snd = "minecraft:item.mace.smash_ground_heavy"
	}
	h.playSound(players, snd, sndPlayer, t.x, t.y, t.z, 1, 1)
	h.spawnParticles(players, particlePoof, t.x, t.y, t.z, maceKnockRadius/2, 0.1, 40)

	// wind_burst: launch the attacker upward like a wind charge to chain smashes.
	if wb := heldStack(t).enchLvl(enchWindBurst); wb > 0 {
		mult := windBurstMult(wb)
		t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: 0, VY: maceKnockPower * mult, VZ: 0})
		now := h.tick.Load()
		t.spinUntil = now + windBurstGrace // let the launch through the speed check
		t.moveBudget = budgetCapTicks * spinPerTick
		h.playSound(players, "minecraft:entity.wind_charge.wind_burst", sndPlayer, t.x, t.y, t.z, 1, 1)
	}
}

// windBurstMult is the vanilla wind_burst knockback multiplier per level.
func windBurstMult(level int) float64 {
	switch {
	case level >= 3:
		return 2.2
	case level == 2:
		return 1.75
	default:
		return 1.2
	}
}
