package server

import (
	"math"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Reach for the entities that are not mobs, stands or vehicles: item
// frames, paintings, leash knots and end crystals. handleAttack and
// handleInteract test every target's bounding box against
// entity_interaction_range + 3 before anything else happens.

// aabb is an axis-aligned box.
type aabb struct{ x0, y0, z0, x1, y1, z1 float64 }

// aabbOfSize is AABB.ofSize: a box of the given size centred on (x, y, z).
func aabbOfSize(x, y, z, sx, sy, sz float64) aabb {
	return aabb{x - sx/2, y - sy/2, z - sz/2, x + sx/2, y + sy/2, z + sz/2}
}

// withinEntityAABB is Player.isWithinEntityInteractionRange(aabb, buffer).
func withinEntityAABB(t *tracked, b aabb, buffer float64) bool {
	reach := t.playerAttrs().Value(attr.EntityInteractionRange) + buffer
	ex, ey, ez := t.x, t.y+t.eyeHeight(), t.z
	dx := math.Max(0, math.Max(b.x0-ex, ex-b.x1))
	dy := math.Max(0, math.Max(b.y0-ey, ey-b.y1))
	dz := math.Max(0, math.Max(b.z0-ez, ez-b.z1))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

// hangingStep is dirStep as floats, for a hanging entity's facing.
func hangingStep(dir int32) (float64, float64, float64) {
	if dir < 0 || dir > 5 {
		return 0, 0, 0
	}
	s := dirStep[dir]
	return float64(s[0]), float64(s[1]), float64(s[2])
}

// hangingWallShift is how far a hanging entity's centre sits from its
// cell's centre, toward the wall.
const hangingWallShift = 0.46875

// frameBox is ItemFrame.createBoundingBox: a 0.75 plate (a full block with a
// map in it), 1/16 deep, pushed against the support.
func (f *itemFrame) frameBox() aabb {
	sx, sy, sz := hangingStep(f.dir)
	cx := float64(f.x) + 0.5 - sx*hangingWallShift
	cy := float64(f.y) + 0.5 - sy*hangingWallShift
	cz := float64(f.z) + 0.5 - sz*hangingWallShift
	size := 0.75
	if isMapItem(f.held.item) && f.held.mapID != 0 {
		size = 1
	}
	x, y, z := size, size, size
	switch f.dir {
	case 0, 1:
		y = 0.0625
	case 2, 3:
		z = 0.0625
	default:
		x = 0.0625
	}
	return aabbOfSize(cx, cy, cz, x, y, z)
}

// paintingBox is Painting.calculateBoundingBox: the variant's size, 1/16
// deep against the wall, shifted half a block counterclockwise and up for
// an even width or height.
func (pt *painting) paintingBox() aabb {
	sx, _, sz := hangingStep(pt.dir)
	cx := float64(pt.x) + 0.5 - sx*hangingWallShift
	cy := float64(pt.y) + 0.5
	cz := float64(pt.z) + 0.5 - sz*hangingWallShift
	even := func(n int) float64 {
		if n%2 == 0 {
			return 0.5
		}
		return 0
	}
	lx, lz := facingDelta(leftOf(faceName(pt.dir)))
	cx += float64(lx) * even(pt.w)
	cz += float64(lz) * even(pt.w)
	cy += even(pt.h)
	w, ht := float64(pt.w), float64(pt.h)
	if pt.dir == 2 || pt.dir == 3 {
		return aabbOfSize(cx, cy, cz, w, ht, 0.0625)
	}
	return aabbOfSize(cx, cy, cz, 0.0625, ht, w)
}

// knotBox is LeashFenceKnotEntity.recalculateBoundingBox: 0.375 wide and
// 0.5 tall, standing 0.375 up the fence.
func (k *leashKnot) knotBox() aabb {
	x, y, z := float64(k.pos.x)+0.5, float64(k.pos.y)+0.375, float64(k.pos.z)+0.5
	return aabb{x - 0.1875, y, z - 0.1875, x + 0.1875, y + 0.5, z + 0.1875}
}

// crystalBox is the end crystal's 2 × 2 box on its feet.
func (c *crystal) crystalBox() aabb {
	return aabb{c.x - 1, c.y, c.z - 1, c.x + 1, c.y + 2, c.z + 1}
}

// fixtureOutOfReach reports a click on a frame, painting, knot or crystal
// the player cannot reach (another dimension, or beyond
// entity_interaction_range + 3); false for every other target.
func (h *hub) fixtureOutOfReach(t *tracked, eid int32) bool {
	if t == nil {
		return false
	}
	far := func(dim int, b aabb) bool { return dim != t.dim || !withinEntityAABB(t, b, interactSlack) }
	if f := h.itemFrames[eid]; f != nil {
		return far(f.dim, f.frameBox())
	}
	if pt := h.paintings[eid]; pt != nil {
		return far(pt.dim, pt.paintingBox())
	}
	if k := h.knots[eid]; k != nil {
		return far(k.dim, k.knotBox())
	}
	if c := h.crystals[eid]; c != nil {
		return far(c.dim, c.crystalBox())
	}
	if c := h.cushions[eid]; c != nil {
		return far(c.dim, c.box())
	}
	return false
}
