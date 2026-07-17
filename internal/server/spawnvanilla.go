package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The exact-vanilla NaturalSpawner (opt-in via `-spawner vanilla`). It differs
// from the default tachyne sampler in three ways, each mirroring Mojang's code:
//
//   - one position attempt per spawnable chunk per tick (not one per 8), with
//     the three-group pack loop of spawnCategoryForPosition;
//   - one-time chunk-GENERATION herds (spawnMobsForChunkGeneration) the first
//     time a chunk enters range — the primary source of animals in vanilla;
//   - the full distance gates: 24-block minimum from a player, a 24-block
//     exclusion around world spawn, and the per-category max spawn distance.
//
// Our world does not persist mobs, so each chunk seeds its herd exactly once per
// pod lifetime (seededChunks); a restart clears both the herds and the set
// together, so it stays self-consistent. herdTopUp is disabled in this mode.

const (
	creatureGenProbability = 0.1 // vanilla MobSpawnSettings.creatureGenerationProbability
	chunkSeedBudget        = 8   // new chunks seeded per tick (bounds first-load cost)
	spawnPointExclusion    = 24  // vanilla: no spawns within 24 blocks of world spawn
)

// categorySpawnRange is the vanilla MobCategory despawnDistance, reused as the
// maximum distance a mob of that category may spawn from a player
// (NaturalSpawner.isValidSpawnPostitionForType). This is distinct from
// categoryDespawnDist, which is -1 for the persistent creature category.
var categorySpawnRange = [catCount]int{128, 128, 128, 128, 64}

// spawnVanilla runs the exact-vanilla path for one tick.
func (h *hub) spawnVanilla(players map[int32]*tracked, chunks [][2]int32, chunkSet map[[2]int32]bool, counts *[catCount]int) {
	h.seedChunkGeneration(players, chunkSet, counts) // one-time herds as land loads
	h.spawnVanillaTick(players, chunks, counts)      // per-tick NaturalSpawner
}

// spawnVanillaTick is NaturalSpawner.spawnForChunk: filter categories by the
// global cap + persistent gate once, then one position attempt per chunk.
func (h *hub) spawnVanillaTick(players map[int32]*tracked, chunks [][2]int32, counts *[catCount]int) {
	spawnPersistent := h.tick.Load()%creatureSpawnMod == 0
	var caps [catCount]int
	var active [catCount]bool
	for cat := 0; cat < catCount; cat++ {
		if cat == catMonster && h.rules.Difficulty == diffPeaceful {
			continue
		}
		if cat == catCreature && !spawnPersistent {
			continue // persistent category only every 400 ticks
		}
		caps[cat] = categoryCap[cat] * len(chunks) / spawnChunkArea
		active[cat] = counts[cat] < caps[cat] // global cap gate (canSpawnForCategoryGlobal)
	}
	for _, c := range chunks {
		for cat := 0; cat < catCount; cat++ {
			if !active[cat] || counts[cat] >= caps[cat] {
				continue
			}
			h.spawnCategoryForChunk(players, cat, c, counts, caps[cat])
		}
	}
}

// spawnCategoryForChunk is NaturalSpawner.spawnCategoryForChunk: a random column
// at a random height through the whole chunk (this is what populates caves),
// then the three-group pack loop if the anchor block is not solid.
func (h *hub) spawnCategoryForChunk(players map[int32]*tracked, cat int, c [2]int32, counts *[catCount]int, cap int) {
	x := int(c[0])*16 + h.rng.Intn(16)
	z := int(c[1])*16 + h.rng.Intn(16)
	surface := h.world.SurfaceFeet(x, z)
	y := worldgen.MinY + h.rng.Intn(surface+2-worldgen.MinY) // uniform [minY, surface+1]
	if worldgen.Collides(h.world.At(x, y, z)) {
		return // vanilla: a redstone-conductor / solid anchor aborts the attempt
	}
	h.spawnGroupsAt(players, cat, x, y, z, counts, cap)
}

