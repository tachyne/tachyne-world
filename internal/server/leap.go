package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// LeapAtTargetGoal. A spider, wolf, cat, ocelot or fox with a target two to
// four blocks off, on the ground, springs at it one tick in five: its motion
// becomes the direction to the target at 0.4 plus a fifth of what it had,
// with an upward kick of 0.4 (0.3 for the cats), and the goal holds until it
// lands. tachyne's spiders and wolves only ever walked up to you.

const (
	leapMinSq   = 4.0  // canUse: distanceToSqr in [4, 16]
	leapMaxSq   = 16.0 //
	leapSpeed   = 0.4  // start: normalize().scale(0.4)
	leapCarry   = 0.2  // …add(deltaMovement.scale(0.2))
	leapRollOdd = 5    // nextInt(reducedTickDelay(5)) == 0
)

// leapKick is the goal's yd per species (0 = no such goal).
func leapKick(etype int) float64 {
	switch etype {
	case entitySpider, entityCaveSpider, entityWolf, entityFox:
		return 0.4
	case entityCat, entityOcelot:
		return 0.3
	}
	return 0
}

// leapTarget is getTarget for the leap: the player a hostile hunts (or an
// angry wolf/fox goes for), else the mob a hunter is after.
func (h *hub) leapTarget(m *mob) (x, y, z float64, ok bool) {
	if m.hasTarget {
		return m.tx, m.y, m.tz, true // the hunt keeps only x/z; the target is near its level
	}
	if m.wolfPrey != 0 {
		if o := h.mobs[m.wolfPrey]; o != nil && o.dim == m.dim && o.dying == 0 {
			return o.x, o.y, o.z, true
		}
	}
	return 0, 0, 0, false
}

// leapCheck is canUse + start, run each mob update.
func (h *hub) leapCheck(players map[int32]*tracked, m *mob) {
	yd := leapKick(m.etype)
	if yd == 0 || m.leaping || m.dying > 0 || m.mount != 0 {
		return
	}
	tx, ty, tz, ok := h.leapTarget(m)
	if !ok {
		return
	}
	d2 := dist3sq(tx, ty, tz, m.x, m.y, m.z)
	if d2 < leapMinSq || d2 > leapMaxSq {
		return
	}
	if !h.mobOnGround(m) {
		return
	}
	hit := false
	for i := 0; i < mobMoveInterval; i++ { // the goal rolls every tick
		if h.rng.Intn(leapRollOdd) == 0 {
			hit = true
		}
	}
	if !hit {
		return
	}
	dx, dz := tx-m.x, tz-m.z
	vx, vz := m.vx*leapCarry, m.vz*leapCarry
	if hd := math.Hypot(dx, dz); hd > 1e-6 {
		vx += dx / hd * leapSpeed
		vz += dz / hd * leapSpeed
	}
	m.leaping, m.leapVX, m.leapVY, m.leapVZ = true, vx, yd, vz
}

// mobOnGround: its feet sit on the floor at its own level.
func (h *hub) mobOnGround(m *mob) bool {
	w := h.worldFor(m.dim)
	return m.y <= float64(w.MobFeetFrom(int(math.Floor(m.x)), int(math.Floor(m.z)), int(math.Floor(m.y))))+1e-6
}

// leapFlight flies the spring: gravity each tick, the ground stops it.
func (h *hub) leapFlight(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	for i := 0; i < mobMoveInterval; i++ {
		nx, nz := m.x+m.leapVX, m.z+m.leapVZ
		if h.ownedAt(nx, nz) && !worldgen.Collides(w.At(int(math.Floor(nx)), int(math.Floor(m.y)), int(math.Floor(nz)))) {
			m.x, m.z = nx, nz
		} else {
			m.leapVX, m.leapVZ = 0, 0
		}
		m.y += m.leapVY
		// LivingEntity.travelInAir: gravity, then air drag — 0.91 across,
		// 0.98 down — every tick the leap is in the air.
		m.leapVX, m.leapVZ = m.leapVX*0.91, m.leapVZ*0.91
		m.leapVY = (m.leapVY - m.effectiveGravity(m.leapVY)) * 0.98
		if m.swims && m.leapVY < 0 && worldgen.HoldsWater(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
			m.leaping, m.leapVX, m.leapVY, m.leapVZ = false, 0, 0, 0 // a leaping swimmer is home again
			return
		}
		feet := float64(w.MobFeetFrom(int(math.Floor(m.x)), int(math.Floor(m.z)), int(math.Floor(m.y))))
		if m.leapVY < 0 && m.y <= feet {
			m.y = feet
			m.leaping, m.leapVX, m.leapVY, m.leapVZ = false, 0, 0, 0
			return
		}
	}
	m.vx, m.vz = 0, 0
}
