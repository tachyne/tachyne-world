package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The blocks whose whole effect on a walker is on the way up or down, as
// walking travel applies them each tick (mobtravel.go, mobBlockEffects):
//
//   - a bubble column (BubbleColumnBlock.entityInside) lifts whatever is in
//     it — min(0.7, dy + 0.06) a tick, and min(1.8, dy + 0.1) in the top
//     cell, where there is nothing above (onAboveBubbleColumn), which is
//     what throws a mob clear of the surface — or drags it down with
//     max(−0.3, dy − 0.03) and max(−0.9, dy − 0.03);
//   - a slime block turns a living thing's landing into a bounce of the
//     same speed (Entity.restituteMovementAfterCollisions, restitution 1),
//     and the fall costs nothing (fallOn with no damage);
//   - a cobweb (WebBlock.entityInside → makeStuckInBlock) lets a body sink
//     at 0.05 of its fall speed, resetting its fall as it goes;
//   - a honey block's side (HoneyBlock.entityInside) holds a falling body
//     to a slide of 0.05 a tick, with no fall damage at the bottom.

const (
	columnTopUpStep   = 0.1  // onAboveBubbleColumn: min(1.8, dy + 0.1)
	columnTopUpCap    = 1.8  //
	columnTopDownCap  = -0.9 // max(−0.9, dy − 0.03)
	webStuckY         = 0.05 // WebBlock: makeStuckInBlock(0.25, 0.05, 0.25)
	webStuckYWeaving  = 0.25 // …(0.5, 0.25, 0.5) under Weaving
	floatJumpChance   = 0.8  // FloatGoal: jumps on 80% of ticks
	liquidJumpStep    = 0.04 // LivingEntity.jumpInLiquid
	honeySlideSpeed   = 0.05 // HoneyBlock.THROTTLE_SLIDE_SPEED_TO
	honeySlideEventID = 53   // EntityEvent.HONEY_SLIDE: the slide particles
)

// mobStuckInWeb is makeStuckInBlock's exceptions for a cobweb: a spider
// walks a web freely, and the wither is never stuck in anything.
func mobStuckInWeb(m *mob) bool {
	return m.etype != entitySpider && m.etype != entityCaveSpider && m.etype != entityWither
}

// boxCells calls fn for every cell of the mob's column from its feet to the
// top of its box.
func boxCells(y, ht float64, fn func(cy int) bool) {
	for cy := floorInt(y); cy <= floorInt(y+ht-1e-7); cy++ {
		if fn(cy) {
			return
		}
	}
}

// inWebCell reports a cobweb anywhere in the mob's box.
func (h *hub) inWebCell(m *mob, fx, fz int) bool {
	w := h.worldFor(m.dim)
	found := false
	boxCells(m.y, m.box().h, func(cy int) bool {
		found = w.At(fx, cy, fz) == cobwebState
		return found
	})
	return found
}

// stuckResetsFall reports whether a body of height ht at (x, y, z) is caught
// by a block that resets its fall every tick through makeStuckInBlock: a
// cobweb anywhere in the box, or powder snow at its position.
func (h *hub) stuckResetsFall(dim int, x, y, z, ht float64) bool {
	w := h.worldFor(dim)
	fx, fz := floorInt(x), floorInt(z)
	if isPowderSnow(w.At(fx, floorInt(y), fz)) {
		return true
	}
	found := false
	boxCells(y, ht, func(cy int) bool {
		found = w.At(fx, cy, fz) == cobwebState
		return found
	})
	return found
}

// honeyWallAt reports whether a body of half-width hw at (x, z) is pressed
// against the side of a honey block in cell row cy — overlapping the
// neighbouring cell, but outside the block's inset face
// (HoneyBlock.isSlidingDown's overlap test).
func honeyWallAt(w *world.World, x, z float64, cy int, hw float64) bool {
	fx, fz := floorInt(x), floorInt(z)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			bx, bz := fx+dx, fz+dz
			ox, oz := math.Abs(float64(bx)+0.5-x), math.Abs(float64(bz)+0.5-z)
			if ox >= 0.5+hw || oz >= 0.5+hw {
				continue
			}
			if ox+1e-7 <= honeyFaceInset+hw && oz+1e-7 <= honeyFaceInset+hw {
				continue
			}
			if isHoneyBlock(w.At(bx, cy, bz)) {
				return true
			}
		}
	}
	return false
}

// mobOnHoneyWall reports a mob at its current height against a honey side,
// below the block's top (the 0.9375 cut-off).
func (h *hub) mobOnHoneyWall(m *mob) bool {
	w := h.worldFor(m.dim)
	hw := m.box().w / 2
	found := false
	boxCells(m.y, m.box().h, func(cy int) bool {
		found = m.y <= float64(cy)+0.9375-1e-7 && honeyWallAt(w, m.x, m.z, cy, hw)
		return found
	})
	return found
}

// playerEyeHeight is a standing player's eye height.
const playerEyeHeight = 1.62

// Blaze flight. A blaze is not a flier: it has gravity and walks, but it
// falls slowly (aiStep: a falling blaze's vertical motion ×0.6 each tick)
// and lifts itself toward a target whose eyes are more than
// allowedHeightOffset above its own — that offset re-rolled every 100 ticks
// as triangle(0.5, 6.891), so a blaze bobs rather than locking to a height
// (Blaze.customServerAiStep).
func blazeWantsLift(m *mob) bool {
	return m.hasTarget && m.ty+playerEyeHeight > m.y+mobEyeHeight(m)+m.blazeLift
}

// blazeAirTick is one tick of the blaze's own vertical rules, applied before
// the move as vanilla's aiStep and customServerAiStep do.
func (h *hub) blazeAirTick(m *mob) {
	if !m.onGround && m.vy < 0 {
		m.vy *= 0.6
	}
	if m.blazeLiftIn--; m.blazeLiftIn <= 0 {
		m.blazeLiftIn = 100
		m.blazeLift = h.triangle(0.5, 6.891)
	}
	if blazeWantsLift(m) {
		m.vy += (0.3 - m.vy) * 0.3
	}
}
