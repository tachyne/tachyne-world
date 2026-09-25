package server

import (
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Natural mob spawning — a port of vanilla NaturalSpawner. The old system
// spawned one hostile pack per second at the SURFACE near one player, gated
// by the clock (night only); caves never spawned anything and torches were
// the only light rule. Vanilla instead attempts spawns across every loaded
// chunk at a RANDOM HEIGHT through the whole column — that is what populates
// caves — and has no time gate at all: the light rules do the gating (sky
// light fails daytime surface spawns; block light zero keeps lit bases and
// torch-lit tunnels safe; storms darken the sky enough to spawn monsters in
// daytime). Categories have separate caps scaled by the loaded-chunk count,
// species come from per-biome weighted pools with vanilla pack sizes, and
// non-persistent mobs despawn by distance.
//
// Divergences (deliberate, documented): no per-player local mob cap (our
// player counts are small); underground-water creatures (glow squid) are
// folded into the water-creature pool by depth; vanilla's chunk-generation
// creature herds are approximated by a low-rate herd top-up near players
// (our chunks regenerate on the fly and mobs don't persist, so without it
// the countryside would empty after every restart); the nether keeps its
// own spawner (updateNetherMobs).

// Mob categories (vanilla MobCategory): caps are per 17×17 chunks = 289,
// scaled by the spawnable-chunk count; -1 despawn distance = persistent.
const (
	catMonster = iota
	catCreature
	catAmbient
	catWaterCreature
	catWaterAmbient
	catAxolotls         // vanilla AXOLOTLS: its own cap, lush caves only
	catUndergroundWater // vanilla UNDERGROUND_WATER_CREATURE: the glow squid, its own cap
	catCount
)

var categoryCap = [catCount]int{70, 10, 15, 5, 20, 5, 5}
var categoryDespawnDist = [catCount]int{128, 128, 128, 128, 64, 128, 128}

const (
	spawnChunkArea   = 17 * 17 // vanilla MAGIC_NUMBER: cap scale denominator
	spawnDistChunks  = 8       // vanilla SPAWN_DISTANCE_CHUNK: the spawn ring is ±8 chunks
	creatureSpawnMod = 400     // persistent categories spawn every 400 ticks
	maxSpawnCluster  = 4       // vanilla Mob.getMaxSpawnClusterSize default
	// Vanilla attempts one position per loaded chunk per tick; we sample at
	// one per 8 chunks per tick per category. Most attempts die instantly
	// (random Y lands inside rock), so a higher rate is what makes cave
	// density feel vanilla — affordable now that chunk light is cached in
	// the world (the first live tuning at 1/64 left caves near-empty).
)

// spawnerEntry is vanilla MobSpawnSettings.SpawnerData: a weighted species
// with its pack-size range.
type spawnerEntry struct {
	etype            int
	weight, min, max int
}

// Monster pools per biome family (vanilla BiomeDefaultFeatures.monsters +
// the per-biome overrides: desert husks, snowy strays, drowned oceans).
var (
	monsterPoolDefault = []spawnerEntry{
		{entitySpider, 100, 4, 4}, {entityZombie, 95, 4, 4}, {entityZombieVillager, 5, 1, 1},
		{entitySkeleton, 100, 4, 4}, {entityCreeper, 100, 4, 4}, {entitySlime, 100, 4, 4},
		{entityEnderman, 10, 1, 4}, {entityWitch, 5, 1, 1},
	}
	monsterPoolDesert = []spawnerEntry{
		{entitySpider, 100, 4, 4}, {entityHusk, 80, 4, 4}, {entityZombie, 19, 4, 4},
		{entityZombieVillager, 1, 1, 1}, {entitySkeleton, 100, 4, 4}, {entityCreeper, 100, 4, 4},
		{entitySlime, 100, 4, 4}, {entityEnderman, 10, 1, 4}, {entityWitch, 5, 1, 1},
	}
	monsterPoolSnowy = []spawnerEntry{
		{entitySpider, 100, 4, 4}, {entityZombie, 95, 4, 4}, {entityZombieVillager, 5, 1, 1},
		{entitySkeleton, 20, 4, 4}, {entityStray, 80, 4, 4}, {entityCreeper, 100, 4, 4},
		{entitySlime, 100, 4, 4}, {entityEnderman, 10, 1, 4}, {entityWitch, 5, 1, 1},
	}
	monsterPoolOcean = []spawnerEntry{ // vanilla oceans: drowned replace zombies
		{entitySpider, 100, 4, 4}, {entityDrowned, 95, 4, 4}, {entityZombieVillager, 5, 1, 1},
		{entitySkeleton, 100, 4, 4}, {entityCreeper, 100, 4, 4}, {entitySlime, 100, 4, 4},
		{entityEnderman, 10, 1, 4}, {entityWitch, 5, 1, 1},
	}
	monsterPoolRiver = []spawnerEntry{ // vanilla rivers add a heavy drowned entry
		{entitySpider, 100, 4, 4}, {entityZombie, 95, 4, 4}, {entityZombieVillager, 5, 1, 1},
		{entitySkeleton, 100, 4, 4}, {entityCreeper, 100, 4, 4}, {entitySlime, 100, 4, 4},
		{entityEnderman, 10, 1, 4}, {entityWitch, 5, 1, 1}, {entityDrowned, 100, 1, 1},
	}
)

// Creature pools: the vanilla farm four everywhere it applies, plus the
// per-biome signatures (vanilla weights/packs from OverworldBiomes).
var (
	creaturePoolDefault = []spawnerEntry{
		{entitySheep, 12, 4, 4}, {entityPig, 10, 4, 4}, {entityChicken, 10, 4, 4}, {entityCow, 8, 4, 4},
	}
	creaturePoolPlains = append([]spawnerEntry{
		{entityHorse, 5, 2, 6}, {entityDonkey, 1, 1, 3},
	}, creaturePoolDefault...)
	creaturePoolDesert = []spawnerEntry{{entityRabbit, 12, 2, 3}, {entityCamel, 1, 1, 1}} // no farm animals (1.21.11 biome report)
	creaturePoolSnowy  = []spawnerEntry{{entityRabbit, 10, 2, 3}, {entityPolarBear, 1, 1, 2}, {entityFox, 8, 2, 4}}
	creaturePoolTaiga  = append([]spawnerEntry{
		{entityWolf, 8, 4, 4}, {entityRabbit, 4, 2, 3}, {entityFox, 8, 2, 4},
	}, creaturePoolDefault...)
	creaturePoolJungle = append([]spawnerEntry{
		{entityParrot, 40, 1, 2}, {entityPanda, 1, 1, 2}, {entityOcelot, 2, 1, 3},
	}, creaturePoolDefault...)
	creaturePoolSavanna = append([]spawnerEntry{
		{entityHorse, 1, 2, 6}, {entityDonkey, 1, 1, 1}, {entityLlama, 8, 4, 4}, {entityArmadillo, 10, 2, 3},
	}, creaturePoolDefault...)
	creaturePoolPeaks = []spawnerEntry{{entityGoat, 5, 1, 3}}

	ambientPool = []spawnerEntry{{entityBat, 10, 8, 8}} // vanilla caveSpawns

	waterCreaturePool  = []spawnerEntry{{entitySquid, 10, 1, 4}, {entityDolphin, 2, 1, 2}}
	waterCreatureCaves = []spawnerEntry{{entityGlowSquid, 10, 4, 6}} // vanilla underground_water_creature
	waterAmbientWarm   = []spawnerEntry{{entityTropicalFish, 25, 8, 8}, {entityPufferfish, 15, 1, 3}, {entityNautilus, 2, 1, 2}}
	waterAmbientCold   = []spawnerEntry{{entityCod, 15, 3, 6}, {entitySalmon, 15, 1, 5}}
	waterAmbientOcean  = []spawnerEntry{{entityCod, 10, 3, 6}}
	waterAmbientRiver  = []spawnerEntry{{entitySalmon, 5, 1, 5}}
)

// mobSpawnCategory classifies a live mob for cap counting and despawning.
func mobSpawnCategory(m *mob) int {
	switch m.etype {
	case entityBat:
		return catAmbient
	case entityAxolotl:
		return catAxolotls
	case entitySquid, entityDolphin, entityNautilus:
		return catWaterCreature
	case entityGlowSquid:
		return catUndergroundWater
	case entityCod, entitySalmon, entityTropicalFish, entityPufferfish:
		return catWaterAmbient
	case entitySulfurCube, entityCamelHusk, entityZombieHorse, entityZombieNautilus:
		return catMonster // MobCategory.MONSTER, though none of them hunts
	}
	if m.hostile {
		return catMonster
	}
	return catCreature
}

// spawnExempt: mobs outside the natural-spawn economy — they neither count
// toward caps nor despawn (vanilla MISC category / persistence flags).
func (h *hub) spawnExempt(m *mob) bool {
	return m.tamed || m == h.dragon ||
		m.etype == entityVillager || m.etype == entityIronGolem ||
		m.etype == entitySnowGolem || m.etype == entityWanderingTrader
}

// countsTowardCaps is NaturalSpawner.createState's census filter: a mob
// counts against its category unless persistence is required of it (a
// name tag, picked-up gear, a tame) or it requires custom persistence (a
// lead, a seat, a bucket, a raid, an enderman's block); the MISC-category
// kinds (villagers, golems, traders, the dragon) never count.
func (h *hub) countsTowardCaps(m *mob) bool {
	return !h.spawnExempt(m) && !m.named() && !m.persistent && !h.requiresCustomPersistence(m)
}

// naturalSpawn runs every tick — the port of NaturalSpawner.spawnForChunk
// over the spawnable-chunk set of every dimension with a player in it (the
// union of those players' view windows), one position attempt per chunk.
func (h *hub) naturalSpawn(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || len(players) == 0 {
		return
	}
	for dim := 0; dim <= 2; dim++ {
		if dim != 0 && (h.worldFor(dim) == nil || h.worldFor(dim) == h.world) {
			continue // no such dimension on this server
		}
		h.naturalSpawnDim(players, dim)
	}
}

// naturalSpawnDim is one dimension's pass. chunkSet is the view window (mob
// load/unload + chunk-generation seeding); spawnRing is the vanilla
// SPAWN_DISTANCE_CHUNK=8 ring around each player — its size is the mob-cap
// denominator (NaturalSpawner.SpawnState.spawnableChunkCount), so one
// player's 17×17=289 spawn ring yields exactly maxInstancesPerChunk.
func (h *hub) naturalSpawnDim(players map[int32]*tracked, dim int) {
	if h.scratchChunks == nil {
		h.scratchChunks, h.scratchRing = map[[2]int32]bool{}, map[[2]int32]bool{}
	}
	clear(h.scratchChunks)
	clear(h.scratchRing)
	chunkSet, spawnRing := h.scratchChunks, h.scratchRing
	here := 0
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		here++
		r := t.p.radius()
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for x := cx - r; x <= cx+r; x++ {
			for z := cz - r; z <= cz+r; z++ {
				chunkSet[[2]int32{x, z}] = true
				if x >= cx-spawnDistChunks && x <= cx+spawnDistChunks &&
					z >= cz-spawnDistChunks && z <= cz+spawnDistChunks {
					spawnRing[[2]int32{x, z}] = true
				}
			}
		}
	}
	if here == 0 {
		return
	}
	chunks := make([][2]int32, 0, len(chunkSet))
	for c := range chunkSet {
		chunks = append(chunks, c)
	}

	if dim == 0 {
		// Load/unload mobs with their chunks before counting or spawning, so caps
		// see the reloaded packs and freed-up room from unloaded ones.
		h.reconcileMobChunks(players, chunkSet)
	}

	var counts [catCount]int
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || !h.countsTowardCaps(m) {
			continue
		}
		counts[mobSpawnCategory(m)]++
	}

	h.spawnVanilla(players, dim, chunks, chunkSet, len(spawnRing), &counts)
}

