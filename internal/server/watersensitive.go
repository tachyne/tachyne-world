package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Water-sensitive mobs — the enderman, the blaze, the strider and the snow
// golem (isSensitiveToWater). LivingEntity.aiStep hurts them for 1 as drown
// damage every tick they are in water or rain; the hurt cooldown lets one
// through every ten ticks. An enderman's hurtServer also tries a random
// teleport on nine hurts in ten that no living thing dealt, so it is gone
// from water or rain almost at once, a little the worse for it.

const wetHurtTicks = 10 // one damage per ten ticks: the hurt cooldown's half

func waterSensitive(etype int) bool {
	switch etype {
	case entityEnderman, entityBlaze, entityStrider, entitySnowGolem:
		return true
	}
	return false
}

// mobWet is Entity.isInWaterOrRain: water at the body, or rain falling on
// the block it stands in or the one above.
func (h *hub) mobWet(m *mob) bool {
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	fx, fy, fz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if worldgen.HoldsWater(w.At(fx, fy, fz)) ||
		worldgen.HoldsWater(w.At(fx, floorInt(m.y+mobEyeHeight(m)), fz)) {
		return true
	}
	return m.dim == dimOverworld && h.raining && (h.isRainingAt(fx, fy, fz) || h.isRainingAt(fx, fy+1, fz))
}

// waterSensitiveTick runs once per mob update (mobMoveInterval ticks) for a
// water-sensitive mob. It reports whether the mob left or died, so the
// caller skips the rest of its update.
func (h *hub) waterSensitiveTick(players map[int32]*tracked, m *mob) bool {
	if m.wetHurt > 0 {
		m.wetHurt -= mobMoveInterval
	}
	if m.dying > 0 || !h.mobWet(m) {
		return false
	}
	if m.wetHurt <= 0 {
		m.wetHurt = wetHurtTicks
		h.hurtMobOf(players, m, 1, dtDrown)
		if m.health <= 0 || h.mobs[m.eid] == nil {
			return true
		}
	}
	if m.etype == entityEnderman {
		// Every tick in the wet is a hurt; each one rolls the teleport.
		ox, oz := m.x, m.z
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Intn(10) != 0 && h.endermanTeleport(players, m) {
				break
			}
		}
		return math.Abs(m.x-ox)+math.Abs(m.z-oz) > 0
	}
	return false
}
