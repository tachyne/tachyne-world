package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TryFindLand, a frog's way out of the water (FrogAi's SWIM activity): a
// frog standing in water looks, at most every three seconds, for the
// nearest spot within eight blocks (by steps along the axes) that is dry,
// open and on a solid top, and makes for it at 1.5 of its pace. That is why a
// frog in a pond keeps hopping out onto the bank instead of floating about.

const (
	frogFindLandRange    = 8   // TryFindLand.create(8, 1.5F)
	frogFindLandSpeed    = 1.5 //
	frogFindLandCooldown = 60  // COOLDOWN_TICKS
	frogFindLandGiveUp   = 200 // a walk target it cannot reach is dropped
)

// frogFindLandStep runs each mob update. Returns whether it holds the frog.
func (h *hub) frogFindLandStep(m *mob) bool {
	if m.panic > 0 || m.dying > 0 {
		m.frogLandSet = false
		return false
	}
	now := h.tick.Load()
	if m.frogLandSet { // the walk target, until reached (closeEnough 1)
		tx, tz := float64(m.frogLand.x)+0.5, float64(m.frogLand.z)+0.5
		if math.Hypot(tx-m.x, tz-m.z) <= 1 || now >= m.frogLandUntil {
			m.frogLandSet = false
			return false
		}
		h.steerTo(m, tx, tz, frogFindLandSpeed)
		return true
	}
	w := h.worldFor(m.dim)
	fx, fy, fz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if !worldgen.IsWater(w.At(fx, fy, fz)) || now < m.frogLandNext {
		return false
	}
	m.frogLandNext = now + frogFindLandCooldown
	if p, ok := frogLandNear(w, fx, fy, fz); ok {
		m.frogLand, m.frogLandSet, m.frogLandUntil = p, true, now+frogFindLandGiveUp
		h.steerTo(m, float64(p.x)+0.5, float64(p.z)+0.5, frogFindLandSpeed)
		return true
	}
	return false
}

// frogLandNear is findBlocksInBoxByManhattanDistance's first dry, open
// block on a sturdy top, nearest first, never straight above or below.
func frogLandNear(w interface{ At(x, y, z int) uint32 }, fx, fy, fz int) (blockPos, bool) {
	for d := 1; d <= 3*frogFindLandRange; d++ {
		for dy := -min(d, frogFindLandRange); dy <= min(d, frogFindLandRange); dy++ {
			rest := d - abs(dy)
			for dx := -min(rest, frogFindLandRange); dx <= min(rest, frogFindLandRange); dx++ {
				dzAbs := rest - abs(dx)
				if dzAbs > frogFindLandRange {
					continue
				}
				for _, dz := range [2]int{-dzAbs, dzAbs} {
					if dx == 0 && dz == 0 {
						continue // differsHorizontally
					}
					x, y, z := fx+dx, fy+dy, fz+dz
					if s := w.At(x, y, z); worldgen.HoldsWater(s) || worldgen.IsLava(s) || worldgen.IsSolid(s) {
						continue // a fluid, or something with a collision shape
					}
					if worldgen.IsSturdyTop(w.At(x, y-1, z)) {
						return blockPos{x, y, z}, true
					}
					if dzAbs == 0 {
						break
					}
				}
			}
		}
	}
	return blockPos{}, false
}