// spawnCap is vanilla NaturalSpawner.canSpawnForCategoryGlobal: the per-category
// ceiling on loaded non-persistent mobs, maxInstancesPerChunk × spawnable-chunk
// count / 289 (one player's 17×17 spawn ring → exactly maxInstancesPerChunk).
func spawnCap(cat, spawnRingSize int) int {
	return categoryCap[cat] * spawnRingSize / spawnChunkArea
}

// spawnPool picks the weighted species list for a category at a position:
// the biome's own data (the cave biome down a column, as vanilla samples
// the biome at the position), else the hand-written family fallback.
func (h *hub) spawnPool(dim, cat, x, y, z int) []spawnerEntry {
	w := h.worldFor(dim)
	if dim == dimNether && cat == catMonster && h.inFortressPiece(x, z) {
		return fortressSpawnPool // the structure's spawn override
	}
	if pool, ok := biomeSpawnPool(w.BiomeAt3D(x, y, z), cat); ok {
		return pool
	}
	biome := w.BiomeAt(x, z)
	switch cat {
	case catMonster:
		switch {
		case isDesertBiome(biome):
			return monsterPoolDesert
		case isColdBiome(biome):
			return monsterPoolSnowy
		case isOceanBiome(biome):
			return monsterPoolOcean
		case isRiverBiome(biome):
			return monsterPoolRiver
		}
		return monsterPoolDefault
	case catCreature:
		switch {
		case isMushroomBiome(biome):
			return creaturePoolMushroom
		case isBeachBiome(biome):
			return creaturePoolBeach
		case isMangroveBiome(biome):
			return creaturePoolMangrove
		case isSwampBiome(biome):
			return creaturePoolSwamp
		case biome == "minecraft:wooded_badlands":
			return creaturePoolWoodedBadlands
		case isBadlandsBiome(biome):
			return creaturePoolBadlands
		case isDesertBiome(biome):
			return creaturePoolDesert
		case isColdBiome(biome):
			return creaturePoolSnowy
		case isPeaksBiome(biome):
			return creaturePoolPeaks
		case isTaigaBiome(biome):
			return creaturePoolTaiga
		case isJungleBiome(biome):
			return creaturePoolJungle
		case isSavannaBiome(biome):
			return creaturePoolSavanna
		case isPlainsBiome(biome):
			return creaturePoolPlains
		}
		return creaturePoolDefault
	case catAmbient:
		return ambientPool
	case catAxolotls: // the lush_caves cave biome, wherever it is in the column
		if h.world.BiomeAt3D(x, y, z) == "minecraft:lush_caves" {
			return axolotlPool
		}
		return nil
	case catWaterCreature:
		if y < 30 { // vanilla underground_water_creature: glow squid in cave water
			return waterCreatureCaves
		}
		if isOceanBiome(biome) || isRiverBiome(biome) {
			return waterCreaturePool
		}
		return nil
	case catWaterAmbient:
		switch {
		case isWarmOceanBiome(biome):
			return waterAmbientWarm
		case isColdBiome(biome):
			return waterAmbientCold
		case isOceanBiome(biome):
			return waterAmbientOcean
		case isRiverBiome(biome):
			return waterAmbientRiver
		}
		return nil
	}
	return nil
}

