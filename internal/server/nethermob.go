package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Nether mobs: zombified piglins (neutral until hit), magma cubes (slime
// pattern, fireproof) and blazes (ranged fire). They spawn around nether
// players on netherrack, live entirely in dimension 1 through the dim-aware
// entity plumbing, and their drops feed brewing (blaze rods, magma cream).

const (
	piglinHealth = 20
	blazeHealth  = 20

	netherMobCap     = 14 // per-player-area cap
	netherSpawnRange = 40
	blazeShootRange  = 12.0
	blazeFireballDmg = 5
)

var (
	entityZombifiedPiglin = entityID("zombified_piglin")
	entityMagmaCube       = entityID("magma_cube")
	entityBlaze           = entityID("blaze")
	entitySmallFireball   = entityID("small_fireball")
)

func isNetherMob(etype int) bool {
	switch etype {
	case entityZombifiedPiglin, entityMagmaCube, entityBlaze,
		entityPiglin, entityHoglin, entityStrider, entityWitherSkeleton, entityGhast:
		return true
	}
	return false
}

// updateNetherMobs is the nether's spawn pass (called on the hostile cadence).
func (h *hub) updateNetherMobs(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful {
		return
	}
	count := 0
	for _, m := range h.mobs {
		if m.dim == 1 {
			count++
		}
	}
	for _, t := range players {
		if t.dim != 1 || count >= netherMobCap {
			continue
		}
		// Roll a spot on a ring around the player, on standable netherrack.
		ang := h.rng.Float64() * 2 * math.Pi
		d := 16 + h.rng.Float64()*float64(netherSpawnRange-16)
		x := int(t.x + math.Cos(ang)*d)
		z := int(t.z + math.Sin(ang)*d)
		if m := h.spawnFortressMob(players, x, z); m != nil {
			count++ // inside a fortress: its own garrison rolls, on its own floors
			continue
		}
		y, ok := h.nether.Gen().NetherFloorOK(x, z)
		if !ok { // solid rock or open lava sea — no fallback-height spawns
			continue
		}
		if h.nether.At(x, y-1, z) != worldgen.Netherrack ||
			h.nether.At(x, y, z) != worldgen.Air || h.nether.At(x, y+1, z) != worldgen.Air {
			continue // the world view (with edits) must agree it's standable
		}
		etype := entityZombifiedPiglin
		switch r := h.rng.Intn(100); {
		case r < 22:
			etype = entityMagmaCube
		case r < 34:
			etype = entityBlaze
		case r < 46:
			etype = entityPiglin
		case r < 58:
			etype = entityHoglin
		case r < 66:
			etype = entityStrider
		case r < 74:
			etype = entityWitherSkeleton
		case r < 80:
			etype = entityGhast
		}
		m := h.spawnMobIn(players, etype, 1, float64(x)+0.5, float64(y), float64(z)+0.5)
		h.configureNetherMob(players, m)
		count++
	}
}

// configureNetherMob applies species quirks (mirrors configureHostile2).
func (h *hub) configureNetherMob(players map[int32]*tracked, m *mob) {
	if m == nil {
		return // plugin-cancelled spawn
	}
	switch m.etype {
	case entityZombifiedPiglin:
		m.hostile, m.neutral = true, true        // armed but peaceful until hit
		m.behavior = Behavior(hostileBehavior{}) // speed from speedFor (attr 0.23)
		m.setFollowRange(35)                     // zombie-family FOLLOW_RANGE (vanilla behavior)
		m.setBaseArmor(2)
	case entityMagmaCube:
		m.hostile = true
		m.size = 1 + h.rng.Intn(3)*1 // 1/2/4-ish
		if m.size == 3 {
			m.size = 4
		}
		m.applyCubeSize()
		m.behavior = Behavior(hostileBehavior{})
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(slimeMeta(m.eid, m.size)))
	case entityBlaze:
		m.hostile = true
		m.health = blazeHealth
		m.behavior = Behavior(rangedBehavior{}) // speed from speedFor (attr 0.23)
		m.setFollowRange(48)                    // Blaze FOLLOW_RANGE (vanilla 1.21.5)
	default:
		h.applySpecies(players, m) // roster nether species (piglin/hoglin/strider/…)
		h.rollStriderRider(players, m)
	}
}
