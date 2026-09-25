package server

// Raid witches heal (NearestHealableRaiderTargetGoal + the Raider branch of
// performRangedAttack): a witch in a raid looks now and then — one tick in
// five hundred, and only half of those — for the nearest fellow raider it
// can see within its follow range (any raider but a witch, hurt or not) and
// makes it her target. Her RangedAttackGoal then walks her in until it is
// within ten blocks and has been in sight for five ticks, and on its throw
// clock lobs a healing potion (four health or less) or one of regeneration
// and lets the target go. The goal's 200-tick cooldown starts when she
// picks the raider, and while it runs she leaves the players alone
// (setCanAttack(false)).

const (
	witchHealOdds     = 500 // NearestHealableRaiderTargetGoal randomInterval
	witchHealCooldown = 200 // DEFAULT_COOLDOWN: no attacking players meanwhile
)

// witchHealTick is the goal; true while it holds the witch's attack (a
// raider targeted, or the cooldown still running).
func (h *hub) witchHealTick(players map[int32]*tracked, m *mob) bool {
	if m.witchHealCD > 0 {
		m.witchHealCD -= mobMoveInterval
	}
	if t := h.witchHealTargetOf(m); t != nil {
		los := h.seeTimeTickLOS(m, h.mobSeesMob(m, t), false)
		if m.attackCD > 0 {
			m.attackCD--
			return true
		}
		if !los || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > witchRange {
			return true // RangedAttackGoal: still closing, or out of sight
		}
		kind := int8(potRegen)
		if t.health <= 4 {
			kind = potHealing
		}
		h.witchLob(players, m, t.x+t.vx/mobMoveInterval, t.y+mobEyeHeight(t), t.z+t.vz/mobMoveInterval, kind, true) // vx is per mob update
		m.attackCD = witchCooldown
		m.witchHealTarget = 0 // setTarget(null)
		return true
	}
	if m.witchHealCD > 0 {
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
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD && h.mobSeesMob(m, o) {
			target, bestD = o, d
		}
	})
	if target == nil {
		return false
	}
	m.witchHealTarget, m.seeTime = target.eid, 0
	m.witchHealCD = witchHealCooldown
	return true
}

// witchHealTargetOf is the raider the heal goal made her target, while it
// is still a live raider within her follow range; a lost one is let go.
func (h *hub) witchHealTargetOf(m *mob) *mob {
	if m.witchHealTarget == 0 {
		return nil
	}
	t := h.mobs[m.witchHealTarget]
	if t == nil || t.dying > 0 || t.dim != m.dim || t.raidCenter == (blockPos{}) ||
		dist3(t.x, t.y, t.z, m.x, m.y, m.z) > m.followRange() {
		m.witchHealTarget = 0
		return nil
	}
	return t
}