// rollSpawner draws from a weighted pool (vanilla WeightedList.getRandom).
func (h *hub) rollSpawner(pool []spawnerEntry) (spawnerEntry, bool) {
	total := 0
	for _, e := range pool {
		total += e.weight
	}
	if total == 0 {
		return spawnerEntry{}, false
	}
	r := h.rng.Intn(total)
	for _, e := range pool {
		if r -= e.weight; r < 0 {
			return e, true
		}
	}
	return spawnerEntry{}, false
}

// spawnPositionOK is vanilla SpawnPlacements.isSpawnPositionOk: solid ground
// with two clear cells for land categories, open water for aquatic ones.
func (h *hub) spawnPositionOK(dim, cat, etype, x, y, z int) bool {
	// Every SpawnPlacementType begins with the border test, so this belongs
	// ahead of the rest of the checks rather than beside them.
	if !h.withinBorder(dim, float64(x)+0.5, float64(z)+0.5) {
		return false
	}
	w := h.worldFor(dim)
	at := w.At(x, y, z)
	// Drowned use vanilla's IN_WATER placement even though they are monsters:
	// water at the anchor with a non-solid block above (SpawnPlacementType
	// IN_WATER = fluid is water && block above is not a redstone conductor).
	if etype == entityDrowned {
		return worldgen.HoldsWater(at) && !worldgen.Collides(w.At(x, y+1, z))
	}
	if etype == entityStrider { // IN_LAVA placement: lava at the anchor
		return worldgen.IsLava(at)
	}
	switch cat {
	case catWaterCreature, catWaterAmbient, catUndergroundWater:
		return worldgen.HoldsWater(at) && worldgen.HoldsWater(w.At(x, y+1, z))
	case catAxolotls: // IN_WATER placement: water here, no conductor above
		return worldgen.HoldsWater(at) && !worldgen.Collides(w.At(x, y+1, z))
	}
	if worldgen.HoldsWater(at) || worldgen.IsLava(at) {
		return false
	}
	// vanilla isSpawnPositionOk ends in level.noCollision(getSpawnAABB(...)) —
	// the mob's WHOLE box has to fit. This used to test two cells flat, which
	// is right for the 1.95-high zombie family and wrong for everything
	// taller: an enderman is 2.9 blocks and needs three. Two-high pockets are
	// most of every cave system, so the short test made every one of them an
	// enderman spawn site that vanilla would have refused — which is why they
	// built up far past anything a vanilla world shows.
	for dy := 0; dy < spawnClearCells(etype); dy++ {
		if worldgen.Collides(w.At(x, y+dy, z)) {
			return false
		}
	}
	below := w.At(x, y-1, z)
	return worldgen.Collides(below) && !worldgen.IsThinFloor(below)
}

