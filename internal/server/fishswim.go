package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// AbstractFish.FishSwimGoal, a RandomSwimmingGoal(1.0, 40): a fish that is
// not following a school leader (canRandomSwim) sets off, one tick in
// twenty, for a water cell within ten blocks sideways and seven up or down
// (BehaviorUtils.getRandomSwimmablePos), swims there at its own pace and
// floats where it is until the next draw. Like every RandomStrollGoal it
// gives up drawing once its noActionTime reaches 100 ticks, which only a
// player within 32 blocks resets. Cod, salmon, tropical fish and the
// pufferfish all carry it.

const (
	fishSwimInterval = 40 / 2 // reducedTickDelay(40): a 1-in-20 draw each tick
	fishSwimXZ       = 10
	fishSwimY        = 7
	fishSwimArrive   = 1.0
	fishSwimMaxLeg   = 200 // mob updates before an unreached swim is dropped
)

// isAbstractFish is the AbstractFish class.
func isAbstractFish(etype int) bool {
	return etype == entityCod || etype == entitySalmon || etype == entityTropicalFish || etype == entityPufferfish
}

// fishSwimStep runs the goal for one update. It reports whether it took the
// fish's move.
func (h *hub) fishSwimStep(m *mob) bool {
	if !isAbstractFish(m.etype) || m.dying > 0 || !h.inWater(m.dim, m.x, m.y, m.z) {
		m.fishSwimSet = false
		return false
	}
	if schoolingFish[m.etype] && m.schoolLeader != 0 {
		m.fishSwimSet = false // canRandomSwim: a follower keeps to its leader
		return false
	}
	if !m.fishSwimSet {
		if m.idleSecs*20 >= 100 { // checkNoActionTime
			return h.fishDrift(m)
		}
		drew := false
		for i := 0; i < mobMoveInterval && !drew; i++ {
			drew = h.rng.Intn(fishSwimInterval) == 0
		}
		if !drew {
			return h.fishDrift(m)
		}
		x, y, z, ok := h.randomSwimmablePos(m, fishSwimXZ, fishSwimY)
		if !ok {
			return h.fishDrift(m)
		}
		m.fishSwimX, m.fishSwimY, m.fishSwimZ, m.fishSwimSet, m.fishSwimLeg = x, y, z, true, 0
	}
	dx, dy, dz := m.fishSwimX-m.x, m.fishSwimY-m.y, m.fishSwimZ-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if m.fishSwimLeg++; d < fishSwimArrive || m.fishSwimLeg > fishSwimMaxLeg {
		m.fishSwimSet = false // the navigation is done
		return h.fishDrift(m)
	}
	sp := m.moveSpeed() // speed modifier 1.0
	m.vx, m.vy, m.vz = dx/d*sp, dy/d*sp, dz/d*sp
	m.rest = 0
	return true
}

// fishDrift holds a fish with no swim to make: its way runs off.
func (h *hub) fishDrift(m *mob) bool {
	m.vx, m.vz = m.vx*0.6, m.vz*0.6
	return true
}

// randomSwimmablePos is BehaviorUtils.getRandomSwimmablePos: up to ten
// random cells within xz sideways and y up or down whose block a swimmer
// can path through — water.
func (h *hub) randomSwimmablePos(m *mob, xz, y int) (float64, float64, float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, 0, false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for i := 0; i < 10; i++ {
		x := bx + h.rng.Intn(2*xz+1) - xz
		cy := by + h.rng.Intn(2*y+1) - y
		z := bz + h.rng.Intn(2*xz+1) - xz
		if !w.Loaded(int32(x>>4), int32(z>>4)) || !worldgen.HoldsWater(w.At(x, cy, z)) {
			continue
		}
		return float64(x) + 0.5, float64(cy) + 0.5, float64(z) + 0.5, true
	}
	return 0, 0, 0, false
}
