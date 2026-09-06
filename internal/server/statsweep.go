package server

import "math"

// Custom statistics that vanilla's ServerStatsCounter keeps and this engine
// did not: the movement family (Player.checkMovementStatistics), the damage
// family (LivingEntity.actuallyHurt / Player.attack), the "interact with"
// and "inspect" counters (their menus' openers), the clocks
// (total_world_time, time_since_death, sneak_time) and a handful of
// one-shot events. Vanilla stores damage in tenths of a heart-point (×10)
// and distances in centimetres (×100); both conventions kept here.

// moveStats is Player.checkMovementStatistics for one movement update
// (called before the position is applied, so t.* is the previous point).
func (h *hub) moveStats(t *tracked, e evMove) {
	dx, dy, dz := e.x-t.x, e.y-t.y, e.z-t.z
	horiz := math.Hypot(dx, dz)
	dist3d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if dist3d >= 8 { // a teleport, not a walk
		return
	}
	cm := func(d float64) int32 { return int32(math.Round(d * 100)) }
	inWater := h.inWater(t.dim, e.x, e.y, e.z)
	headUnder := h.inWater(t.dim, e.x, e.y+1.5, e.z)
	switch {
	case headUnder && inWater:
		if d := cm(dist3d); d > 0 {
			h.incCustom(t, "walk_under_water_one_cm", d)
		}
	case inWater:
		if d := cm(horiz); d > 0 {
			h.incCustom(t, "walk_on_water_one_cm", d)
		}
	case h.onClimbable(t.dim, e.x, e.y, e.z) && dy > 0:
		if d := cm(dy); d > 0 {
			h.incCustom(t, "climb_one_cm", d)
		}
	case e.onGround:
		if d := cm(horiz); d > 0 {
			switch {
			case t.p.sneaking:
				h.incCustom(t, "crouch_one_cm", d)
			case e.sprinting:
				h.incCustom(t, "sprint_one_cm", d)
			default:
				h.incCustom(t, "walk_one_cm", d)
			}
		}
	case t.gliding():
		if d := cm(dist3d); d > 0 {
			h.incCustom(t, "aviate_one_cm", d)
		}
	case t.gamemode == gmCreative || t.gamemode == gmSpectator: // airborne in a flying mode
		if d := cm(dist3d); d > 0 {
			h.incCustom(t, "fly_one_cm", d)
		}
	default: // airborne without wings: only the drop counts
		if dy < 0 {
			h.incCustom(t, "fall_one_cm", cm(-dy))
		}
	}
	if !t.airborne && !e.onGround && dy > 0 && !inWater && t.onGround {
		h.incCustom(t, "jump", 1)
	}
}

// onClimbable reports a ladder, vine or scaffolding at the feet.
func (h *hub) onClimbable(dim int, x, y, z float64) bool {
	return isClimbable(h.worldFor(dim).At(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))))
}

var climbableRanges = blockRange("ladder", "vine", "scaffolding", "weeping_vines", "weeping_vines_plant",
	"twisting_vines", "twisting_vines_plant", "cave_vines", "cave_vines_plant")

func isClimbable(s uint32) bool { return inRanges2(s, climbableRanges) }

// rideStats credits the distance a ridden animal carried its rider.
func (h *hub) rideStats(t *tracked, m *mob, d float64) {
	if d <= 0 || d >= 8 {
		return
	}
	name := "horse_one_cm"
	switch m.etype {
	case entityPig:
		name = "pig_one_cm"
	case entityStrider:
		name = "strider_one_cm"
	case entityHappyGhast:
		name = "happy_ghast_one_cm"
	}
	h.incCustom(t, name, int32(math.Round(d*100)))
}

// vehicleStats credits boat or minecart distance.
func (h *hub) vehicleStats(t *tracked, v *vehicle, d float64) {
	if d <= 0 || d >= 8 {
		return
	}
	name := "minecart_one_cm"
	if v.isBoat() {
		name = "boat_one_cm"
	}
	h.incCustom(t, name, int32(math.Round(d*100)))
}

// tenths is vanilla's damage-stat unit: Math.round(damage × 10).
func tenths(f float32) int32 { return int32(math.Round(float64(f) * 10)) }

// evStat bumps a custom statistic from the connection side, for the few
// actions the connection resolves without the hub (potting a flower).
type evStat struct {
	eid  int32
	name string
}

func (evStat) isHubEvent() {}