// spawnClearCells is how many blocks of headroom a species needs: its own
// height rounded up, and never fewer than the two the old test assumed.
func spawnClearCells(etype int) int {
	h := defaultMobBox.h
	if b, ok := mobBoxes[etype]; ok {
		h = b.h
	}
	return max(2, int(math.Ceil(h)))
}

// skyDarken is how much the sky's contribution to brightness is reduced by
// the time of day and the weather — vanilla's SKY_LIGHT_LEVEL environment
// attribute (26.x Timelines: 15 by day, 4 at night, keyframed dusk/dawn
// ramps; rain and thunder blend toward the night level). This is the
// mechanic that lets monsters spawn on the surface during a thunderstorm.
func (h *hub) skyDarken() int {
	day := float64(h.dayTime.Load() % dayLengthTicks)
	var level float64
	switch { // multiplier keyframes 133→1, 11867→1, 13670→4/15, 22330→4/15, wrap
	case day < 133:
		level = 4 + (15-4)*(day+24000-22330)/(133+24000-22330)
	case day < 11867:
		level = 15
	case day < 13670:
		level = 15 - (15-4)*(day-11867)/(13670-11867)
	case day < 22330:
		level = 4
	default:
		level = 4 + (15-4)*(day-22330)/(133+24000-22330)
	}
	// Weather blends toward the night level (alphas from WeatherAttributes).
	level += (4 - level) * 0.3125 * float64(h.rainLevel)
	level += (4 - level) * 0.52734375 * float64(h.thunderLevel)
	return 15 - int(level)
}

