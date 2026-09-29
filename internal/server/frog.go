package server

import "math"

// Frogs eat (vanilla FrogAi + Frog.canEat + the magma_cube loot table's
// frog branch): a frog goes after the nearest small slime or small magma
// cube, walks up to it and takes it with its tongue. A slime eaten this way
// leaves nothing; a magma cube leaves the froglight of the frog's variant —
// ochre for temperate, pearlescent for warm, verdant for cold.

const (
	frogHuntRange   = 6.0  // how far a frog notices a meal
	frogTongueRange = 1.75 // ShootTongue's reach
	frogHuntSpeed   = 1.25
)

var froglightItems = map[int32]int32{
	frogTemperate: int32(itemByName["ochre_froglight"]),
	frogWarm:      int32(itemByName["pearlescent_froglight"]),
	frogCold:      int32(itemByName["verdant_froglight"]),
}

func froglightFor(variant int32) int32 {
	if it, ok := froglightItems[variant]; ok {
		return it
	}
	return froglightItems[frogTemperate]
}

// frogFood is Frog.canEat: a size-one slime or magma cube.
func frogFood(m *mob) bool {
	return (m.etype == entitySlime || m.etype == entityMagmaCube) && m.size <= 1 && m.dying == 0
}

// frogStep steers a frog at a meal, or eats it. Returns true when hunting.
func (h *hub) frogStep(players map[int32]*tracked, m *mob) bool {
	if m.baby || m.panic > 0 || m.loveTicks > 0 {
		return false
	}
	var meal *mob
	best := frogHuntRange
	h.grid().nearby(m.dim, m.x, m.z, frogHuntRange, func(o *mob) {
		if !frogFood(o) {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best {
			meal, best = o, d
		}
	})
	if meal == nil {
		return false
	}
	if best <= frogTongueRange {
		h.frogEat(players, m, meal)
		m.vx, m.vz = 0, 0
		return true
	}
	dx, dz := meal.x-m.x, meal.z-m.z
	if hd := math.Hypot(dx, dz); hd > 1e-6 {
		sp := m.moveSpeed() * frogHuntSpeed
		m.vx, m.vz = dx/hd*sp, dz/hd*sp
	}
	m.rest = 0
	return true
}

// frogEat is the tongue landing: the meal dies as a frog's kill.
func (h *hub) frogEat(players map[int32]*tracked, m, meal *mob) {
	h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.frog.tongue", sndNeutral, m.x, m.y, m.z, 2, 1)
	meal.frogEaten = int8(m.variant) + 1
	meal.lastAttacker = m.eid
	meal.hitByPlayer, meal.hurtByPlayerTil = false, 0
	meal.health = 0
	h.killMob(players, meal)
	h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.frog.eat", sndNeutral, m.x, m.y, m.z, 2, 1)
}

const (
	frogCroakTicks = 60 // Croak.CROAK_TICKS
	poseCroaking   = 8  // Pose.CROAKING
)

// frogIdleStep is FrogAi's idle RunOne, run ashore whenever the frog has
// no walk target: RandomStroll.stroll(1) (weight 1),
// SetWalkTargetFromLookTarget(1, 3) (1), Croak (3) and a do-nothing that
// needs the ground under it (2). The do-nothing finishes at once, so the
// frog draws again on its next update. It reports whether it took the move.
func (h *hub) frogIdleStep(players map[int32]*tracked, m *mob) bool {
	if m.dying > 0 || m.tempted || m.croakLeft > 0 || h.inWater(m.dim, m.x, m.y, m.z) {
		return false // SWIM runs in the water (IS_IN_WATER); a croak holds it
	}
	if h.idleWalkStep(m) {
		return true
	}
	started := h.runOne([]int{1, 1, 3, 2}, func(i int) bool {
		switch i {
		case 0:
			h.randomStroll(m, 1, 10, 7)
			return true
		case 1:
			return h.walkFromLookTarget(players, m, 1)
		case 2: // Croak: from the standing pose
			if m.goatJumping {
				return false
			}
			h.frogCroak(players, m)
			return true
		default:
			return m.grounded()
		}
	})
	if !started {
		return false
	}
	if !h.idleWalkStep(m) {
		m.vx, m.vz = m.vx*0.6, m.vz*0.6
	}
	return true
}

// frogCroak is Croak.start: the CROAKING pose for sixty ticks.
func (h *hub) frogCroak(players map[int32]*tracked, m *mob) {
	m.croakLeft = frogCroakTicks
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseCroaking)))
}

// frogCroakStep holds a croaking frog still until the sixty ticks are up
// (or something sends it running), then stands it back up.
func (h *hub) frogCroakStep(players map[int32]*tracked, m *mob) bool {
	if m.croakLeft -= mobMoveInterval; m.croakLeft <= 0 || m.panic > 0 || m.hasTarget {
		m.croakLeft = 0
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, poseStanding)))
		return false
	}
	m.vx, m.vz = 0, 0
	return true
}
