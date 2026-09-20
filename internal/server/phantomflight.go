package server

import "math"

// The phantom's flight. Vanilla never flies one at you directly: it circles
// an anchor above its target at a radius of five to fifteen blocks, and
// every eight to twelve seconds it swoops — dropping to the target's level,
// striking, and climbing back to circle again. The circling, the pause and
// the dive are the whole shape of the encounter, and a phantom that simply
// hovered over you had none of it.

const (
	phantomCircleMin  = 5.0 // PhantomCircleAroundAnchorGoal: 5 + rand(10)
	phantomCircleSpan = 10.0
	phantomAnchorLow  = 10.0 // the anchor sits 10 + rand(20) above the target
	phantomAnchorSpan = 20.0
	phantomFirstSweep = 10 // the first swoop comes ten ticks in
	phantomSweepMin   = 8  // …then every 8 + rand(4) seconds
	phantomSweepSpan  = 4
	phantomSwoopTicks = 60   // a swoop lasts until it passes the target (or this long)
	phantomSwoopSpeed = 1.75 // …and it dives faster than it circles
)

// phantomFlightBehavior is the circle-and-swoop steering.
type phantomFlightBehavior struct{}

func (phantomFlightBehavior) name() string { return "phantom-flight" }

func (phantomFlightBehavior) steer(h *hub, m *mob) (float64, float64) {
	if !m.hasTarget {
		m.phantomSwoop = 0
		return wanderBehavior{}.steer(h, m)
	}
	if m.phantomRadius == 0 { // a fresh anchor: its own radius, height and spin
		m.phantomRadius = phantomCircleMin + h.rng.Float64()*phantomCircleSpan
		m.phantomHigh = phantomAnchorLow + h.rng.Float64()*phantomAnchorSpan
		m.phantomCW = h.rng.Intn(2) == 0
		m.phantomNext = phantomFirstSweep
	}
	sp := m.moveSpeed()
	if m.phantomSwoop > 0 { // SWOOP: straight at it, and faster
		m.phantomSwoop -= mobMoveInterval
		dx, dz := m.tx-m.x, m.tz-m.z
		if d := math.Hypot(dx, dz); d > 0.5 {
			return dx / d * sp * phantomSwoopSpeed, dz / d * sp * phantomSwoopSpeed
		}
		m.phantomSwoop = 0
		return 0, 0
	}
	if m.phantomNext -= mobMoveInterval; m.phantomNext <= 0 {
		m.phantomNext = (phantomSweepMin + h.rng.Intn(phantomSweepSpan)) * 20
		m.phantomSwoop = phantomSwoopTicks
		h.playSoundDim(h.playersRef, m.dim, "minecraft:entity.phantom.swoop", sndHostile,
			m.x, m.y, m.z, 10, 0.95+h.rng.Float32()*0.1)
		return 0, 0
	}
	// CIRCLE: hold the radius around the anchor and go round it.
	dx, dz := m.x-m.tx, m.z-m.tz
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return sp, 0
	}
	// Tangent, plus a pull in or out to keep the radius.
	tx, tz := -dz/d, dx/d
	if !m.phantomCW {
		tx, tz = -tx, -tz
	}
	rx, rz := dx/d*(m.phantomRadius-d)*0.2, dz/d*(m.phantomRadius-d)*0.2
	vx, vz := tx+rx, tz+rz
	if n := math.Hypot(vx, vz); n > 1e-6 {
		vx, vz = vx/n*sp, vz/n*sp
	}
	return vx, vz
}

// phantomAltitude is the height the flight wants: high over the target while
// it circles, down at the target while it swoops.
func (m *mob) phantomAltitude() (float64, bool) {
	if m.etype != entityPhantom || !m.hasTarget || m.ty == 0 {
		return 0, false
	}
	if m.phantomSwoop > 0 {
		return m.ty, true
	}
	high := m.phantomHigh
	if high == 0 {
		high = phantomAnchorLow
	}
	return m.ty + high, true
}
