package server

import "math"

// Hostiles hunt by sight. Vanilla's NearestAttackableTargetGoal for players
// is registered mustSee: a monster acquires a player only when it has line
// of sight to them, and TargetGoal.canContinueToUse then keeps the target
// while it stays in follow range, letting go once it has gone unseen for
// sixty ticks (HurtByTargetGoal remembers an attacker for three hundred).
// tachyne aggroed on the nearest player within range through any amount of
// stone, so a cave full of monsters came for a player walking overhead.

const (
	targetUnseenMemory = 60  // TargetGoal.unseenMemoryTicks
	hurtByUnseenMemory = 300 // HurtByTargetGoal.unseenMemoryTicks
)

// noPlayers is an empty roster, for asking nearestQuarry about shadows only.
var noPlayers = map[int32]*tracked{}

// huntTarget is the player a hostile hunts this update: the one it has,
// while that one stays in reach and has been seen within the memory, else
// the nearest huntable player it can see. nil = nobody.
func (h *hub) huntTarget(players map[int32]*tracked, m *mob, reach float64) *tracked {
	if m.targetEID != 0 {
		if t := players[m.targetEID]; t != nil && isSurvival(t.gamemode) && !t.dead && t.dim == m.dim &&
			(t.x-m.x)*(t.x-m.x)+(t.z-m.z)*(t.z-m.z) <= reach*reach &&
			(m.etype != entityDrowned || m.anger > 0 || h.drownedOKTarget(t)) {
			memory := targetUnseenMemory
			if m.anger > 0 {
				memory = hurtByUnseenMemory
			}
			if h.mobSees(m, t) {
				m.unseenTicks = 0
				return t
			}
			m.unseenTicks += mobMoveInterval
			if m.unseenTicks <= memory {
				return t
			}
		}
		m.targetEID, m.unseenTicks = 0, 0
	}
	var best *tracked
	bestD2 := reach * reach
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead || t.dim != m.dim {
			continue
		}
		// Drowned.okTarget: by daylight a drowned only comes for somebody who
		// is in the water with it (anger from a blow overrides it, as
		// HurtByTargetGoal sits above the player goal).
		if m.etype == entityDrowned && m.anger == 0 && !h.drownedOKTarget(t) {
			continue
		}
		// Slime/MagmaCube.addTargetingGoals: the player goal's selector takes
		// only someone within 4 blocks up or down. It is an acquisition
		// test: a target already held is kept whatever its height.
		if (m.etype == entitySlime || m.etype == entityMagmaCube) && math.Abs(t.y-m.y) > 4 {
			continue
		}
		if d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d2 < bestD2 && h.mobSees(m, t) {
			best, bestD2 = t, d2
		}
	}
	if best != nil {
		m.targetEID, m.unseenTicks = best.p.eid, 0
	}
	return best
}
