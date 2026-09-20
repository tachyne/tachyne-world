package server

import "math"

// Skeleton → stray. A skeleton left standing in powder snow freezes into a
// stray, which is the one conversion the engine never ran: powder snow was a
// soft landing and nothing more.
//
// Vanilla's Skeleton.tick keeps two clocks of its own, quite separate from
// the freeze DAMAGE every other entity takes (a skeleton's canFreeze() is
// false, so frost never hurts it and the frost overlay never fills). It
// counts up while it stands in the snow, and once that first clock is full it
// starts a second, longer one; only when THAT runs out does the skeleton
// turn. Step out of the snow at any point — before the countdown or during
// it — and both clocks are thrown away, so a half-finished conversion never
// resumes where it left off. That is the opposite of the drowning
// conversion beside it, which finishes on dry land once it has begun, and the
// difference is deliberate in both.
//
// Counted in whole seconds, like that drowning conversion, because both run
// off the one-per-second environment step.
const (
	strayFreezeSecs  = 140 / 20 // Skeleton.tick: time in the snow before the conversion starts…
	strayConvertSecs = 300 / 20 // …and TOTAL_CONVERSION_TIME once it has
)

// mobInPowderSnow is Entity.isInPowderSnow for a mob: the cell at its feet or
// the one at its body. The same two cells the player's freeze clock reads.
func (h *hub) mobInPowderSnow(m *mob) bool {
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	fx, fz, feet := int(math.Floor(m.x)), int(math.Floor(m.z)), int(math.Floor(m.y))
	return w.At(fx, feet, fz) == powderSnowBlock || w.At(fx, feet+1, fz) == powderSnowBlock
}

// strayFreezeStep runs one second of a skeleton's powder-snow clocks. It
// reports true when the skeleton has turned, in which case the caller is
// holding a mob that no longer exists and must let go of it.
func (h *hub) strayFreezeStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entitySkeleton {
		return false
	}
	if !h.mobInPowderSnow(m) {
		m.snowSecs, m.strayIn = 0, 0 // out of the snow: both clocks are dropped
		return false
	}
	if m.strayIn > 0 {
		if m.strayIn--; m.strayIn > 0 {
			return false
		}
		// SOUND_SKELETON_TO_STRAY, at the block the skeleton froze in — the
		// level event carries the crackle, so there is no sound to play here.
		h.levelEvent(players, m.dim, worldEventSkelToStray,
			int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)), 0)
		h.convertMob(players, m, entityStray)
		return true
	}
	if m.snowSecs++; m.snowSecs >= strayFreezeSecs {
		m.strayIn = strayConvertSecs
	}
	return false
}
