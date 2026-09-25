package server

// Dolphins lead to treasure (vanilla Dolphin.mobInteract +
// DolphinSwimToTreasureGoal): fed a fish, a dolphin takes a bearing on
// the nearest shipwreck within fifty chunks (vanilla's #dolphin_located is
// shipwrecks and ocean ruins; the engine's oceans hold wrecks) and swims
// for it, giving up the errand once within four blocks — or at once when
// there is nothing within reach.

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const (
	dolphinTreasureRadius = 50 * 16
	dolphinArrive         = 4.0
	dolphinLeadSpeed      = 1.3
)

var fishItems = func() map[int32]bool {
	out := map[int32]bool{}
	for _, n := range []string{"cod", "salmon", "tropical_fish", "pufferfish"} { // #minecraft:fishes
		if id, ok := itemByName[n]; ok {
			out[int32(id)] = true
		}
	}
	return out
}()

// tryFeedDolphin is Dolphin.mobInteract: a fish sets it off after treasure.
func (h *hub) tryFeedDolphin(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityDolphin || m.dying > 0 || !fishItems[heldStack(t).item] {
		return false
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
	if isSurvival(t.gamemode) {
		h.consumeHeld(t)
	}
	if m.baby {
		return true // a calf just eats
	}
	if x, z, ok := h.worldFor(m.dim).Gen().NearestDolphinTreasure(int(m.x), int(m.z), dolphinTreasureRadius); ok && m.dim == 0 {
		m.gotFish, m.treasureX, m.treasureZ = true, x, z
		h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.play", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
	return true
}

// dolphinStep swims a fed dolphin toward its wreck.
func (h *hub) dolphinStep(players map[int32]*tracked, m *mob) bool {
	if !m.gotFish {
		return false
	}
	tx, tz := float64(m.treasureX)+0.5, float64(m.treasureZ)+0.5
	dx, dz := tx-m.x, tz-m.z
	if dx*dx+dz*dz <= dolphinArrive*dolphinArrive {
		m.gotFish = false
		return false
	}
	h.steerTo(m, tx, tz, dolphinLeadSpeed)
	return true
}

// dolphinJumpOdds is DolphinJumpGoal's interval, reducedTickDelay(10), as a
// one-in-N chance per goal update.
const dolphinJumpOdds = 5

// dolphinJumpSteps are DolphinJumpGoal.STEPS_TO_CHECK: the cells along its
// heading that must be open water with open air above.
var dolphinJumpSteps = [...]int{0, 1, 4, 5, 6, 7}

// dolphinJumpStart is DolphinJumpGoal: now and then a dolphin swimming at
// the surface, with clear water ahead and open air over it, leaps — 0.6
// forward and 0.7 up, arcing back into the sea with a splash. Reports
// whether it jumped (the leap then carries it, leapFlight).
func (h *hub) dolphinJumpStart(players map[int32]*tracked, m *mob) bool {
	if m.leaping || h.rng.Intn(dolphinJumpOdds) != 0 {
		return false
	}
	w := h.worldFor(m.dim)
	if w == nil || !worldgen.HoldsWater(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
		return false
	}
	// getMotionDirection: the horizontal axis it is mostly moving along.
	sx, sz := 0, 0
	switch {
	case math.Abs(m.vx) >= math.Abs(m.vz) && m.vx != 0:
		sx = int(math.Copysign(1, m.vx))
	case m.vz != 0:
		sz = int(math.Copysign(1, m.vz))
	default:
		return false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for _, i := range dolphinJumpSteps {
		x, z := bx+sx*i, bz+sz*i
		if st := w.At(x, by, z); !worldgen.HoldsWater(st) || worldgen.Collides(st) {
			return false // waterIsClear: water, and nothing in #blocks_dolphin_jump
		}
		if w.At(x, by+1, z) != worldgen.Air || w.At(x, by+2, z) != worldgen.Air {
			return false // surfaceIsClear
		}
	}
	m.leaping = true
	m.leapVX, m.leapVY, m.leapVZ = float64(sx)*0.6, 0.7, float64(sz)*0.6
	h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.jump", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// dolphinFollowBoat is Dolphin's two FollowPlayerRiddenEntityGoals
// (AbstractBoat and AbstractNautilus): a boat or a nautilus within five
// blocks that a player is steering draws the dolphin in — to the cell
// behind the rider until within four blocks, then ten blocks ahead in the
// line of travel until it falls twelve behind — while it keeps moving. It
// re-aims every ten ticks.
func (h *hub) dolphinFollowBoat(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	// ridden is the followed entity as the goal sees it: where it is, the
	// way it last moved, and the player steering it, if it moved within
	// the last two ticks (hasMovedHorizontallyRecently).
	type ridden struct {
		x, y, z, dx, dz float64
		rider           *tracked
	}
	moving := func(eid int32) (ridden, bool) {
		if v := h.vehicles[eid]; v != nil {
			if v.isBoat() && v.rider != 0 && players[v.rider] != nil && v.dim == m.dim && now-v.movedAt <= 2 {
				return ridden{v.x, v.y, v.z, v.moveDX, v.moveDZ, players[v.rider]}, true
			}
			return ridden{}, false
		}
		if n := h.mobs[eid]; n != nil && nautilusKind(n.etype) && n.rider != 0 && players[n.rider] != nil &&
			n.dim == m.dim && n.dying == 0 && now-n.rideMovedAt <= 2 {
			return ridden{n.x, n.y, n.z, n.rideDX, n.rideDZ, players[n.rider]}, true
		}
		return ridden{}, false
	}
	near := func(x, y, z, hw, hh float64) bool {
		return math.Abs(x-m.x) <= 5+hw && math.Abs(z-m.z) <= 5+hw && math.Abs(y-m.y) <= 5+hh
	}
	r, ok := moving(m.followBoat)
	if !ok {
		m.followBoat = 0
		for _, c := range h.vehicles {
			if cr, ok := moving(c.eid); ok && near(c.x, c.y, c.z, 1.4, 1) {
				m.followBoat, r = c.eid, cr
				break
			}
		}
		if m.followBoat == 0 {
			h.grid().nearby(m.dim, m.x, m.z, 8, func(o *mob) {
				if m.followBoat != 0 || !nautilusKind(o.etype) {
					return
				}
				if cr, ok := moving(o.eid); ok && near(o.x, o.y, o.z, o.box().w/2+0.5, 1) {
					m.followBoat, r = o.eid, cr
				}
			})
		}
		if m.followBoat == 0 {
			return false
		}
		m.followAhead, m.followRecalc = false, 0
	}
	t := r.rider
	if m.followRecalc--; m.followRecalc <= 0 {
		m.followRecalc = 5 // adjustedTickDelay(10), in mob updates
		d := dist3(m.x, m.y, m.z, t.x, t.y, t.z)
		if !m.followAhead && d < 4 {
			m.followAhead, m.followRecalc = true, 0
		} else if m.followAhead && d > 12 {
			m.followAhead, m.followRecalc = false, 0
		}
	}
	var tx, tz float64
	if n := math.Hypot(r.dx, r.dz); m.followAhead && n > 1e-9 {
		tx, tz = t.x+r.dx/n*10, t.z+r.dz/n*10 // ten ahead in its motion direction
	} else {
		fx, _, fz := lookVector(t.yaw, 0)
		tx, tz = t.x-fx, t.z-fz // the cell behind the rider
	}
	h.steerTo(m, tx, tz, 1.0)
	return true
}
