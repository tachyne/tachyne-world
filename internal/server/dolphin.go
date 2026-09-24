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
	if t.gamemode == gmSurvival {
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

// dolphinFollowBoat is FollowPlayerRiddenEntityGoal(AbstractBoat): a boat
// within five blocks that a player is rowing draws the dolphin in — to the
// cell behind the rower until within four blocks, then ten blocks ahead in
// the boat's line of travel until it falls twelve behind — while the boat
// keeps moving. It re-aims every ten ticks.
func (h *hub) dolphinFollowBoat(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	moving := func(v *vehicle) bool {
		return v != nil && v.isBoat() && v.rider != 0 && players[v.rider] != nil && v.dim == m.dim && now-v.movedAt <= 2
	}
	v := h.vehicles[m.followBoat]
	if !moving(v) {
		m.followBoat, v = 0, nil
		for _, c := range h.vehicles {
			if moving(c) && math.Abs(c.x-m.x) <= 5+1.4 && math.Abs(c.z-m.z) <= 5+1.4 && math.Abs(c.y-m.y) <= 5+1 {
				v = c
				break
			}
		}
		if v == nil {
			return false
		}
		m.followBoat, m.followAhead, m.followRecalc = v.eid, false, 0
	}
	t := players[v.rider]
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
	if m.followAhead {
		n := math.Hypot(v.moveDX, v.moveDZ)
		tx, tz = t.x+v.moveDX/n*10, t.z+v.moveDZ/n*10 // ten ahead in its motion direction
	} else {
		fx, _, fz := lookVector(t.yaw, 0)
		tx, tz = t.x-fx, t.z-fz // the cell behind the rower
	}
	h.steerTo(m, tx, tz, 1.0)
	return true
}
