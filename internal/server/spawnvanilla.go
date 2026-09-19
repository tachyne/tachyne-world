package server

import (
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The vanilla NaturalSpawner — the engine's only natural spawner since
// 2026-09-19 (the cheaper sampler and its animal top-up are gone). Its
// shape, mirroring Mojang's code:
//
//   - one position attempt per spawnable chunk per tick (not one per 8), with
//     the three-group pack loop of spawnCategoryForPosition;
//   - one-time chunk-GENERATION herds (spawnMobsForChunkGeneration) the first
//     time a chunk enters range — the primary source of animals in vanilla;
//   - the full distance gates: 24-block minimum from a player, a 24-block
//     exclusion around world spawn, and the per-category max spawn distance.
//
// Each chunk seeds its packs exactly once EVER: the seeded set persists with
// the mobs (mobstore), so a restart never re-lays them.

const (
	creatureGenProbability = 0.1 // vanilla MobSpawnSettings.creatureGenerationProbability
	chunkSeedBudget        = 8   // new chunks seeded per tick (bounds first-load cost)
	spawnPointExclusion    = 24  // vanilla: no spawns within 24 blocks of world spawn
)

// categorySpawnRange is the vanilla MobCategory despawnDistance, reused as the
// maximum distance a mob of that category may spawn from a player
// (NaturalSpawner.isValidSpawnPostitionForType); creatures skip the gate
// (canSpawnFarFromPlayer).
var categorySpawnRange = [catCount]int{128, 128, 128, 128, 64, 128, 128}

// spawnVanilla runs the exact-vanilla path for one tick.
func (h *hub) spawnVanilla(players map[int32]*tracked, dim int, chunks [][2]int32, chunkSet map[[2]int32]bool, spawnRingSize int, counts *[catCount]int) {
	h.seedChunkGeneration(players, dim, chunkSet, counts)           // one-time packs as land loads
	h.buildLocalCaps(players, dim)                                  // LocalMobCapCalculator: per-player counts
	h.buildSpawnPotential(dim)                                      // PotentialCalculator: the costed species' charges
	h.spawnVanillaTick(players, dim, chunks, spawnRingSize, counts) // per-tick NaturalSpawner
}

// spawnVanillaTick is NaturalSpawner.spawnForChunk: filter categories by the
// global cap + persistent gate once, then one position attempt per chunk.
func (h *hub) spawnVanillaTick(players map[int32]*tracked, dim int, chunks [][2]int32, spawnRingSize int, counts *[catCount]int) {
	spawnPersistent := h.tick.Load()%creatureSpawnMod == 0
	var caps [catCount]int
	var active [catCount]bool
	for cat := 0; cat < catCount; cat++ {
		if cat == catMonster && (h.rules.Difficulty == diffPeaceful || !h.rules.SpawnMonsters) {
			continue // peaceful, or gamerule spawn_monsters off
		}
		if cat == catCreature && !spawnPersistent {
			continue // persistent category only every 400 ticks
		}
		caps[cat] = spawnCap(cat, spawnRingSize)
		active[cat] = counts[cat] < caps[cat] // global cap gate (canSpawnForCategoryGlobal)
	}
	for _, c := range chunks {
		for cat := 0; cat < catCount; cat++ {
			if !active[cat] || counts[cat] >= caps[cat] {
				continue
			}
			h.spawnCategoryForChunk(players, dim, cat, c, counts, caps[cat])
		}
	}
}

// spawnCategoryForChunk is NaturalSpawner.spawnCategoryForChunk: a random column
// at a random height through the whole chunk (this is what populates caves),
// then the three-group pack loop if the anchor block is not solid.
func (h *hub) spawnCategoryForChunk(players map[int32]*tracked, dim, cat int, c [2]int32, counts *[catCount]int, cap int) {
	if !h.localCapAllows(cat, c) {
		return // canSpawnForCategoryLocal: every player near this chunk is at the category's cap
	}
	w := h.worldFor(dim)
	x := int(c[0])*16 + h.rng.Intn(16)
	z := int(c[1])*16 + h.rng.Intn(16)
	minY, surface := worldgen.MinY, w.SurfaceFeet(x, z)
	if dim != 0 {
		minY = 0 // the nether and the end floor at 0
	}
	if surface+2 <= minY {
		return
	}
	y := minY + h.rng.Intn(surface+2-minY) // uniform [minY, surface+1]
	if worldgen.Collides(w.At(x, y, z)) {
		return // vanilla: a redstone-conductor / solid anchor aborts the attempt
	}
	h.spawnGroupsAt(players, dim, cat, x, y, z, counts, cap)
}

// spawnGroupsAt is NaturalSpawner.spawnCategoryForPosition: up to three groups,
// each a random-walking pack whose size comes from the rolled biome entry, with
// every member re-checked for distance, position and spawn rules. The whole
// position stops once maxSpawnCluster mobs have been placed (vanilla returns).
func (h *hub) spawnGroupsAt(players map[int32]*tracked, dim, cat, ax, ay, az int, counts *[catCount]int, cap int) {
	total := 0
	for g := 0; g < 3; g++ {
		if !h.spawnOneGroupAt(players, dim, cat, ax, ay, az, counts, cap, &total) {
			return
		}
	}
}

// spawnOneGroupAt places one pack; false once the position is done (the
// category cap or the cluster limit reached). The pack shares its
// SpawnGroupData — the wolf/horse/llama/rabbit/fox variant — through the
// scoped spawn group.
func (h *hub) spawnOneGroupAt(players map[int32]*tracked, dim, cat, ax, ay, az int, counts *[catCount]int, cap int, total *int) (more bool) {
	more = true
	h.withSpawnGroup(func() {
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
			d := h.nearestPlayerSq(players, dim, float64(x)+0.5, float64(ay), float64(z)+0.5)
			if d <= float64(spawnMinDist*spawnMinDist) || (dim == 0 && h.nearWorldSpawn(x, ay, z)) {
				continue // never within 24 of a player, nor 24 of world spawn
			}
			if r := categorySpawnRange[cat]; cat != catCreature && r > 0 && d > float64(r*r) {
				continue // may not spawn beyond the category's max distance from a player (creatures canSpawnFarFromPlayer)
			}
			if !picked {
				sd2, ok := h.rollSpawner(h.spawnPool(dim, cat, x, ay, z))
				if !ok {
					break // empty pool for this biome/category — abandon the group
				}
				sd = sd2
				groupSize = sd.min + h.rng.Intn(sd.max-sd.min+1) // reset to the biome pack size
				picked = true
			}
			if !h.spawnPositionOK(dim, cat, sd.etype, x, ay, z) {
				continue
			}
			if !h.spawnPotentialOK(dim, sd.etype, x, ay, z) {
				continue // SpawnState.canSpawn: the costed species' potential is over its budget here
			}
			sky, block := h.worldFor(dim).LightAt(x, ay, z)
			if !h.spawnRulesOK(dim, cat, sd.etype, x, ay, z, sky, block) {
				continue
			}
			h.spawnNatural(players, dim, cat, sd.etype, x, ay, z)
			h.localCapAdd(cat, x, z)
			h.addSpawnCharge(dim, sd.etype, x, ay, z)
			counts[cat]++
			if counts[cat] >= cap {
				more = false
				return
			}
			if *total++; *total >= maxSpawnClusterFor(sd.etype) { // Mob.getMaxSpawnClusterSize: 4 total → done (fish 8, horses 6, ghasts 1)
				more = false
				return
			}
		}
	})
	return more
}

