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
		floor := h.nether.At(x, y-1, z)
		if floor == worldgen.Air || worldgen.IsFluid(floor) ||
			h.nether.At(x, y, z) != worldgen.Air || h.nether.At(x, y+1, z) != worldgen.Air {
			continue // the world view (with edits) must agree it's standable
		}
		etype, ok := h.netherSpawnPick(x, z, floor)
		if !ok {
			continue
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

// netherSpawnWeights is each nether biome's MONSTER spawn list (NetherBiomes);
// blazes and wither skeletons come only from a fortress's own rolls.
var netherSpawnWeights = map[string][]struct {
	etype, weight int
}{
	"minecraft:nether_wastes":    {{entityGhast, 50}, {entityZombifiedPiglin, 100}, {entityMagmaCube, 2}, {entityEnderman, 1}, {entityPiglin, 15}},
	"minecraft:soul_sand_valley": {{entitySkeleton, 20}, {entityGhast, 50}, {entityEnderman, 1}},
	"minecraft:basalt_deltas":    {{entityGhast, 40}, {entityMagmaCube, 100}},
	"minecraft:crimson_forest":   {{entityZombifiedPiglin, 1}, {entityHoglin, 9}, {entityPiglin, 5}},
	"minecraft:warped_forest":    {{entityEnderman, 1}},
}

// netherSpawnPick draws a species for a spot by its biome's weights, one
// roll in six a strider (the CREATURE list, weight 60 everywhere); a hoglin
// never on a wart block (Hoglin.checkHoglinSpawnRules).
func (h *hub) netherSpawnPick(x, z int, floor uint32) (int, bool) {
	if h.rng.Intn(6) == 0 {
		return entityStrider, true
	}
	list := netherSpawnWeights[h.nether.Gen().NetherBiomeAt(x, z)]
	if len(list) == 0 {
		return 0, false
	}
	total := 0
	for _, e := range list {
		total += e.weight
	}
	r := h.rng.Intn(total)
	for _, e := range list {
		if r < e.weight {
			if e.etype == entityHoglin && floor == worldgen.NetherWartBlock {
				return 0, false
			}
			return e.etype, true
		}
		r -= e.weight
	}
	return 0, false
}
