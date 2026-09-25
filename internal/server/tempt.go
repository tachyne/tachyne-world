package server

import (
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Tempting: an animal walks after a player holding its food (TemptGoal for
// the goal-driven species, FollowTemptation for the brain-driven ones). The
// nearest player within the mob's TEMPT_RANGE attribute (10 for animals) with the item in either hand is
// followed at the species' speed modifier, stopping short at its close-enough
// distance; when the player puts the food away the animal calms down for
// 100 ticks before it can be tempted again.

const (
	temptStop = 2.5 // TemptGoal's 6.25 sq / FollowTemptation's default
	temptCalm = 100 / mobMoveInterval
)

// temptSpeed is the per-species speed modifier; a species not listed is
// never tempted (wolves, foxes, parrots).
var temptSpeed = map[int]float64{
	entityCow: 1.25, entityMooshroom: 1.25, entitySheep: 1.1, entityPig: 1.2,
	entityChicken: 1.0, entityRabbit: 1.0, entityPanda: 1.0, entityTurtle: 1.1,
	entityBee: 1.25, entityHorse: 1.25, entityDonkey: 1.25, entityMule: 1.25,
	entitySkeletonHorse: 1.25, entityZombieHorse: 1.25, entityLlama: 1.25,
	entityTraderLlama: 1.25, entityStrider: 1.4, entityGoat: 1.25,
	entityAxolotl: 0.5, entityArmadillo: 1.25, entityFrog: 1.25, entityCamel: 2.5, entityCamelHusk: 2.5, entityHappyGhast: 1.25,
	entitySniffer: 1.25, entityTadpole: 1.25,
	entityNautilus: 1.3, entityZombieNautilus: 0.9, // NautilusAi / ZombieNautilusAi SPEED_MULTIPLIER_WHEN_TEMPTED
	entityCat: 0.6, entityOcelot: 0.6, // the scare-able creep (CatTemptGoal / OcelotTemptGoal)
}

// scareable are the species whose TemptGoal has canScare: within six blocks
// the player must hold still — a step (more than 0.1 of a block) or a turn
// of more than five degrees breaks the spell.
var scareable = map[int]bool{entityCat: true, entityOcelot: true}

// temptStopFor is the close-enough distance where the species overrides it.
func temptStopFor(m *mob) float64 {
	switch m.etype {
	case entityArmadillo:
		if m.baby {
			return 1
		}
		return 2
	case entityHappyGhast:
		return 3 // HappyGhastAi: FollowTemptation stops three blocks off
	case entityCamel, entityCamelHusk, entitySniffer, entityNautilus, entityZombieNautilus:
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
	case entityMule, entitySkeletonHorse:
		return breedFoods[entityHorse][item] // #horse_tempt_items for the whole family
	case entityZombieHorse:
		// ZombieHorse.addBehaviourGoals replaces the family's goals with its
		// own TemptGoal on #zombie_horse_food: a red mushroom.
		return item == itemRedMushroom
	case entityNautilus, entityZombieNautilus: // NAUTILUS_TEMPTATIONS: #nautilus_food, tamed or not
		return breedFoods[entityNautilus][item]
	case entityCamelHusk:
		return item == itemByName["rabbit_foot"] // FollowTemptation reads isFood: #camel_husk_food
	case entityHappyGhast: // #happy_ghast_tempt_items: its food (snowballs) and every harness
		return item == itemByName["snowball"] || strings.HasSuffix(itemRegistryName(item), "_harness")
	}
	return isLoveFood(etype, item)
}

// temptingPlayer is the nearest non-spectator player within TEMPT_RANGE
// holding the species' tempt item in either hand.
func (h *hub) temptingPlayer(players map[int32]*tracked, m *mob) *tracked {
	var best *tracked
	r := m.mobAttrs().Value(attr.TemptRange) // TemptGoal / TemptingSensor read the attribute
	bestD2 := r * r
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
	if !ok || (m.tamed && !nautilusKind(m.etype)) || m.loveTicks > 0 {
		return false // a nautilus's tempting sensor asks nothing of its taming
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
	if scareable[m.etype] {
		// canContinueToUse: close by, the player's slightest move or turn ends
		// it; farther off, just remember where they are.
		if m.tempted && dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) < 36 {
			moved := (t.x-m.temptPX)*(t.x-m.temptPX)+(t.y-m.temptPY)*(t.y-m.temptPY)+(t.z-m.temptPZ)*(t.z-m.temptPZ) > 0.01
			turned := math.Abs(float64(t.pitch-m.temptPitch)) > 5 || math.Abs(float64(t.yaw-m.temptYaw)) > 5
			if moved || turned {
				m.tempted = false
				m.temptCalm = temptCalm
				return false
			}
		} else {
			m.temptPX, m.temptPY, m.temptPZ = t.x, t.y, t.z
		}
		m.temptYaw, m.temptPitch = t.yaw, t.pitch
	}
	m.tempted = true
	m.rest = 0
	if m.flies {
		m.flyAim(h.tick.Load(), t.y) // a flier comes to the player's height
	}
	dx, dz := t.x-m.x, t.z-m.z
	if dist3(t.x, t.y, t.z, m.x, m.y, m.z) < temptStopFor(m) {
		m.vx, m.vz = 0, 0
		if nautilusKind(m.etype) {
			m.vy = 0
		}
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi) // look at the player
		return true
	}
	if nautilusKind(m.etype) { // a water-bound path: it swims up or down to the player as well
		h.swimToward(m, t.x, t.y, t.z, speed)
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