// nearWorldSpawn reports whether (x,y,z) is within the spawn-point exclusion
// radius (vanilla forbids natural spawns within 24 blocks of world spawn). The
// vanilla test is full 3D — respawnData.pos().closerToCenterThan(Vec3(x+0.5,
// y, z+0.5), 24) — so a deep-cave column directly under spawn is still allowed.
func (h *hub) nearWorldSpawn(x, y, z int) bool {
	if !h.hasWorldSpawn {
		return false
	}
	dx := float64(x) + 0.5 - h.worldSpawnX
	dy := float64(y) - h.worldSpawnY
	dz := float64(z) + 0.5 - h.worldSpawnZ
	return dx*dx+dy*dy+dz*dz < spawnPointExclusion*spawnPointExclusion
}

// seedChunkGeneration runs NaturalSpawner.spawnMobsForChunkGeneration once for
// each freshly loaded chunk — the vanilla animal herds that appear as you
// explore. Bounded to chunkSeedBudget new chunks per tick so a fresh join does
// not seed a whole view window in one tick.
func (h *hub) seedChunkGeneration(players map[int32]*tracked, dim int, chunkSet map[[2]int32]bool, counts *[catCount]int) {
	if h.seededChunks == nil {
		h.seededChunks = map[[2]int32]bool{}
	}
	if dim == dimEnd {
		return // the end's biomes have no creature pools
	}
	if dim == dimNether {
		h.seedNetherGeneration(players, chunkSet, counts)
		return
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
		h.seedChunkBees(players, c)
	}
}