// spawnGroupsAt is NaturalSpawner.spawnCategoryForPosition: up to three groups,
// each a random-walking pack whose size comes from the rolled biome entry, with
// every member re-checked for distance, position and spawn rules. The whole
// position stops once maxSpawnCluster mobs have been placed (vanilla returns).
func (h *hub) spawnGroupsAt(players map[int32]*tracked, cat, ax, ay, az int, counts *[catCount]int, cap int) {
	total := 0
	for g := 0; g < 3; g++ {
		x, z := ax, az
		groupSize := h.rng.Intn(4) + 1 // vanilla ceil(rand*4) = 1..4 until a type is rolled
		picked := false
		var sd spawnerEntry
		for j := 0; j < groupSize; j++ {
			x += h.rng.Intn(6) - h.rng.Intn(6) // ±5 scatter, same Y across the pack
			z += h.rng.Intn(6) - h.rng.Intn(6)
			if !h.ownedBlock(x, z) {
				continue
			}
			d := h.nearestPlayerSq(players, float64(x)+0.5, float64(ay), float64(z)+0.5)
			if d <= float64(spawnMinDist*spawnMinDist) || h.nearWorldSpawn(x, z) {
				continue // never within 24 of a player, nor 24 of world spawn
			}
			if r := categorySpawnRange[cat]; r > 0 && d > float64(r*r) {
				continue // may not spawn beyond the category's max distance from a player
			}
			if !picked {
				sd2, ok := h.rollSpawner(h.spawnPool(cat, x, ay, z))
				if !ok {
					break // empty pool for this biome/category — abandon the group
				}
				sd = sd2
				groupSize = sd.min + h.rng.Intn(sd.max-sd.min+1) // reset to the biome pack size
				picked = true
			}
			if !h.spawnPositionOK(cat, x, ay, z) {
				continue
			}
			sky, block := h.world.LightAt(x, ay, z)
			if !h.spawnRulesOK(cat, sd.etype, x, ay, z, sky, block) {
				continue
			}
			h.spawnNatural(players, cat, sd.etype, x, ay, z)
			counts[cat]++
			if counts[cat] >= cap {
				return
			}
			if total++; total >= maxSpawnCluster { // vanilla getMaxSpawnClusterSize: 4 total → done
				return
			}
		}
	}
}

// nearWorldSpawn reports whether (x,z) is within the spawn-point exclusion
// radius (vanilla forbids natural spawns within 24 blocks of world spawn).
func (h *hub) nearWorldSpawn(x, z int) bool {
	if !h.hasWorldSpawn {
		return false
	}
	dx, dz := float64(x)+0.5-h.worldSpawnX, float64(z)+0.5-h.worldSpawnZ
	return dx*dx+dz*dz < spawnPointExclusion*spawnPointExclusion
}

// seedChunkGeneration runs NaturalSpawner.spawnMobsForChunkGeneration once for
// each freshly loaded chunk — the vanilla animal herds that appear as you
// explore. Bounded to chunkSeedBudget new chunks per tick so a fresh join does
// not seed a whole view window in one tick.
func (h *hub) seedChunkGeneration(players map[int32]*tracked, chunkSet map[[2]int32]bool, counts *[catCount]int) {
	if h.seededChunks == nil {
		h.seededChunks = map[[2]int32]bool{}
	}
	budget := chunkSeedBudget
	for c := range chunkSet {
		if budget <= 0 {
			return // seed the rest on later ticks (map order is random — no starvation)
		}
		if h.seededChunks[c] {
			continue
		}
		// A chunk with persisted mobs parked in the store was already populated
		// in a prior session — mark it seeded WITHOUT laying a second herd.
		// seededChunks is in-memory and resets on restart; without this guard the
		// one-time generation herd is re-laid on top of the reloaded persisted
		// herd (reconcileMobChunks reloads it a few chunks per tick, so seeding
		// races ahead of the reload), doubling animals on every restart.
		if h.mobstore != nil && h.mobstore.has(c[0], c[1]) {
			h.seededChunks[c] = true
			continue
		}
		h.seededChunks[c] = true
		budget--
		h.seedChunkAnimals(players, c, counts)
	}
}

// seedChunkAnimals is the CREATURE-only chunk-generation pass: a geometric loop
// on the 0.1 probability, each iteration a weighted biome pack placed on valid
// ground. No player-distance or cap gate (vanilla runs this at world-gen).
func (h *hub) seedChunkAnimals(players map[int32]*tracked, c [2]int32, counts *[catCount]int) {
	cx0, cz0 := int(c[0])*16, int(c[1])*16
	if !h.ownedBlock(cx0, cz0) {
		return
	}
	pool := h.spawnPool(catCreature, cx0+8, h.world.MobFeet(cx0+8, cz0+8), cz0+8)
	if len(pool) == 0 {
		return
	}
	for h.rng.Float32() < creatureGenProbability {
		sd, ok := h.rollSpawner(pool)
		if !ok {
			return
		}
		pack := sd.min + h.rng.Intn(sd.max-sd.min+1)
		x := cx0 + h.rng.Intn(16)
		z := cz0 + h.rng.Intn(16)
		for k := 0; k < pack; k++ {
			for attempt := 0; attempt < 4; attempt++ { // vanilla: up to 4 placement tries per individual
				if h.ownedBlock(x, z) && h.spawnableAnimal(x, z) {
					h.spawnAnimal(players, sd.etype, x, z)
					counts[catCreature]++
					break
				}
				x = clampChunk(x+h.rng.Intn(5)-h.rng.Intn(5), cx0)
				z = clampChunk(z+h.rng.Intn(5)-h.rng.Intn(5), cz0)
			}
		}
	}
}

// clampChunk keeps a drifting seed coordinate inside its 16-wide chunk band.
func clampChunk(v, base int) int {
	if v < base {
		return base
	}
	if v > base+15 {
		return base + 15
	}
	return v
}