// rawBrightness is vanilla getRawBrightness(pos, amount): the larger of block
// light and (stored sky light − amount). `darken` is that amount — the number
// of levels subtracted from the raw sky value: pass -1 for the time/weather
// skyDarken (the normal getMaxLocalRawBrightness), 0 for the true raw value
// (animal spawn check), or a fixed amount like 10 (the thunderstorm rule,
// which REPLACES skyDarken rather than capping the sky).
func (h *hub) rawBrightness(sky, block uint8, darken int) int {
	if darken < 0 {
		darken = h.skyDarken()
	}
	s := int(sky) - darken
	if s < 0 {
		s = 0
	}
	return max(s, int(block))
}

// darkEnoughToSpawn is vanilla Monster.isDarkEnoughToSpawn: a random sky-light
// gate, a hard block-light zero (the overworld monsterSpawnBlockLightLimit —
// torches are absolute protection), then the effective brightness against a
// uniform(0,7) roll; thunderstorms cap the sky term at 10, which is what
// makes storms spawn monsters in daytime.
func (h *hub) darkEnoughToSpawn(sky, block uint8) bool {
	if int(sky) > h.rng.Intn(32) {
		return false
	}
	if block > 0 {
		return false
	}
	darken := -1 // normal: time/weather skyDarken
	if h.thundering {
		darken = 10 // vanilla getMaxLocalRawBrightness(pos, 10): a fixed −10, not a cap
	}
	return h.rawBrightness(sky, block, darken) <= h.rng.Intn(8)
}

