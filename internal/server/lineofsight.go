package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Line of sight. Vanilla's LivingEntity.hasLineOfSight clips a ray from the
// mob's eyes to the target's eyes through the blocks' collision shapes (no
// fluids) and calls it seen only when nothing is hit within 128 blocks; every
// ranged goal asks it before a shot, and RangedAttackGoal and the bow and
// crossbow goals keep a seeTime that gates the shot and the advance. tachyne
// had no such thing: a skeleton shot through a stone wall, a ghast charged a
// fireball at a player in a sealed room, and a guardian's beam went through
// the monument's walls.

const (
	sightMaxDist   = 128.0 // LivingEntity.hasLineOfSight: past this, unseen
	playerEyeStand = 1.62  // Player standing eye height
	playerEyeSneak = 1.27  // …crouching
	bowSeeGiveUp   = -60   // RangedBowAttackGoal: the draw is let go past this
)

// playerEyeY is a player's getEyeY.
func playerEyeY(t *tracked) float64 {
	if t.p.sneaking {
		return t.y + playerEyeSneak
	}
	return t.y + playerEyeStand
}

// mobSees is LivingEntity.hasLineOfSight(target) for a player target.
func (h *hub) mobSees(m *mob, t *tracked) bool {
	if t.dim != m.dim {
		return false
	}
	return h.sightClear(m.dim, m.x, m.y+mobEyeHeight(m), m.z, t.x, playerEyeY(t), t.z)
}

// mobSeesMob is the same for a mob target.
func (h *hub) mobSeesMob(m, o *mob) bool {
	if o.dim != m.dim {
		return false
	}
	return h.sightClear(m.dim, m.x, m.y+mobEyeHeight(m), m.z, o.x, o.y+mobEyeHeight(o), o.z)
}

// sightClear is Level.clip with ClipContext.Block.COLLIDER and Fluid.NONE
// reduced to a yes/no: it walks the cells the segment crosses (Amanatides &
// Woo) and reports whether none of them, the two end cells aside, collides.
func (h *hub) sightClear(dim int, x0, y0, z0, x1, y1, z1 float64) bool {
	dx, dy, dz := x1-x0, y1-y0, z1-z0
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist > sightMaxDist {
		return false
	}
	if dist < 1e-9 {
		return true
	}
	w := h.worldFor(dim)
	if w == nil {
		return true
	}
	cx, cy, cz := int(math.Floor(x0)), int(math.Floor(y0)), int(math.Floor(z0))
	ex, ey, ez := int(math.Floor(x1)), int(math.Floor(y1)), int(math.Floor(z1))
	sx, tMaxX, tDeltaX := ddaAxis(x0, dx)
	sy, tMaxY, tDeltaY := ddaAxis(y0, dy)
	sz, tMaxZ, tDeltaZ := ddaAxis(z0, dz)
	for steps := 0; steps < 3*int(sightMaxDist)+3; steps++ {
		if cx == ex && cy == ey && cz == ez {
			return true
		}
		switch {
		case tMaxX < tMaxY && tMaxX < tMaxZ:
			cx += sx
			tMaxX += tDeltaX
		case tMaxY < tMaxZ:
			cy += sy
			tMaxY += tDeltaY
		default:
			cz += sz
			tMaxZ += tDeltaZ
		}
		if cx == ex && cy == ey && cz == ez {
			return true
		}
		if worldgen.Collides(w.At(cx, cy, cz)) {
			return false
		}
	}
	return true
}

// ddaAxis is one axis of the traversal setup: the step direction, the
// parametric distance to the first cell boundary, and the distance between
// boundaries (in units of the segment, so 1 = the whole way).
func ddaAxis(o, d float64) (step int, tMax, tDelta float64) {
	if d == 0 {
		return 0, math.Inf(1), math.Inf(1)
	}
	tDelta = math.Abs(1 / d)
	if d > 0 {
		return 1, (math.Floor(o) + 1 - o) / d, tDelta
	}
	return -1, (o - math.Floor(o)) / -d, tDelta
}

// seeTimeTick is the ranged goals' sight bookkeeping for one mob update:
// RangedAttackGoal counts seeTime up while the target is seen and resets it
// unseen; the bow and crossbow goals zero it whenever sight changes and then
// count up seen, down unseen (a bow held past -60 is let go). Returns whether
// the target is seen now.
func (h *hub) seeTimeTick(m *mob, t *tracked, bow bool) bool {
	return h.seeTimeTickLOS(m, h.mobSees(m, t), bow)
}

// seeTimeTickLOS is seeTimeTick with the sight test already made (a mob target).
func (h *hub) seeTimeTickLOS(m *mob, los bool, bow bool) bool {
	switch {
	case !bow && los:
		m.seeTime += mobMoveInterval
	case !bow:
		m.seeTime = 0
	default:
		if los != (m.seeTime > 0) {
			m.seeTime = 0
		}
		if los {
			m.seeTime += mobMoveInterval
		} else {
			m.seeTime -= mobMoveInterval
		}
	}
	return los
}
