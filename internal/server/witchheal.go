package server

import (
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
	h.witchLob(players, m, target.x+target.vx/mobMoveInterval, target.y+mobEyeHeight(target), target.z+target.vz/mobMoveInterval, kind, true) // vx is per mob update
	m.witchHealCD = witchHealCooldown
	m.attackCD = witchCooldown
	return true
}