// spawnRulesOK is the per-category/per-species spawn rule check (vanilla
// SpawnPlacements.checkSpawnRules dispatch).
func (h *hub) spawnRulesOK(dim, cat, etype, x, y, z int, sky, block uint8) bool {
	w := h.worldFor(dim)
	switch etype { // the species with their own SpawnPlacements predicate
	case entityOcelot: // Ocelot.checkOcelotSpawnRules: two tries in three, whatever the light (it sits in the jungle's monster pool)
		return h.rng.Intn(3) != 0
	case entityGhast: // Ghast.checkGhastSpawnRules: one try in twenty, any light
		return h.rules.Difficulty != diffPeaceful && h.rng.Intn(20) == 0
	case entityMagmaCube, entityBlaze: // checkMagmaCubeSpawnRules / checkAnyLightMonsterSpawnRules
		return h.rules.Difficulty != diffPeaceful
	case entitySulfurCube: // checkSulfurCubeSpawnRules: always — any light, any difficulty
		return true
	case entityZombifiedPiglin, entityPiglin, entityHoglin: // never on a nether wart block, any light
		return h.rules.Difficulty != diffPeaceful && w.At(x, y-1, z) != worldgen.NetherWartBlock
	case entityStrider: // Strider.checkStriderSpawnRules: air above the lava column
		yy := y + 1
		for worldgen.IsLava(w.At(x, yy, z)) && yy < 320 {
			yy++
		}
		return w.At(x, yy, z) == worldgen.Air
	}
	switch cat {
	case catUndergroundWater: // GlowSquid.checkGlowSquidSpawnRules: deep, unlit water
		return y <= worldgen.SeaLevel-33 && h.rawBrightness(sky, block, 0) == 0 && worldgen.IsWater(w.At(x, y, z))
	case catMonster:
		if !h.darkEnoughToSpawn(sky, block) {
			return false
		}
		switch etype {
		case entitySlime:
			return h.slimeSpawnOK(x, y, z)
		case entityHusk, entityStray: // vanilla: these need direct sky above
			return sky == 15
		case entityDrowned:
			// vanilla Drowned.checkDrownedSpawnRules: needs water in the block
			// below the anchor, then a rarity roll — 1/15 in river biomes
			// (#is_river = the MORE_FREQUENT_DROWNED_SPAWNS tag), else 1/40 and
			// only well below sea level (isDeepEnoughToSpawn). Water AT the anchor
			// and darkness are already enforced by placement + darkEnoughToSpawn.
			if !worldgen.IsWater(w.At(x, y-1, z)) {
				return false
			}
			if isRiverBiome(w.BiomeAt(x, z)) {
				return h.rng.Intn(15) == 0
			}
			return h.rng.Intn(40) == 0 && y < worldgen.SeaLevel-5
		}
		return true
	case catCreature: // vanilla Animal.checkAnimalSpawnRules: grass + light > 8
		below := w.At(x, y-1, z)
		if ok, handled := creatureFloorOK(etype, below, y); handled {
			if !ok {
				return false // the species' own spawnable-on tag (spawnspecies.go)
			}
		} else {
			switch below {
			case worldgen.GrassBlock, worldgen.Dirt, worldgen.SnowBlock, worldgen.Sand:
			default:
				return false
			}
		}
		// vanilla Animal.isBrightEnoughToSpawn: getRawBrightness(pos, 0) > 8 — the
		// TRUE raw light with no time-of-day darkening, so a sky-lit surface (15)
		// permits animal spawns day or night.
		return h.rawBrightness(sky, block, 0) > 8
	case catAmbient: // vanilla Bat.checkBatSpawnRules
		return y < w.SurfaceFeet(x, z) && h.rng.Intn(2) == 0 &&
			h.rawBrightness(sky, block, -1) <= h.rng.Intn(4) &&
			inRanges2(w.At(x, y-1, z), batFloor) // #bats_spawnable_on below
	case catWaterCreature: // squid/dolphin/nautilus near the surface band
		return y >= worldgen.SeaLevel-13 && y <= worldgen.SeaLevel
	case catWaterAmbient: // vanilla surface-water band
		return y >= worldgen.SeaLevel-13 && y <= worldgen.SeaLevel
	case catAxolotls: // checkAxolotlSpawnRules: clay below, no light rule
		return inRanges2(w.At(x, y-1, z), axolotlFloor)
	}
	return false
}

// slimeSpawnOK is vanilla Slime.checkSlimeSpawnRules: swamp surface spawns
// scale with the moon (y 50–70), and everywhere 1-in-10 slime chunks spawn
// them below y 40 at 1-in-10 odds per attempt.
func (h *hub) slimeSpawnOK(x, y, z int) bool {
	if isSwampBiome(h.world.BiomeAt(x, z)) && y > 50 && y < 70 &&
		h.rng.Float32() < 0.5*moonBrightness(h.dayTime.Load()) {
		return true
	}
	cx, cz := int32(chunkFloor(float64(x))), int32(chunkFloor(float64(z)))
	return y < 40 && h.rng.Intn(10) == 0 && isSlimeChunk(h.world.Seed(), cx, cz)
}

// isSlimeChunk reproduces vanilla WorldgenRandom.seedSlimeChunk + Java's
// Random.nextInt(10): the chunk-seed mixing wraps in 32-bit like the Java
// int arithmetic it comes from.
func isSlimeChunk(seed int64, cx, cz int32) bool {
	t1 := int64(cx * cx * 4987142) // int overflow wraps — deliberate
	t2 := int64(cx * 5947611)
	t3 := int64(int32(cz*cz)) * 4392871 // this term alone is long math in vanilla
	t4 := int64(cz * 389711)
	s := (seed + t1 + t2 + t3 + t4) ^ 987234911
	r := (s ^ 0x5DEECE66D) & (1<<48 - 1) // java.util.Random seed scramble
	// Java nextInt(10), non-power-of-two path
	for {
		r = (r*0x5DEECE66D + 0xB) & (1<<48 - 1)
		bits := int32(r >> 17) // next(31)
		val := bits % 10
		if bits-val+9 >= 0 {
			return val == 0
		}
	}
}

