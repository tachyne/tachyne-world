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
	h.playSoundDim(players, m.dim, "minecraft:entity.frog.tongue", sndNeutral, m.x, m.y, m.z, 1, 1)
	meal.frogEaten = int8(m.variant) + 1
	meal.lastAttacker = m.eid
	meal.hitByPlayer, meal.hurtByPlayerTil = false, 0
	meal.health = 0
	h.killMob(players, meal)
	h.playSoundDim(players, m.dim, "minecraft:entity.frog.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
}

const (
	frogCroakTicks = 60 // Croak.CROAK_TICKS
	poseCroaking   = 8  // Pose.CROAKING
)

// frogIdleCroak is the idle RunOne's choice when a frog's stroll ends and
// it would stand: Croak (weight 3) against a plain pause (weight 2). A frog
// in water or with somewhere to be does not croak.
func (h *hub) frogIdleCroak(players map[int32]*tracked, m *mob) {
	if m.baby || m.tempted || m.loveTicks > 0 || m.croakLeft > 0 || h.inWater(m.dim, m.x, m.y, m.z) || h.rng.Intn(5) >= 3 {
		return
	}
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
