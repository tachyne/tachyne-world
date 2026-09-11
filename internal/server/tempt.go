package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Tempting: an animal walks after a player holding its food (TemptGoal for
// the goal-driven species, FollowTemptation for the brain-driven ones). The
// nearest player within TEMPT_RANGE (10) with the item in either hand is
// followed at the species' speed modifier, stopping short at its close-enough
// distance; when the player puts the food away the animal calms down for
// 100 ticks before it can be tempted again.

const (
	temptRange = 10.0 // Animal's TEMPT_RANGE attribute
	temptStop  = 2.5  // TemptGoal's 6.25 sq / FollowTemptation's default
	temptCalm  = 100 / mobMoveInterval
)

// temptSpeed is the per-species speed modifier; a species not listed is
// never tempted (wolves, foxes, parrots, and cats/ocelots, whose scare-able
// variant is folded into the engine's skittish flee).
var temptSpeed = map[int]float64{
	entityCow: 1.25, entityMooshroom: 1.25, entitySheep: 1.1, entityPig: 1.2,
	entityChicken: 1.0, entityRabbit: 1.0, entityPanda: 1.0, entityTurtle: 1.1,
	entityBee: 1.25, entityHorse: 1.25, entityDonkey: 1.25, entityMule: 1.25,
	entitySkeletonHorse: 1.25, entityZombieHorse: 1.25, entityLlama: 1.25,
	entityTraderLlama: 1.25, entityStrider: 1.4, entityGoat: 1.25,
	entityAxolotl: 0.5, entityArmadillo: 1.25, entityFrog: 1.25, entityCamel: 2.5,
	entitySniffer: 1.25, entityTadpole: 1.25,
}

// temptStopFor is the close-enough distance where the species overrides it.
func temptStopFor(m *mob) float64 {
	switch m.etype {
	case entityArmadillo:
		if m.baby {
			return 1
		}
		return 2
	case entityCamel, entitySniffer:
		if m.baby {
			return 2.5
		}
		return 3.5
	}
	return temptStop
}

// isTemptItem is the species' tempt predicate: its food tag, plus the rod
// that steers it (a carrot or warped fungus on a stick).
func isTemptItem(etype int, item int32) bool {
	if item == 0 {
		return false
	}
	switch etype {
	case entityPig:
		if item == itemByName["carrot_on_a_stick"] {
			return true
		}
	case entityStrider:
		if item == itemByName["warped_fungus_on_a_stick"] {
			return true
		}
	case entityMule, entitySkeletonHorse, entityZombieHorse:
		return breedFoods[entityHorse][item] // #horse_tempt_items for the whole family
	}
	return isLoveFood(etype, item)
}

// temptingPlayer is the nearest non-spectator player within TEMPT_RANGE
// holding the species' tempt item in either hand.
func (h *hub) temptingPlayer(players map[int32]*tracked, m *mob) *tracked {
	var best *tracked
	bestD2 := temptRange * temptRange
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if !isTemptItem(m.etype, heldStack(t).item) && !isTemptItem(m.etype, t.offhand.item) {
			continue
		}
		if d2 := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	return best
}

// temptStep steers a tempted animal after its player. Returns whether the
// goal holds the mob this update (walking, or standing close and looking).
func (h *hub) temptStep(players map[int32]*tracked, m *mob) bool {
	speed, ok := temptSpeed[m.etype]
	if !ok || m.tamed || m.loveTicks > 0 {
		return false
	}
	if m.temptCalm > 0 {
		m.temptCalm--
		return false
	}
	t := h.temptingPlayer(players, m)
	if t == nil {
		if m.tempted {
			m.tempted = false
			m.temptCalm = temptCalm // stop(): calmDown before the next temptation
		}
		return false
	}
	m.tempted = true
	m.rest = 0
	dx, dz := t.x-m.x, t.z-m.z
	if dist3(t.x, t.y, t.z, m.x, m.y, m.z) < temptStopFor(m) {
		m.vx, m.vz = 0, 0
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi) // look at the player
		return true
	}
	hd := math.Hypot(dx, dz)
	if hd < 1e-6 {
		return false
	}
	if m.etype == entityAxolotl { // AxolotlAi.getSpeedModifier: 0.5 in water, 0.15 ashore
		if w := h.worldFor(m.dim); w != nil && !worldgen.HoldsWater(w.At(int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)))) {
			speed = 0.15
		}
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vz = dx/hd*sp, dz/hd*sp
	return true
}
