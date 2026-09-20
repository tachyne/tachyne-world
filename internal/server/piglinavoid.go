package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// What a piglin keeps away from. Vanilla's brain has two avoid rules besides
// the fight: soul fire and its lanterns and torches — the repellents players
// build their bastion paths out of — and zombified piglins, which a living
// one will not go near. Neither was implemented, so soul torches did nothing
// and piglins wandered through their own undead.

const (
	piglinRepellentH  = 8   // REPELLENT_DETECTION_RANGE_HORIZONTAL
	piglinRepellentV  = 4   // …_VERTICAL
	piglinZombieDist  = 6.0 // DESIRED_DISTANCE_FROM_ZOMBIFIED
	piglinAvoidMin    = 100 // AVOID_ZOMBIFIED_DURATION: 5-7 seconds
	piglinAvoidSpan   = 40
	piglinAvoidSpeed  = 1.0 // SPEED_MULTIPLIER_WHEN_RETREATING
	piglinAvoidTarget = 12  // SetWalkTargetAwayFrom's distance
)

// isPiglinRepellent is #piglin_repellents: soul fire and the three soul
// lights.
func isPiglinRepellent(s uint32) bool {
	for _, n := range []string{"soul_fire", "soul_torch", "soul_wall_torch", "soul_lantern", "soul_campfire"} {
		lo := worldgen.BlockBase(n)
		if lo == 0 {
			continue
		}
		hi := lo
		if mn, mx := worldgen.BlockRange(n); mn != 0 {
			lo, hi = mn, mx
		}
		if s >= lo && s <= hi {
			return true
		}
	}
	return false
}

// piglinAvoidStep walks a piglin away from a repellent or a zombified piglin.
// Reports whether it holds the piglin this update.
func (h *hub) piglinAvoidStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityPiglin && m.etype != entityPiglinBrute {
		return false
	}
	if m.dying != 0 || m.admireUntil != 0 {
		return false
	}
	if m.piglinFlee > 0 { // already retreating: keep going
		m.piglinFlee -= mobMoveInterval
		return h.stepAwayFrom(m, m.piglinFleeX, m.piglinFleeZ)
	}
	// The zombified sensor first: it is the one that moves a piglin.
	var zomb *mob
	bestD := piglinZombieDist
	h.grid().nearby(m.dim, m.x, m.z, piglinZombieDist, func(o *mob) {
		if o.etype != entityZombifiedPiglin || o.dying > 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD {
			zomb, bestD = o, d
		}
	})
	if zomb != nil {
		m.piglinFlee = piglinAvoidMin + h.rng.Intn(piglinAvoidSpan)
		m.piglinFleeX, m.piglinFleeZ = zomb.x, zomb.z
		m.hasTarget = false
		return h.stepAwayFrom(m, zomb.x, zomb.z)
	}
	if pos, ok := h.piglinNearestRepellent(m); ok {
		m.hasTarget = false
		return h.stepAwayFrom(m, float64(pos.x)+0.5, float64(pos.z)+0.5)
	}
	return false
}

// piglinNearestRepellent is the NEAREST_REPELLENT sensor's 8 × 4 × 8 box.
func (h *hub) piglinNearestRepellent(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return blockPos{}, false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, bestD := blockPos{}, math.Inf(1)
	for x := bx - piglinRepellentH; x <= bx+piglinRepellentH; x++ {
		for y := by - piglinRepellentV; y <= by+piglinRepellentV; y++ {
			for z := bz - piglinRepellentH; z <= bz+piglinRepellentH; z++ {
				if !isPiglinRepellent(w.At(x, y, z)) {
					continue
				}
				if d := dist3(float64(x)+0.5, float64(y), float64(z)+0.5, m.x, m.y, m.z); d < bestD {
					best, bestD = blockPos{x, y, z}, d
				}
			}
		}
	}
	return best, !math.IsInf(bestD, 1)
}

// stepAwayFrom is SetWalkTargetAwayFrom: head off in the opposite direction.
func (h *hub) stepAwayFrom(m *mob, x, z float64) bool {
	dx, dz := m.x-x, m.z-z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		dx, dz, d = 1, 0, 1
	}
	sp := m.moveSpeed() * piglinAvoidSpeed
	m.vx, m.vz = dx/d*sp, dz/d*sp
	m.rest = 0
	return true
}