// spawnNatural creates the mob with its category wiring.
func (h *hub) spawnNatural(players map[int32]*tracked, dim, cat, etype, x, y, z int) {
	fx, fy, fz := float64(x)+0.5, float64(y), float64(z)+0.5
	switch {
	case etype == entitySulfurCube: // a monster that is no hunter: its own setup, never the hostile stance
		// AgeableMob.finalizeSpawn's group data, then setSpawnSize: after the
		// first of a pack, one in twenty is born a baby, and a baby is size 1.
		if m := h.spawnSulfurCube(players, dim, fx, fy, fz, false); m != nil {
			h.rollPackBaby(players, m)
			if m.baby {
				h.initSulfurCube(m, true)
			}
		}
	case dim == dimNether && netherConfigured(etype): // the nether's own kit (piglin family, cubes, blazes, ghasts, striders)
		m := h.spawnMobIn(players, etype, dim, fx, fy, fz)
		h.configureNetherMob(players, m)
		h.rollPackBaby(players, m)
	case cat == catMonster && etype != entityOcelot: // the ocelot is listed under the jungle's monsters but is no monster
		m := h.spawnHostileYIn(players, etype, dim, fx, fy, fz)
		if etype == entityHusk {
			h.rollCamelHusk(players, m) // Husk.finalizeSpawn: NATURAL spawns only
		}
		if etype == entityDrowned {
			h.rollDrownedNautilus(players, m, false) // Drowned.finalizeSpawn: NATURAL or STRUCTURE
		}
	case cat == catWaterCreature || cat == catWaterAmbient || cat == catAxolotls || cat == catUndergroundWater:
		h.spawnSpecies(players, etype, dim, fx, fy+0.5, fz)
	default:
		m := h.spawnMobIn(players, etype, dim, fx, fy, fz)
		h.applySpecies(players, m)
		h.rollPackBaby(players, m)
	}
}

// netherConfigured is the species configureNetherMob dresses: the ones the
// old nether pass spawned, with their nether stances.
func netherConfigured(etype int) bool {
	switch etype {
	case entityZombifiedPiglin, entityMagmaCube, entityBlaze, entityPiglin, entityHoglin, entityStrider, entityGhast:
		return true
	}
	return false
}

// rollPackBaby is AgeableMob.finalizeSpawn's group data for a natural pack:
// the first member is grown, each later one is born a baby at the species'
// chance (packBabyChance). Only inside a spawn-group scope.
func (h *hub) rollPackBaby(players map[int32]*tracked, m *mob) {
	g := h.spawnGroup
	if m == nil || g == nil {
		return
	}
	if ch := packBabyChance(m.etype); g.members > 0 && ch > 0 && h.rng.Float32() <= ch {
		m.baby, m.growLeft = true, growUpTicks
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	}
	g.members++
}

// nearestPlayerSq is the 3D squared distance to the closest player in the dimension.
func (h *hub) nearestPlayerSq(players map[int32]*tracked, dim int, x, y, z float64) float64 {
	best := math.Inf(1)
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		d := (t.x-x)*(t.x-x) + (t.y-y)*(t.y-y) + (t.z-z)*(t.z-z)
		if d < best {
			best = d
		}
	}
	return best
}

// Biome family classifiers for the pool tables (the generator's name set).
func isOceanBiome(b string) bool { return strings.Contains(b, "ocean") }
func isRiverBiome(b string) bool { return strings.Contains(b, "river") }
func isWarmOceanBiome(b string) bool {
	return strings.Contains(b, "warm_ocean") || strings.Contains(b, "lukewarm")
}
func isPeaksBiome(b string) bool {
	return strings.Contains(b, "peak") || strings.Contains(b, "slopes") || b == "minecraft:grove"
}
func isTaigaBiome(b string) bool  { return strings.Contains(b, "taiga") }
func isJungleBiome(b string) bool { return strings.Contains(b, "jungle") }
func isSavannaBiome(b string) bool {
	return strings.Contains(b, "savanna")
}
func isPlainsBiome(b string) bool { return strings.Contains(b, "plains") || b == "minecraft:meadow" }

