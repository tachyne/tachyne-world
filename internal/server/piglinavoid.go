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

// alertZombifiedPiglins is ZombifiedPiglin.alertOthers: hit one and every
// other one inside its FOLLOW_RANGE box — thirty-five across, ten up and down
// — takes the same grudge. This is the rule behind "never hit a zombified
// piglin", and the engine's version only raised their anger inside a sixteen
// block sphere without ever telling them WHO to come for, so the pack seethed
// where it stood.
//
// Vanilla only alerts the ones with no target of their own, so a piglin
// already fighting someone is left to it.
const (
	zPiglinAlertRange = 35.0 // FOLLOW_RANGE
	zPiglinAlertY     = 10.0
)

func (h *hub) alertZombifiedPiglins(from *mob, t *tracked) {
	h.grid().nearby(from.dim, from.x, from.z, zPiglinAlertRange, func(o *mob) {
		if o.eid == from.eid || o.etype != entityZombifiedPiglin || o.dying > 0 {
			return
		}
		if math.Abs(o.x-from.x) > zPiglinAlertRange || math.Abs(o.z-from.z) > zPiglinAlertRange ||
			math.Abs(o.y-from.y) > zPiglinAlertY {
			return
		}
		if o.targetEID != 0 {
			return // already has someone of its own
		}
		h.provoke(o, t)
	})
}

// guardedByPiglins is the #guarded_by_piglins block tag: the containers and
// gold a piglin considers its own. Opening one in front of an idle piglin
// turns it on you (PiglinAi.angerNearbyPiglins), which is what makes looting
// a bastion a decision rather than a stroll.
var guardedByPiglins = func() map[uint32]bool {
	m := map[uint32]bool{}
	names := []string{
		"gold_block", "barrel", "chest", "ender_chest", "gilded_blackstone",
		"trapped_chest", "raw_gold_block", "gold_ore", "deepslate_gold_ore",
		"copper_chest", "exposed_copper_chest", "weathered_copper_chest",
		"oxidized_copper_chest", "waxed_copper_chest", "waxed_exposed_copper_chest",
		"waxed_weathered_copper_chest", "waxed_oxidized_copper_chest",
		"shulker_box",
	}
	for _, c := range []string{
		"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black",
	} {
		names = append(names, c+"_shulker_box")
	}
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			for s := lo; s <= hi; s++ {
				m[s] = true
			}
		}
	}
	return m
}()

// piglinGuardRange is the box angerNearbyPiglins searches, inflated 16 from
// the player.
const piglinGuardRange = 16.0

// angerNearbyPiglins turns every idle piglin that can see the player. Vanilla
// only checks sight for the cases that ask for it — opening a container does,
// so that a piglin around a corner is not offended by a noise.
func (h *hub) angerNearbyPiglins(players map[int32]*tracked, t *tracked, needSight bool) {
	h.grid().nearby(t.dim, t.x, t.z, piglinGuardRange, func(m *mob) {
		if m.etype != entityPiglin || m.dying > 0 || m.baby {
			return
		}
		if m.targetEID != 0 || m.anger > 0 {
			return // isIdle: one already busy is left to it
		}
		if needSight && !h.mobSees(m, t) {
			return
		}
		h.provoke(m, t)
	})
}
