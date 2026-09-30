package server

import (
	"math"
	"math/rand"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Experience orbs move as vanilla's ExperienceOrb.tick does: gravity 0.03
// (none while the box is inside a block), the 0.98 drag on every axis with
// the floor's friction on top once it rests, a rise through water, a pop out
// of lava, a bounce at 0.4 of a fast landing, and the pull of the nearest
// player within eight blocks — an acceleration of (1 − distance/8)² × 0.1 a
// tick along the line to the middle of the player's eyes, so an orb swoops
// in from the edge of the range and overshoots a little, rather than
// gliding at a fixed speed as it did until 2026-09-30.

// motionRand is the randomness of a fresh entity's own motion — the hop an
// orb or a drop leaves with, an orb's pop out of lava — which vanilla draws
// from the entity's own random. It is kept apart from h.rng so that giving a
// drop its hop does not move every later roll of the game's dice.
func (h *hub) motionRand() *rand.Rand {
	if h.physRng == nil {
		h.physRng = rand.New(rand.NewSource(2))
	}
	return h.physRng
}

// tickOrb runs one tick of an orb's motion.
func (h *hub) tickOrb(players map[int32]*tracked, o *xpOrb) {
	w := h.worldFor(o.dim)
	if w == nil || !w.Loaded(int32(floorInt(o.x)>>4), int32(floorInt(o.z)>>4)) {
		return // vanilla ticks entities only in loaded chunks
	}
	fx, fz := floorInt(o.x), floorInt(o.z)
	colliding := orbBoxBlocked(w, fx, o.y, fz)
	switch {
	case worldgen.HoldsWater(w.At(fx, floorInt(o.y+orbEyeHeight), fz)):
		// setUnderwaterMovement: it rises slowly and drags sideways.
		o.vx *= itemFluidDrag
		o.vy = math.Min(o.vy+itemFluidLift, itemFluidLiftCap)
		o.vz *= itemFluidDrag
	case !colliding:
		o.vy -= orbGravity
	}
	if worldgen.IsLava(w.At(fx, floorInt(o.y), fz)) {
		// Lava throws it out again, in a random direction.
		r := h.motionRand()
		o.vx = float64((r.Float32() - r.Float32()) * 0.2)
		o.vy = 0.2
		o.vz = float64((r.Float32() - r.Float32()) * 0.2)
	}
	h.orbFollow(players, o)
	if o.follow == 0 && colliding &&
		orbBoxBlocked(w, floorInt(o.x+o.vx), o.y+o.vy, floorInt(o.z+o.vz)) {
		// Stuck in a block with its next move blocked too: out through the
		// nearest open side (moveTowardsClosestSpace).
		o.vx, o.vy, o.vz = h.towardsClosestSpace(w, o.x, o.y+orbHeight/2, o.z, o.vx, o.vy, o.vz)
	}

	fall := o.vy
	landed := orbMove(w, o)
	f := orbDrag
	grounded := orbGrounded(w, o)
	if grounded {
		f *= blockFriction(w.At(floorInt(o.x), floorInt(o.y)-1, floorInt(o.z)))
	}
	o.vx, o.vy, o.vz = o.vx*f, o.vy*f, o.vz*f
	if landed && fall < -orbGravity {
		o.vy = -fall * orbBounce // verticalCollisionBelow: it bounces
	}
	if grounded {
		if math.Abs(o.vx) < orbSleepSpeed {
			o.vx = 0
		}
		if math.Abs(o.vz) < orbSleepSpeed {
			o.vz = 0
		}
	}
}

// orbFollow is followNearbyPlayer: keep to the player it is flying at while
// they stay within eight blocks, else take the nearest one inside that, and
// accelerate toward the middle of their eyes. The engine only lets a
// survival player collect experience, so only a survival player draws it.
func (h *hub) orbFollow(players map[int32]*tracked, o *xpOrb) {
	canFollow := func(t *tracked) bool {
		return t != nil && isSurvival(t.gamemode) && !t.dead && t.dim == o.dim
	}
	if t := players[o.follow]; !canFollow(t) || dist3sq(t.x, t.y, t.z, o.x, o.y, o.z) > orbFollowRange*orbFollowRange {
		o.follow = 0
		best := orbFollowRange * orbFollowRange
		for k, t := range players {
			if !canFollow(t) {
				continue
			}
			if d2 := dist3sq(t.x, t.y, t.z, o.x, o.y, o.z); d2 <= best {
				o.follow, best = k, d2
			}
		}
	}
	t := players[o.follow]
	if o.follow == 0 || t == nil {
		return
	}
	dx, dy, dz := t.x-o.x, t.y+t.eyeHeight()/2-o.y, t.z-o.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-9 {
		return
	}
	power := 1 - d/orbFollowRange
	pull := power * power * orbFollowPull / d
	o.vx += dx * pull
	o.vy += dy * pull
	o.vz += dz * pull
}

// orbBoxBlocked reports a colliding block anywhere in the orb's box, with
// its feet at y, in column (x, z).
func orbBoxBlocked(w *world.World, x int, y float64, z int) bool {
	hit := false
	boxCells(y, orbHeight, func(cy int) bool {
		hit = worldgen.Collides(w.At(x, cy, z))
		return hit
	})
	return hit
}

// orbGrounded reports an orb resting exactly on the block under it.
func orbGrounded(w *world.World, o *xpOrb) bool {
	if w == nil {
		return false
	}
	fy := floorInt(o.y)
	return o.y == float64(fy) && worldgen.Collides(w.At(floorInt(o.x), fy-1, floorInt(o.z)))
}

// orbMove is Entity.move for the orb's box against the block grid, the
// vertical axis first and then each horizontal one, as Entity.collide
// resolves them: a floor or a ceiling ends the vertical speed, a wall that
// axis's. Reports whether it came down on a floor (verticalCollisionBelow).
func orbMove(w *world.World, o *xpOrb) bool {
	fx, fz := floorInt(o.x), floorInt(o.z)
	landed := false
	switch {
	case o.vy < 0:
		ny := o.y + o.vy
		for cy := floorInt(o.y) - 1; cy >= floorInt(ny); cy-- {
			if worldgen.Collides(w.At(fx, cy, fz)) {
				ny, o.vy, landed = float64(cy+1), 0, true
				break
			}
		}
		o.y = ny
	case o.vy > 0:
		ny := o.y + o.vy
		for cy := floorInt(o.y+orbHeight-1e-7) + 1; cy <= floorInt(ny+orbHeight-1e-7); cy++ {
			if worldgen.Collides(w.At(fx, cy, fz)) {
				ny, o.vy = math.Max(o.y, float64(cy)-orbHeight), 0
				break
			}
		}
		o.y = ny
	}
	if o.vx != 0 {
		if nx := o.x + o.vx; floorInt(nx) != fx && orbBoxBlocked(w, floorInt(nx), o.y, fz) {
			o.vx = 0
		} else {
			o.x = nx
		}
	}
	fx = floorInt(o.x)
	if o.vz != 0 {
		if nz := o.z + o.vz; floorInt(nz) != fz && orbBoxBlocked(w, fx, o.y, floorInt(nz)) {
			o.vz = 0
		} else {
			o.z = nz
		}
	}
	return landed
}

// orbTouches is Player.touch's reach for an orb: the player's box inflated
// by 1 sideways and 0.5 up and down, against the orb's 0.5 box.
func orbTouches(t *tracked, o *xpOrb) bool {
	reach := playerWidth(t)/2 + 1 + orbHalfWidth
	return math.Abs(o.x-t.x) < reach && math.Abs(o.z-t.z) < reach &&
		o.y+orbHeight > t.y-0.5 && o.y < t.y+playerHeight(t)+0.5
}