// despawnSweep is vanilla Mob.checkDespawn for every mob once a second:
// instant beyond the category's despawn distance (water ambient 64, the
// rest 128) when the species removes itself when far away, and past 32
// blocks an idle clock runs — after 30 idle seconds each second has a
// ≈2.5% chance (the 1 Hz form of vanilla's per-tick 1/800). Persistence
// (a name tag, picked-up gear, a bucket, a lead, a rider's seat, a raid, an
// enderman's block) keeps a mob whatever the distance.
func (h *hub) despawnSweep(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, m := range h.mobs {
		if m.dying > 0 || h.spawnExempt(m) {
			continue
		}
		cat := mobSpawnCategory(m)
		if m.jockey {
			cat = catMonster // Chicken.removeWhenFarAway: a jockey's chicken goes like its rider
		}
		dist := categoryDespawnDist[cat]
		best := math.Inf(1)
		for _, t := range players {
			if t.dim != m.dim {
				continue
			}
			if d := (t.x-m.x)*(t.x-m.x) + (t.y-m.y)*(t.y-m.y) + (t.z-m.z)*(t.z-m.z); d < best { // Mob.checkDespawn: distanceToSqr, all three axes
				best = d
			}
		}
		// noActionTime runs whether or not the mob is persistent (serverAiStep
		// counts it every tick) and only a player within 32 blocks resets it.
		// Resetting it while persistent kept an enderman that set its block
		// down from ever despawning: it picks the next one up long before a
		// fresh 30-second clock runs out, and the carriers piled up (bug #31).
		if best < 32*32 {
			m.idleSecs = 0
		} else {
			m.idleSecs++
		}
		if m.named() || m.persistent || h.requiresCustomPersistence(m) {
			continue
		}
		switch {
		case best > float64(dist*dist) && h.removeWhenFarAway(m, best, now): // includes "no player in this dimension"
			h.removeMob(players, m)
		case best > 32*32 && m.idleSecs > 30 && h.rng.Intn(40) == 0 && h.removeWhenFarAway(m, best, now):
			h.removeMob(players, m)
		}
	}
}

// removeWhenFarAway is Mob.removeWhenFarAway with vanilla's overrides: an
// animal never (Animal), but a wild cat or ocelot does after two minutes
// alive, a jockey's chicken goes with its rider, nautiluses, zombie
// horses, hoglins and camel husks always, golems, allays, wardens,
// villagers and traders never, a zombie villager only while not curing, a
// raider never inside its raid, and a patrol captain only beyond 128.
func (h *hub) removeWhenFarAway(m *mob, d2 float64, now uint64) bool {
	age := now - m.spawnTick
	switch m.etype {
	case entityCat, entityOcelot: // Cat / Ocelot: !isTame (trusting) && tickCount > 2400
		return !m.tamed && age > 2400
	case entityChicken:
		return m.jockey
	case entityNautilus, entityZombieHorse, entityHoglin, entityCamelHusk:
		return true
	case entityAllay, entityIronGolem, entitySnowGolem, entityWarden, entityVillager, entityWanderingTrader:
		return false
	case entityZombieVillager:
		return m.converting == 0
	}
	if isIllager(m.etype) || m.etype == entityRavager || m.etype == entityWitch { // Raider
		if m.raidCenter != (blockPos{}) {
			return false
		}
		// PatrollingMonster.removeWhenFarAway: !patrolling || d > 16384 —
		// the whole patrol, not only its captain, once the captain has handed
		// out a waypoint.
		if m.patrolling || m.patrolCaptain {
			return d2 > 16384
		}
		return true
	}
	if mobSpawnCategory(m) == catCreature && !m.hostile {
		return false // Animal.removeWhenFarAway
	}
	return true
}

// requiresCustomPersistence is Mob.requiresCustomPersistence with vanilla's
// overrides: a passenger or a leashed mob, a bucketed fish or axolotl, a
// tamed nautilus, an enderman holding a block, a raider in a raid.
func (h *hub) requiresCustomPersistence(m *mob) bool {
	if m.mount != 0 || m.leash != 0 || m.fromBucket {
		return true
	}
	switch m.etype {
	case entityNautilus:
		return m.tamed
	case entityEnderman:
		return m.carriedBlock != 0
	case entitySulfurCube:
		return m.hasBody() // SulfurCube: a cube carrying a block stays
	}
	return (isIllager(m.etype) || m.etype == entityRavager || m.etype == entityWitch) && m.raidCenter != (blockPos{})
}

// summonAt is SummonCommand's spawn: the mob exactly where it was asked for,
// in the operator's dimension, set up as its natural spawn would be.
func (h *hub) summonAt(players map[int32]*tracked, e evSummon) {
	switch {
	case e.etype == entityEnderDragon:
		h.spawnHostileYIn(players, e.etype, e.dim, e.x, e.y, e.z)
	case e.etype == entitySulfurCube:
		h.spawnSulfurCube(players, e.dim, e.x, e.y, e.z, false)
	case e.etype == entityVillager || e.etype == entityIronGolem:
		h.configureVillageMob(players, h.spawnMobIn(players, e.etype, e.dim, e.x, e.y, e.z))
	case netherConfigured(e.etype):
		h.configureNetherMob(players, h.spawnMobIn(players, e.etype, e.dim, e.x, e.y, e.z))
	case isRosterPassive(e.etype):
		h.spawnSpecies(players, e.etype, e.dim, e.x, e.y, e.z)
	default:
		h.spawnHostileYIn(players, e.etype, e.dim, e.x, e.y, e.z)
	}
}
