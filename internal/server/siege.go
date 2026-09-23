package server

import (
	"log"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Zombie siege: vanilla's VillageSiege, run every tick.
//   - At the siege time marker (18000, midnight) on a dark night it rolls
//     1 in 10.
//   - A siege night then waits for a player standing inside a village
//     (isVillage: within a section of a claimed bed, workstation or bell),
//     outside mushroom fields. It tries ten bearings 32 blocks out from the
//     player for a spawn point, and settles on the first with somewhere a
//     zombie could stand.
//   - It releases twenty zombies there, one every three ticks. Each lands
//     within ±8 of the point, on the world surface, inside the village, and
//     only where monster spawn rules allow it (dark enough, something to
//     stand on).
// A setup that found no point still counts as set up, as in vanilla: that
// night's siege has no zombies. Light clears the state.
//
// It used to roll in a dusk window, pick a village near any player, and
// spawn around the well at two a second.

const (
	siegeZombies     = 20    // VillageSiege.zombiesToSpawn
	siegeChance      = 10    // nextInt(10) == 0 → SIEGE_TONIGHT
	siegeRollAt      = 18000 // ClockTimeMarkers.ROLL_VILLAGE_SIEGE on the day timeline
	siegeDistance    = 32    // the spawn point's distance from the player
	siegeSpread      = 8     // findRandomSpawnPos: nextInt(16) - 8
	siegeSpawnEvery  = 3     // nextSpawnTime = 2, counted down to 0
	siegeSetupTries  = 10
	siegeFindTries   = 10
	siegeDarkSkyFrom = 4 // isBrightOutside is skyDarken < 4
)

// Village-siege states (vanilla VillageSiege.State).
const (
	siegeDone    = iota // SIEGE_DONE
	siegeTonight        // SIEGE_TONIGHT: rolled, set up or waiting to be
)

// updateVillageSiege is VillageSiege.tick.
func (h *hub) updateVillageSiege(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful || h.skyDarken() < siegeDarkSkyFrom {
		h.siegeState, h.siegeSetUp = siegeDone, false
		return
	}
	if h.dayTime.Load()%dayLength == siegeRollAt {
		h.siegeState = siegeDone
		if h.rng.Intn(siegeChance) == 0 {
			h.siegeState = siegeTonight
		}
	}
	if h.siegeState == siegeDone {
		return
	}
	if !h.siegeSetUp {
		if !h.setupSiege(players) {
			return // nobody in a village yet: ask again next tick
		}
		h.siegeSetUp = true
	}
	if h.siegeNext > 0 {
		h.siegeNext--
		return
	}
	h.siegeNext = siegeSpawnEvery - 1
	if h.siegeLeft > 0 {
		h.spawnSiegeZombie(players)
		h.siegeLeft--
		return
	}
	h.siegeState = siegeDone
}

// setupSiege is tryToSetupSiege: true once a player stands in a village.
func (h *hub) setupSiege(players map[int32]*tracked) bool {
	for _, t := range players {
		if t.dim != 0 || t.dead || t.gamemode == gmSpectator {
			continue
		}
		px, py, pz := floorInt(t.x), floorInt(t.y), floorInt(t.z)
		if !h.isVillageAt(px, py, pz) || h.world.BiomeAt(px, pz) == "minecraft:mushroom_fields" {
			continue // #without_zombie_sieges
		}
		h.siegeLeft = 0
		for i := 0; i < siegeSetupTries; i++ {
			ang := h.rng.Float64() * 2 * math.Pi
			c := blockPos{px + int(math.Floor(math.Cos(ang)*siegeDistance)), py, pz + int(math.Floor(math.Sin(ang)*siegeDistance))}
			if _, ok := h.siegeSpot(c); ok {
				h.siegeCenter, h.siegeNext, h.siegeLeft = c, 0, siegeZombies
				log.Printf("zombie siege around (%d,%d): %d zombies", c.x, c.z, siegeZombies)
				break
			}
		}
		return true
	}
	return false
}

// siegeSpot is findRandomSpawnPos: ten tries within ±8 of the point, on the
// world surface, inside the village, where a zombie may spawn.
func (h *hub) siegeSpot(c blockPos) (blockPos, bool) {
	for i := 0; i < siegeFindTries; i++ {
		x := c.x + h.rng.Intn(2*siegeSpread) - siegeSpread
		z := c.z + h.rng.Intn(2*siegeSpread) - siegeSpread
		if !h.world.Ticking(int32(x>>4), int32(z>>4)) {
			continue
		}
		y := h.worldSurface(x, z)
		if !h.isVillageAt(x, y, z) || !h.spawnPositionOK(0, catMonster, entityZombie, x, y, z) {
			continue
		}
		if sky, block := h.world.LightAt(x, y, z); !h.darkEnoughToSpawn(sky, block) {
			continue // Monster.checkMonsterSpawnRules
		}
		return blockPos{x, y, z}, true
	}
	return blockPos{}, false
}

// spawnSiegeZombie is trySpawn: one zombie at a fresh spot, or none this time.
func (h *hub) spawnSiegeZombie(players map[int32]*tracked) {
	p, ok := h.siegeSpot(h.siegeCenter)
	if !ok {
		return
	}
	if m := h.spawnHostileY(players, entityZombie, float64(p.x)+0.5, float64(p.y), float64(p.z)+0.5); m != nil {
		m.yaw = float32(h.rng.Float64() * 360)
	}
}

// isVillageAt is ServerLevel.isVillage: the block's section is within one
// of a section holding a claimed bed, workstation or bell.
func (h *hub) isVillageAt(x, y, z int) bool {
	return sectionsToVillage(h.villageCentres(0), [3]int{x >> 4, y >> 4, z >> 4}) <= 1
}

// worldSurface is the WORLD_SURFACE heightmap: one above the highest block
// that is not air.
func (h *hub) worldSurface(x, z int) int {
	for y := h.world.Ceiling() - 1; y > worldgen.MinY; y-- {
		if h.world.At(x, y, z) != worldgen.Air {
			return y + 1
		}
	}
	return worldgen.MinY
}
