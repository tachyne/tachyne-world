package server

// Nether mobs: zombified piglins (neutral until hit), magma cubes (slime
// pattern, fireproof) and blazes (ranged fire). They spawn around nether
// players on netherrack, live entirely in dimension 1 through the dim-aware
// entity plumbing, and their drops feed brewing (blaze rods, magma cream).

const (
	piglinHealth = 20
	blazeHealth  = 20

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
		entityPiglin, entityHoglin, entityStrider, entityWitherSkeleton, entityGhast,
		entitySkeleton, entityEnderman: // the soul sand valley's and warped forest's
		return true
	}
	return false
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
