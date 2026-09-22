package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Floating. Vanilla's land mobs mostly carry a FloatGoal (or the brain's
// Swim behaviour): in water deeper than the fluid jump threshold they jump
// against the water every tick and bob at the surface with their eyes
// clear of it, swimming across rather than walking the bottom — a cow, a
// creeper, a villager, an illager. The ones registered without it sink and
// walk the bed: the zombie and skeleton families, piglins, hoglins, the
// iron golem, the creaking. tachyne seated every walker on the floor under
// the water, so a cow that fell in a pond drowned in fifteen seconds.

const (
	floatEyeAbove   = 0.1  // the eyes ride this far above the surface
	floatRisePerUpd = 0.15 // how fast a sunk floater comes up (jumpInLiquid +0.04/tick against gravity)
	fluidJumpThresh = 0.4  // LivingEntity.getFluidJumpThreshold (0 for eyes under 0.4)
)

// mobSinks are the walkers vanilla gives no FloatGoal or Swim behaviour.
// A cube and a creaking do NOT belong here: Slime carries a SlimeFloatGoal
// (and MagmaCube inherits it), and the creaking's brain opens with Swim, so
// all three bob rather than walking the bottom.
var mobSinks = map[int]bool{
	entityZombie: true, entityHusk: true, entityDrowned: true, entityZombieVillager: true,
	entitySkeleton: true, entityStray: true, entityBogged: true, entityWitherSkeleton: true,
	entityPiglin: true, entityPiglinBrute: true, entityZombifiedPiglin: true,
	entityHoglin: true, entityZoglin: true, entityIronGolem: true,
	entityShulker: true, entityWither: true,
	entityEnderDragon: true, entityStrider: true,
}

// mobFloats reports whether a walker bobs up in deep water.
func mobFloats(m *mob) bool {
	return !m.flies && !m.swims && !mobSinks[m.etype]
}

// floatLevel is where a floater's feet ride in the water column standing
// on floor (the seated feet height): the surface less the eye height plus a
// little, never below the floor. false when it is not in water past the
// jump threshold (shallow water is waded).
func (h *hub) floatLevel(m *mob, fx, fz int, floor float64) (float64, bool) {
	w := h.worldFor(m.dim)
	bottom := int(math.Floor(floor))
	if !worldgen.IsWater(w.At(fx, bottom, fz)) {
		return 0, false
	}
	top := bottom
	for worldgen.IsWater(w.At(fx, top+1, fz)) {
		top++
	}
	thr := fluidJumpThresh
	if eye := mobEyeHeight(m); eye < fluidJumpThresh {
		thr = 0
	}
	if float64(top+1)-floor <= thr {
		return 0, false
	}
	y := float64(top+1) - mobEyeHeight(m) + floatEyeAbove
	if y <= floor {
		return 0, false // too shallow to lift it: it wades along the bed
	}
	return y, true
}

// floatSwims reports whether a floater at its own level can move into the
// next cell through water: the water it floats in is the floor it steps on.
func (h *hub) floatSwims(m *mob, fnx, fnz int) bool {
	if !mobFloats(m) {
		return false
	}
	w := h.worldFor(m.dim)
	cy := int(math.Floor(m.y))
	return worldgen.IsWater(w.At(int(math.Floor(m.x)), cy, int(math.Floor(m.z)))) && worldgen.IsWater(w.At(fnx, cy, fnz))
}
