package server

import (
	"math"
	"sort"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The phantom's flight. Vanilla never flies one at you directly: it circles
// an anchor above its target at a radius of five to fifteen blocks, and
// every eight to twelve seconds it swoops — dropping to the target's level,
// striking, and climbing back to circle again. The circling, the pause and
// the dive are the whole shape of the encounter, and a phantom that simply
// hovered over you had none of it.

const (
	phantomCircleMin  = 5.0 // PhantomCircleAroundAnchorGoal: 5 + rand(10)
	phantomCircleSpan = 10.0
	phantomAnchorLow  = 20.0 // setAnchorAboveTarget: 20 + rand(20) above the target
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
		m.phantomHigh = phantomAnchorLow + float64(h.rng.Intn(int(phantomAnchorSpan)))
		m.phantomHeight = -4 + h.rng.Float64()*9
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
		m.phantomHigh = phantomAnchorLow + float64(h.rng.Intn(int(phantomAnchorSpan))) // a fresh anchor each swoop
		h.playSoundDim(h.playersRef, m.dim, "minecraft:entity.phantom.swoop", sndHostile,
			m.x, m.y, m.z, 10, 0.95+h.rng.Float32()*0.1)
		return 0, 0
	}
	// CIRCLE: hold the radius around the anchor and go round it. Now and
	// then PhantomCircleAroundAnchorGoal re-rolls its height (one tick in
	// 350) and widens the circle a block (one in 250), snapping back to
	// five and turning the other way past fifteen.
	if h.rng.Intn(350/mobMoveInterval) == 0 {
		m.phantomHeight = -4 + h.rng.Float64()*9
	}
	if h.rng.Intn(250/mobMoveInterval) == 0 {
		if m.phantomRadius++; m.phantomRadius > phantomCircleMin+phantomCircleSpan {
			m.phantomRadius, m.phantomCW = phantomCircleMin, !m.phantomCW
		}
	}
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
	// …and never below the sea: the anchor floors at sea level + 1; the
	// circle rides its height offset above that.
	return math.Max(m.ty+high, float64(worldgen.SeaLevel+1)) + m.phantomHeight, true
}

// phantomTarget is PhantomAttackPlayerTargetGoal: every sixty ticks the
// phantom looks for players in its box grown 16 sideways and 64 up and
// down (within 64 blocks), highest first, and takes the first it can
// attack — in sight, as TargetingConditions.DEFAULT asks; it keeps that one
// only while it still can.
func (h *hub) phantomTarget(players map[int32]*tracked, m *mob) *tracked {
	if t := players[m.targetEID]; t != nil && isSurvival(t.gamemode) && !t.dead && t.dim == m.dim &&
		dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= 64 && h.mobSees(m, t) {
		return t
	}
	m.targetEID = 0
	if m.phantomScan -= mobMoveInterval; m.phantomScan > 0 {
		return nil
	}
	m.phantomScan = 60
	b := m.box()
	var cands []*tracked
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead || t.dim != m.dim {
			continue
		}
		if math.Abs(t.x-m.x) > 16+b.w/2+0.3 || math.Abs(t.z-m.z) > 16+b.w/2+0.3 ||
			t.y+1.8 < m.y-64 || t.y > m.y+b.h+64 || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > 64 {
			continue
		}
		cands = append(cands, t)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].y > cands[j].y })
	for _, t := range cands {
		if h.mobSees(m, t) {
			m.targetEID = t.p.eid
			return t
		}
	}
	return nil
}
