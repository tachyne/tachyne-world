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
			if m.anger > 0 || m.etype == entityEvoker || m.etype == entityIllusioner {
				// HurtByTargetGoal, and the evoker's and illusioner's own
				// player goals (setUnseenMemoryTicks(300)).
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
		// TargetingConditions.test: the range shrinks by how visible the
		// player is to this mob (getVisibilityPercent), never below 2.
		vis := math.Max(reach*visibilityPercent(t, m), 2)
		if d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d2 < bestD2 && d2 <= vis*vis && h.mobSees(m, t) {
			best, bestD2 = t, d2
		}
	}
	if best != nil {
		m.targetEID, m.unseenTicks = best.p.eid, 0
	}
	return best
}

// visibilityPercent is LivingEntity.getVisibilityPercent for a player seen by
// mob m: 0.8 while crouching; while invisible, 0.7 × the share of armour
// slots filled (at least a tenth, so 0.07 bare); and ½ for a worn head whose
// mob_visibility names m's kind (a skeleton skull to a skeleton, a zombie
// head to a zombie, a creeper head to a creeper, a piglin head to piglins).
func visibilityPercent(t *tracked, m *mob) float64 {
	v := 1.0
	if t.sneaking {
		v *= 0.8
	}
	if t.hasEffect(effInvisibility) > 0 {
		worn := 0
		for _, a := range t.armor {
			if a.count > 0 {
				worn++
			}
		}
		cover := float32(worn) / 4
		if cover < 0.1 {
			cover = 0.1
		}
		v *= 0.7 * float64(cover)
	}
	if kinds, ok := mobVisibilityHeads[t.armor[0].item]; ok && t.armor[0].count > 0 && kinds[m.etype] {
		v *= 0.5
	}
	return math.Min(math.Max(v, 0), 10)
}

// mobVisibilityHeads is the mob_visibility component of the heads that
// carry one: who is fooled by it (each at 0.5).
var mobVisibilityHeads = func() map[int32]map[int]bool {
	out := map[int32]map[int]bool{}
	for head, kinds := range map[string][]string{
		"skeleton_skull": {"skeleton"}, "zombie_head": {"zombie"}, "creeper_head": {"creeper"},
		"piglin_head": {"piglin", "piglin_brute"},
	} {
		id, ok := itemByName[head]
		if !ok {
			continue
		}
		m := map[int]bool{}
		for _, k := range kinds {
			if e, ok := entityByName[k]; ok {
				m[e] = true
			}
		}
		out[int32(id)] = m
	}
	return out
}()

// nearestTargetable is nearestHuntable through TargetingConditions' range
// test: the player m already holds counts out to r (canContinueToUse reads no
// visibility), anyone else only within r shrunk by visibilityPercent, never
// below 2. The shooting, spitting and fleeing steps that pick "the nearest
// player" use it so an invisible player is not found by a side door.
func (h *hub) nearestTargetable(players map[int32]*tracked, m *mob, r float64) *tracked {
	var best *tracked
	bestD2 := r * r
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead || t.dim != m.dim {
			continue
		}
		d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z)
		if d2 >= bestD2 || !perceives(t, m, r, d2) {
			continue
		}
		best, bestD2 = t, d2
	}
	return best
}

// perceives is TargetingConditions' range test for a player at squared
// distance d2 from m, with the target m already holds exempt.
func perceives(t *tracked, m *mob, r, d2 float64) bool {
	if t.p.eid == m.targetEID {
		return d2 <= r*r
	}
	vis := math.Max(r*visibilityPercent(t, m), 2)
	return d2 <= vis*vis
}