// seedNetherGeneration is the nether's spawnMobsForChunkGeneration: its
// creature pools hold striders, laid in lava at the chunk's lava surface.
// The nether's seeded set is per pod lifetime (striders are not persisted
// by chunk the way the overworld's animals are).
func (h *hub) seedNetherGeneration(players map[int32]*tracked, chunkSet map[[2]int32]bool, counts *[catCount]int) {
	if h.seededNether == nil {
		h.seededNether = map[[2]int32]bool{}
	}
	w := h.worldFor(dimNether)
	budget := chunkSeedBudget
	for c := range chunkSet {
		if budget <= 0 {
			return
		}
		if h.seededNether[c] {
			continue
		}
		h.seededNether[c] = true
		budget--
		cx0, cz0 := int(c[0])*16, int(c[1])*16
		pool := h.spawnPool(dimNether, catCreature, cx0, 64, cz0)
		if len(pool) == 0 {
			continue
		}
		prob := biomeCreatureProbability(w.BiomeAt(cx0, cz0))
		for h.rng.Float32() < prob {
			sd, ok := h.rollSpawner(pool)
			if !ok {
				break
			}
			pack := sd.min + h.rng.Intn(sd.max-sd.min+1)
			x := cx0 + h.rng.Intn(16)
			z := cz0 + h.rng.Intn(16)
			h.withSpawnGroup(func() {
				for k := 0; k < pack; k++ {
					for attempt := 0; attempt < 4; attempt++ {
						if y, ok := netherTopPos(w, x, z); ok && h.spawnPositionOK(dimNether, catCreature, sd.etype, x, y, z) &&
							h.spawnRulesOK(dimNether, catCreature, sd.etype, x, y, z, 0, 0) {
							h.spawnNatural(players, dimNether, catCreature, sd.etype, x, y, z)
							counts[catCreature]++
							break
						}
						x = clampChunk(x+h.rng.Intn(5)-h.rng.Intn(5), cx0)
						z = clampChunk(z+h.rng.Intn(5)-h.rng.Intn(5), cz0)
					}
				}
			})
		}
	}
}

// netherTopPos is NaturalSpawner.getTopNonCollidingPos under a ceiling: from
// the top, down through the roof to the first air, then down through the
// air to the first block that is not — the lava surface or the floor.
func netherTopPos(w *world.World, x, z int) (int, bool) {
	y := 126
	for y > 0 && w.At(x, y, z) != worldgen.Air {
		y--
	}
	for y > 0 && w.At(x, y, z) == worldgen.Air {
		y--
	}
	return y, y > 0
}

// seedChunkBees hatches the occupants of freshly generated bee nests — the
// two or three bees vanilla stores inside the block entity. tachyne's bees
// live in the world (beehive.go's fill rule wants them NEAR the nest), so
// they appear beside it instead, once per chunk ever on the same seeded gate
// the generation herds use.
func (h *hub) seedChunkBees(players map[int32]*tracked, c [2]int32) {
	bx, bz := int(c[0])*16, int(c[1])*16
	if !h.ownedBlock(bx, bz) {
		return
	}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for wy := worldgen.SeaLevel; wy < worldgen.SeaLevel+96; wy++ {
				s := h.world.At(bx+lx, wy, bz+lz)
				if s < beeNestMin || s > beeNestMax {
					continue
				}
				h.registerHive(blockPos{bx + lx, wy, bz + lz})
				for n := 2 + h.rng.Intn(2); n > 0; n-- {
					h.spawnAnimal(players, entityBee, bx+lx, bz+lz)
				}
			}
		}
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
	biome := h.world.BiomeAt(cx0, cz0) // ChunkGenerator.spawnOriginalMobs: the biome at the chunk's corner
	pool := h.spawnPool(0, catCreature, cx0, h.world.MobFeet(cx0, cz0), cz0)
	if len(pool) == 0 {
		return
	}
	prob := biomeCreatureProbability(biome)
	for h.rng.Float32() < prob {
		sd, ok := h.rollSpawner(pool)
		if !ok {
			return
		}
		pack := sd.min + h.rng.Intn(sd.max-sd.min+1)
		x := cx0 + h.rng.Intn(16)
		z := cz0 + h.rng.Intn(16)
		h.withSpawnGroup(func() { // one SpawnGroupData per pack: shared variant, babies after the first
			for k := 0; k < pack; k++ {
				for attempt := 0; attempt < 4; attempt++ { // vanilla: up to 4 placement tries per individual
					if h.ownedBlock(x, z) && h.spawnableAnimalFor(sd.etype, x, z) {
						h.rollPackBaby(players, h.spawnAnimal(players, sd.etype, x, z))
						counts[catCreature]++
						break
					}
					x = clampChunk(x+h.rng.Intn(5)-h.rng.Intn(5), cx0)
					z = clampChunk(z+h.rng.Intn(5)-h.rng.Intn(5), cz0)
				}
			}
		})
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
