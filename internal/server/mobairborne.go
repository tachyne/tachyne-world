package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The vertical integrator for walkers. A walking mob is normally seated on
// the floor under it every update — a drop is one step, not a flight. That
// is enough for a ledge, but not for the blocks whose whole effect is on
// the way up or down:
//
//   - a bubble column (BubbleColumnBlock.entityInside) lifts whatever is in
//     it — min(0.7, dy + 0.06) a tick, and min(1.8, dy + 0.1) in the top
//     cell, where there is nothing above (onAboveBubbleColumn), which is
//     what throws a mob clear of the surface — or drags it down with
//     max(−0.3, dy − 0.03) and max(−0.9, dy − 0.03);
//   - a slime block (SlimeBlock.updateEntityMovementAfterFallOn) turns a
//     living thing's landing into a bounce of the same speed, and the fall
//     costs nothing (fallOn with no damage);
//   - a cobweb (WebBlock.entityInside → makeStuckInBlock) lets a body sink
//     at 0.05 of its fall speed, resetting its fall as it goes;
//   - a honey block's side (HoneyBlock.entityInside) holds a falling body
//     to a slide of 0.05 a tick, with no fall damage at the bottom.
//
// A mob in any of those runs here, tick by tick, the way LivingEntity.travel
// moves it: the move, then drag and gravity (0.98 in air; 0.8 and a
// sixteenth of gravity in water), then the block effects. It leaves the
// integrator when it lands on something that is not slime, or when it is
// back in still water with nothing lifting it.

