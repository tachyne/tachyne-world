package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The drowned's own goals. Vanilla gives them three that no other zombie has
// — go back to the water when the sun is up, come out onto the beach after
// dark, and swim up toward the surface while they are hunting something in
// the water — plus `okTarget`, which is why a drowned does not chase you
// across a beach at noon: by daylight it only goes for someone in the water.

const (
	drownedWaterSearch = 16.0 // DrownedGoToWaterGoal: how far it looks for water
	drownedBeachSearch = 8.0  // DrownedGoToBeachGoal: MoveToBlockGoal(8, 2)
	drownedArrive      = 1.5
)

// drownedOKTarget is Drowned.okTarget: by day only somebody in the water is
// worth leaving the sea for.
func (h *hub) drownedOKTarget(t *tracked) bool {
	if !h.isDayTime() {
		return true
	}
	return h.inWater(t.dim, t.x, t.y+0.2, t.z)
}

// drownedWaterStep walks a drowned back to the water by day, or out onto the
// beach at night. Reports whether it took the step.
func (h *hub) drownedWaterStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityDrowned || m.dying != 0 {
		return false
	}
	if m.hasTarget && !m.drownedGoal {
		return false // something to chase outranks the walk
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	inWater := h.inWater(m.dim, m.x, m.y+0.2, m.z)
	if m.drownedGoal && math.Hypot(m.x-m.tx, m.z-m.tz) > drownedArrive {
		return true // still walking to the spot it picked
	}
	m.drownedGoal, m.hasTarget = false, false
	switch {
	case h.isDayTime() && !inWater:
		// GoToWater: the sun is up and it is out of it — find some.
		if x, z, ok := h.nearestWaterColumn(m, drownedWaterSearch); ok {
			m.tx, m.tz = x, z
			m.hasTarget, m.drownedGoal, m.rest, m.stroll = true, true, 0, strollMax
			return true
		}
	case !h.isDayTime() && inWater && m.y >= float64(worldgen.SeaLevel-3):
		// GoToBeach: after dark, near the surface, it comes ashore.
		if x, z, ok := h.nearestBeach(m, drownedBeachSearch); ok {
			m.tx, m.tz = x, z
			m.hasTarget, m.drownedGoal, m.rest, m.stroll = true, true, 0, strollMax
			return true
		}
	}
	return false
}

// nearestWaterColumn finds water within r of the mob (the centre of the
// block, as DrownedGoToWaterGoal's random position does).
func (h *hub) nearestWaterColumn(m *mob, r float64) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, bx2, bz2, found := r*r, 0.0, 0.0, false
	ri := int(r)
	for dx := -ri; dx <= ri; dx++ {
		for dz := -ri; dz <= ri; dz++ {
			d2 := float64(dx*dx + dz*dz)
			if d2 > best {
				continue
			}
			for dy := -3; dy <= 3; dy++ {
				if worldgen.IsWater(w.At(bx+dx, by+dy, bz+dz)) {
					best, bx2, bz2, found = d2, float64(bx+dx)+0.5, float64(bz+dz)+0.5, true
					break
				}
			}
		}
	}
	return bx2, bz2, found
}

// nearestBeach finds a block within r the drowned could stand on with two
// blocks of air over it (DrownedGoToBeachGoal.isValidTarget).
func (h *hub) nearestBeach(m *mob, r float64) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, bx2, bz2, found := r*r, 0.0, 0.0, false
	ri := int(r)
	for dx := -ri; dx <= ri; dx++ {
		for dz := -ri; dz <= ri; dz++ {
			d2 := float64(dx*dx + dz*dz)
			if d2 > best {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				x, y, z := bx+dx, by+dy, bz+dz
				if !worldgen.IsSolidFull(w.At(x, y, z)) {
					continue
				}
				if w.At(x, y+1, z) != worldgen.Air || w.At(x, y+2, z) != worldgen.Air {
					continue
				}
				best, bx2, bz2, found = d2, float64(x)+0.5, float64(z)+0.5, true
				break
			}
		}
	}
	return bx2, bz2, found
}
