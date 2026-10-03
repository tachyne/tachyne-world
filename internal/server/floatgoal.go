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
// the water, so a cow that fell in a pond drowned in fifteen seconds. Walking
// travel (mobtravel.go) now does it as vanilla does: the jump in water is
// +0.04 a tick against travelInWater's drag and a sixteenth of gravity.

const (
	fluidJumpThresh = 0.4 // LivingEntity.getFluidJumpThreshold (0 for eyes under 0.4)
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
	entitySkeletonHorse: true, // SkeletonHorse.addBehaviourGoals is empty: no FloatGoal
}

// mobFloats reports whether a walker bobs up in deep water.
func mobFloats(m *mob) bool {
	return !m.flies && !m.swims && !mobSinks[m.etype] && !waterGlider(m.etype) // the gliders hold their depth
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
