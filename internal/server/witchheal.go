package server

import (
	"math"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Raid witches heal (NearestHealableRaiderTargetGoal + the Raider branch of
// performRangedAttack): a witch in a raid looks now and then — one tick in
// five hundred, and only half of those — for a hurt fellow raider within
// its follow range and throws it a healing potion (four health or less)
// or one of regeneration, then leaves the players alone for ten seconds.

const (
	witchHealOdds     = 500 // NearestHealableRaiderTargetGoal randomInterval
	witchHealCooldown = 200 // DEFAULT_COOLDOWN: no attacking players meanwhile
)

// witchHealTick is the goal; true when it threw (or is still on cooldown
// and so may not throw at players).
func (h *hub) witchHealTick(players map[int32]*tracked, m *mob) bool {
	if m.witchHealCD > 0 {
		m.witchHealCD -= mobMoveInterval
		return true
	}
	if m.raidCenter == (blockPos{}) || m.drinkTicks > 0 {
		return false
	}
	if h.rng.Intn(witchHealOdds/mobMoveInterval) != 0 || h.rng.Intn(2) != 0 {
		return false
	}
	var target *mob
	bestD := m.followRange()
	h.grid().nearby(m.dim, m.x, m.z, bestD, func(o *mob) {
		if o == m || o.etype == entityWitch || o.dying > 0 || o.raidCenter == (blockPos{}) {
			return
		}
		if float64(o.health) >= o.mobAttrs().Value(attr.MaxHealth) {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD {
			target, bestD = o, d
		}
	})
	if target == nil {
		return false
	}
	kind := int8(potRegen)
	if target.health <= 4 {
		kind = potHealing
	}
	dx, dy, dz := target.x-m.x, (target.y+1.62-1.1)-m.y, target.z-m.z
	d4 := math.Hypot(dx, dz)
	dy += d4 * 0.2
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return false
	}
	v := witchThrowSpeed * 2
	a := h.launchProjectileIn(players, entitySplashProj, m.dim, m.x, m.y+1.2, m.z, dx/d*v, dy/d*v, dz/d*v)
	a.shooter, a.breaks, a.splash, a.potion, a.mobShot = m.eid, true, true, kind, true
	h.playSoundDim(players, m.dim, "minecraft:entity.witch.throw", sndHostile, m.x, m.y, m.z, 1, 0.8+h.rng.Float32()*0.4)
	m.witchHealCD = witchHealCooldown
	m.attackCD = witchCooldown
	return true
}
