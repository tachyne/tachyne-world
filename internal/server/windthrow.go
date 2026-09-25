package server

import "math"

// A player's wind charge (WindChargeItem.use): thrown from the eyes at 1.5
// blocks a tick along the look, the throw sound, one charge consumed, and
// the item's use_cooldown of half a second — the projectile is the same
// gust the breeze throws (AbstractWindCharge): a point of damage and the
// burst on whatever it strikes.

const (
	pearlCooldown        = 20 // ender_pearl use_cooldown 1.0s
	windChargeCooldown   = 10 // wind_charge use_cooldown 0.5s
	windChargeHitDamage  = 1  // AbstractWindCharge.onHitEntity hurtServer(…, 1.0F)
	playerEyeHeightStand = 1.62
)

// throwWindCharge handles a player's wind-charge right-click.
func (h *hub) throwWindCharge(players map[int32]*tracked, t *tracked) {
	if h.onCooldown(t, itemWindCharge) {
		return
	}
	if isSurvival(t.gamemode) {
		if s := usedStack(t); s.item != itemWindCharge || s.count <= 0 {
			return
		}
		h.consumeUsed(t)
	}
	vx, vy, vz := h.throwFromRotation(t, 0, 1.5, throwUncertainty) // WindChargeItem.use
	a := h.launchProjectileIn(players, entityWindCharge, t.dim, t.x, t.y+t.eyeHeight(), t.z, vx, vy, vz)
	a.shooter = t.p.eid
	a.noHitUntil = h.tick.Load() + arrowNoSelfHT
	a.playerShot = true
	h.playSoundDim(players, t.dim, "minecraft:entity.wind_charge.throw", sndNeutral, t.x, t.y, t.z, 0.5, 0.4/(h.rng.Float32()*0.4+0.8))
	h.setCooldown(t, itemWindCharge, windChargeCooldown)
}

// windChargeShoveMob is the gust's shove on a mob it strikes: along the
// charge's momentum, scaled by the mob's knockback resistance.
func (h *hub) windChargeShoveMob(players map[int32]*tracked, a *arrowEntity, m *mob) {
	d := math.Hypot(a.vx, a.vz)
	if d < 1e-6 || m.kbScale() <= 0 {
		return
	}
	kbp := 0.5 * m.kbScale()
	m.vx, m.vz, m.kb, m.reroute = a.vx/d*kbp, a.vz/d*kbp, 3, 0
	h.mobKnockVelocity(players, m)
}