const (
	columnTopUpStep   = 0.1  // onAboveBubbleColumn: min(1.8, dy + 0.1)
	columnTopUpCap    = 1.8  //
	columnTopDownCap  = -0.9 // max(−0.9, dy − 0.03)
	webStuckY         = 0.05 // WebBlock: makeStuckInBlock(0.25, 0.05, 0.25)
	webStuckYWeaving  = 0.25 // …(0.5, 0.25, 0.5) under Weaving
	mobWaterDragY     = 0.8  // LivingEntity.travelInFluid: vertical × 0.8 in water
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

// inBubbleColumn reports a bubble column anywhere in the mob's box.
func (h *hub) inBubbleColumn(m *mob, fx, fz int) bool {
	w := h.worldFor(m.dim)
	found := false
	boxCells(m.y, m.box().h, func(cy int) bool {
		found = worldgen.IsBubbleColumn(w.At(fx, cy, fz))
		return found
	})
	return found
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

// fallAfterResets is the part of a drop from fromY to toY that still counts
// as a fall: every block on the way that resets the fall distance — water
// (and a bubble column), a cobweb, powder snow, a honey side — leaves only
// what lies below the lowest of them. The body is still touching a cell
// until its feet are a body's height below it.
func (h *hub) fallAfterResets(m *mob, fx, fz int, fromY, toY float64) float64 {
	fell := fromY - toY
	if fell <= 0 {
		return 0
	}
	w := h.worldFor(m.dim)
	ht, hw := m.box().h, m.box().w/2
	for cy := floorInt(toY); cy <= floorInt(fromY+ht-1e-7); cy++ {
		s := w.At(fx, cy, fz)
		reset := worldgen.HoldsWater(s) || isPowderSnow(s) ||
			(s == cobwebState && mobStuckInWeb(m)) || honeyWallAt(w, m.x, m.z, cy, hw)
		if reset {
			return math.Max(0, math.Min(fell, float64(cy)-ht-toY))
		}
	}
	return fell
}

// mobAirborneStep runs the integrator for one mob update, if the mob needs
// it: already in the air on it, or in a bubble column, or caught in a web
// or against a honey wall with a drop beneath. Reports whether it moved the
// mob's height (the caller then leaves the floor alone).
func (h *hub) mobAirborneStep(players map[int32]*tracked, m *mob, fx, fz int, floor float64) bool {
	if !m.airborne {
		switch {
		case h.inBubbleColumn(m, fx, fz):
		case floor < m.y-1e-9 && mobStuckInWeb(m) && h.inWebCell(m, fx, fz):
		case floor < m.y-1e-9 && h.mobOnHoneyWall(m):
		case m.etype == entityBlaze && (floor < m.y-1e-9 || blazeWantsLift(m)):
		default:
			return false
		}
		m.airborne, m.vy = true, 0
	}
	for i := 0; i < mobMoveInterval && m.airborne; i++ {
		h.mobAirTick(players, m, fx, fz)
	}
	return true
}

// mobAirTick is one tick of it.
func (h *hub) mobAirTick(players map[int32]*tracked, m *mob, fx, fz int) {
	w := h.worldFor(m.dim)
	ht := m.box().h
	g := m.gravity()
	feet := floorInt(m.y)
	inWater := worldgen.HoldsWater(w.At(fx, feet, fz))

	// FloatGoal: a floater jumps against deep water.
	if inWater && mobFloats(m) {
		top := feet
		for worldgen.HoldsWater(w.At(fx, top+1, fz)) {
			top++
		}
		thr := fluidJumpThresh
		if mobEyeHeight(m) < fluidJumpThresh {
			thr = 0
		}
		if float64(top+1)-m.y > thr && h.rng.Float64() < floatJumpChance {
			m.vy += liquidJumpStep
		}
	}

	if m.etype == entityBlaze {
		h.blazeAirTick(m)
	}

	// The move, cut by a web's stuck multiplier.
	stuck := 1.0
	if mobStuckInWeb(m) && h.inWebCell(m, fx, fz) {
		stuck = webStuckY
		if m.hasEffect(effWeaving) > 0 {
			stuck = webStuckYWeaving
		}
	}
	dy := m.vy * stuck
	ny := m.y + dy
	landed := false
	switch {
	case dy < 0:
		if floor := float64(h.mobFeetAt(m, fx, fz, feet)); ny <= floor {
			ny, landed = floor, true
		}
		m.airFall += m.y - ny
	case dy > 0:
		if top := floorInt(ny + ht - 1e-7); top > floorInt(m.y+ht-1e-7) && worldgen.Collides(w.At(fx, top, fz)) {
			ny = math.Max(m.y, float64(top)-ht) // a ceiling stops the rise
			m.vy = 0
		}
	}
	m.y = ny
	if stuck != 1 {
		m.vy, m.airFall = 0, 0 // makeStuckInBlock: the motion is spent, the fall reset
	}
	if landed {
		if isSlimeBlock(w.At(fx, floorInt(m.y)-1, fz)) && m.vy < 0 {
			m.vy = -m.vy  // SlimeBlock: a living thing bounces back at full speed…
			m.airFall = 0 // …and the fall costs nothing
		} else {
			h.mobLanded(players, m, m.airFall)
			return
		}
	}

	// travel's drag and gravity.
	if worldgen.HoldsWater(w.At(fx, floorInt(m.y), fz)) {
		m.vy = m.vy*mobWaterDragY - g/16
	} else {
		m.vy = (m.vy - g) * 0.98
	}
	if landed && m.vy <= 0 {
		h.mobLanded(players, m, 0) // too slow to leave the slime again: it rests on it
		return
	}

	// The block effects of where it now is.
	inColumn := false
	boxCells(m.y, ht, func(cy int) bool {
		s := w.At(fx, cy, fz)
		if !worldgen.IsBubbleColumn(s) {
			return false
		}
		inColumn = true
		above := w.At(fx, cy+1, fz)
		top := !worldgen.Collides(above) && !worldgen.HoldsWater(above) && !worldgen.IsLava(above)
		switch {
		case s == worldgen.BubbleColumnDrag && top:
			m.vy = math.Max(columnTopDownCap, m.vy-columnDownStep)
		case s == worldgen.BubbleColumnDrag:
			m.vy = math.Max(columnDownCap, m.vy-columnDownStep)
		case top:
			m.vy = math.Min(columnTopUpCap, m.vy+columnTopUpStep)
		default:
			m.vy = math.Min(columnUpCap, m.vy+columnUpStep)
		}
		return false
	})
	if m.vy/0.98+g < -honeySlideMinFall && h.mobOnHoneyWall(m) {
		// HoneyBlock.doSlideMovement: the fall is held to a slide.
		m.vy = (-honeySlideSpeed - g) * 0.98
		m.airFall = 0
		if h.rng.Intn(5) == 0 {
			h.playSoundDim(players, m.dim, "minecraft:block.honey_block.slide", sndBlock, m.x, m.y, m.z, 1, 1)
		}
		if h.rng.Intn(5) == 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, honeySlideEventID))
		}
	}
	nowWet := worldgen.HoldsWater(w.At(fx, floorInt(m.y), fz))
	if nowWet {
		m.airFall = 0 // in water the fall is forgotten
	}
	if !inColumn && nowWet && m.vy <= 0 {
		// Plain water again, nothing lifting it: the float goal and the
		// floor take it back.
		m.airborne, m.vy = false, 0
	}
}

// mobLanded ends a flight on the integrator: the landing's effects, then
// back to walking.
func (h *hub) mobLanded(players map[int32]*tracked, m *mob, fall float64) {
	m.airborne, m.vy, m.airFall = false, 0, 0
	if fall > 0.5 {
		h.mobTrample(players, m, fall) // FarmlandBlock.fallOn
	}
	if fall > 0 {
		h.mobFallOnEgg(players, m) // TurtleEggBlock.fallOn
	}
	if fall > m.safeFallDistance() {
		h.mobFall(players, m, fall)
	}
}

// slimeLaunch is the bounce off a slime block at the end of a seated drop
// of `fall` blocks: the speed the fall would have reached (gravity with the
// 0.98 air drag), turned round, less the tick's gravity. Zero when it would
// not leave the block.
func slimeLaunch(fall, g float64) float64 {
	v, d := 0.0, 0.0
	for d < fall && v > -4 {
		v = (v - g) * 0.98
		d -= v
	}
	return math.Max(0, (-v-g)*0.98)
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
	if m.vy < 0 {
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
