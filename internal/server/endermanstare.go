package server

import "math"

// What an enderman does about the player looking at it. Vanilla's
// EndermanLookForPlayerGoal is a staring contest with rules: while you have
// it in your crosshair it stops where it is and holds your gaze, it blinks
// away if you close to within four blocks, and if you back off past sixteen
// it spends thirty ticks and then blinks TOWARDS you. Daylight it simply
// cannot stand.

const (
	endermanStareFreeze  = 256.0 // EndermanFreezeWhenLookedAt: within 16 blocks
	endermanStareBlink   = 16.0  // …but inside 4 it blinks away (distanceToSqr < 16)
	endermanChaseBlink   = 256.0 // beyond 16 it closes the gap with a blink
	endermanChaseDelay   = 30    // …after thirty ticks of not being looked at
	endermanDayLightMin  = 0.5   // getLightLevelDependentMagicValue > 0.5
	endermanDaySettleMin = 600   // …and 600 ticks since the target last changed
)

// endermanStareStep runs once per mob update: the freeze, the two blinks and
// the daylight flight. Reports whether the enderman is held in place by a
// stare (the caller skips its movement if so).
func (h *hub) endermanStareStep(players map[int32]*tracked, m *mob) bool {
	if m.dying != 0 {
		return false
	}
	// customServerAiStep: bright sky over an enderman that has been idle for
	// half a minute and it is gone — which is why you see them at night.
	if m.dim == 0 && m.anger == 0 && h.isDayTime() && m.settled >= endermanDaySettleMin &&
		h.lightMagic(m) > endermanDayLightMin && h.skyExposed(m) {
		if f := h.lightMagic(m); h.rng.Float64()*30 < (f-0.4)*2 {
			m.hasTarget, m.targetEID = false, 0
			h.endermanTeleport(players, m)
			return false
		}
	}
	m.settled += mobMoveInterval

	starer := h.starerOf(players, m)
	if starer == nil {
		// Out past sixteen blocks and no longer watched: close the distance.
		if m.hasTarget && m.targetEID != 0 {
			if t := players[m.targetEID]; t != nil && distSq(m, t) > endermanChaseBlink {
				if m.stareTicks += mobMoveInterval; m.stareTicks >= endermanChaseDelay {
					m.stareTicks = 0
					h.endermanBlinkToward(players, m, t)
				}
				return false
			}
		}
		m.stareTicks = 0
		return false
	}
	m.stareTicks = 0
	if distSq(m, starer) < endermanStareBlink {
		h.endermanTeleport(players, m) // too close under that gaze
		return false
	}
	// Held: the enderman stops and stares back while you hold it in view.
	return distSq(m, starer) <= endermanStareFreeze
}

// starerOf is the player currently staring at this enderman, or nil.
func (h *hub) starerOf(players map[int32]*tracked, m *mob) *tracked {
	if m.etype != entityEnderman {
		return nil
	}
	for _, t := range players {
		if t.dim != m.dim || !isSurvival(t.gamemode) || t.dead || t.armor[0].item == itemCarvedPumpkin {
			continue
		}
		ex, ey, ez := m.x-t.x, (m.y+2.55)-(t.y+1.62), m.z-t.z
		d := math.Sqrt(ex*ex + ey*ey + ez*ez)
		if d < 1e-6 || d > m.followRange() {
			continue
		}
		yawR, pitchR := float64(t.yaw)*math.Pi/180, float64(t.pitch)*math.Pi/180
		vx := -math.Sin(yawR) * math.Cos(pitchR)
		vy := -math.Sin(pitchR)
		vz := math.Cos(yawR) * math.Cos(pitchR)
		if (vx*ex+vy*ey+vz*ez)/d > 1-0.025/d {
			return t
		}
	}
	return nil
}

// distSq is the squared distance from a mob to a player.
func distSq(m *mob, t *tracked) float64 {
	dx, dy, dz := m.x-t.x, m.y-t.y, m.z-t.z
	return dx*dx + dy*dy + dz*dz
}

// endermanBlinkToward is EnderMan.teleportTowards: a hop that lands the
// enderman most of the way to its target rather than anywhere at all.
func (h *hub) endermanBlinkToward(players map[int32]*tracked, m *mob, t *tracked) {
	dx, dz := m.x-t.x, m.z-t.z
	d := math.Sqrt(dx*dx + dz*dz)
	if d < 1e-6 {
		return
	}
	// The vanilla vector: normalise, then step 16 blocks along it with a
	// little jitter, landing on the far side of the target.
	nx, nz := dx/d, dz/d
	tx := m.x + (h.rng.Float64()-0.5)*8 - nx*16
	tz := m.z + (h.rng.Float64()-0.5)*8 - nz*16
	ty := m.y + float64(h.rng.Intn(16)-8)
	h.endermanTeleportTo(players, m, tx, ty, tz)
}
