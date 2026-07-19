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
	catCount
)

var categoryCap = [catCount]int{70, 10, 15, 5, 20}
var categoryDespawnDist = [catCount]int{128, -1, 128, 128, 64}

const (
	spawnChunkArea   = 17 * 17 // vanilla MAGIC_NUMBER: cap scale denominator
	creatureSpawnMod = 400     // persistent categories spawn every 400 ticks
	maxSpawnCluster  = 4       // vanilla Mob.getMaxSpawnClusterSize default
	// Vanilla attempts one position per loaded chunk per tick; we sample at
	// one per 8 chunks per tick per category. Most attempts die instantly
	// (random Y lands inside rock), so a higher rate is what makes cave
	// density feel vanilla — affordable now that chunk light is cached in
	// the world (the first live tuning at 1/64 left caves near-empty).
	spawnAttemptBatch = 8
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
	creaturePoolDesert = []spawnerEntry{{entityRabbit, 4, 2, 3}} // vanilla deserts have no farm animals
	creaturePoolSnowy  = []spawnerEntry{{entityRabbit, 10, 2, 3}, {entityPolarBear, 1, 1, 2}, {entityFox, 8, 2, 4}}
	creaturePoolTaiga  = append([]spawnerEntry{
		{entityWolf, 8, 4, 4}, {entityRabbit, 4, 2, 3}, {entityFox, 8, 2, 4},
	}, creaturePoolDefault...)
	creaturePoolJungle = append([]spawnerEntry{
		{entityParrot, 40, 1, 2}, {entityPanda, 1, 1, 2}, {entityOcelot, 2, 1, 3},
	}, creaturePoolDefault...)
	creaturePoolSavanna = append([]spawnerEntry{
		{entityHorse, 1, 2, 6}, {entityDonkey, 1, 1, 1}, {entityLlama, 8, 4, 4},
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
	case entitySquid, entityDolphin, entityGlowSquid, entityAxolotl:
		return catWaterCreature
	case entityCod, entitySalmon, entityTropicalFish, entityPufferfish, entityNautilus:
		return catWaterAmbient
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

// naturalSpawn runs every tick — the port of NaturalSpawner.spawnForChunk
// over the spawnable-chunk set (the union of overworld players' view
// windows). Vanilla attempts one position per chunk per tick; we sample the
// set at one attempt per 64 chunks per tick per category, which converges on
// the same caps within seconds while bounding hub (and light-engine) cost.
func (h *hub) naturalSpawn(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || len(players) == 0 {
		return
	}
	// Spawnable chunks + the players list once per tick.
	chunkSet := map[[2]int32]bool{}
	overworld := 0
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		overworld++
		r := t.p.radius()
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for x := cx - r; x <= cx+r; x++ {
			for z := cz - r; z <= cz+r; z++ {
				chunkSet[[2]int32{x, z}] = true
			}
		}
	}
	if overworld == 0 {
		return
	}
	chunks := make([][2]int32, 0, len(chunkSet))
	for c := range chunkSet {
		chunks = append(chunks, c)
	}

	// Load/unload mobs with their chunks before counting or spawning, so caps see
	// the reloaded herds and freed-up room from unloaded ones.
	h.reconcileMobChunks(players, chunkSet)

	var counts [catCount]int
	for _, m := range h.mobs {
		if m.dim != 0 || m.dying > 0 || h.spawnExempt(m) {
			continue
		}
		counts[mobSpawnCategory(m)]++
	}

	// Two selectable spawners (admin: -spawner). Vanilla mode is the exact
	// NaturalSpawner (one attempt per chunk per tick + chunk-generation herds);
	// the default tachyne mode is the cheaper 1/8 sampler paired with herdTopUp.
	if h.vanillaSpawner {
		h.spawnVanilla(players, chunks, chunkSet, &counts)
		return
	}
	h.spawnTachyne(players, chunks, &counts)
}

// spawnTachyne is the default sampler: one position attempt per spawnAttemptBatch
// chunks per tick per category — cheaper than vanilla's per-chunk rate, tuned to
// converge on the same caps (see natural-spawn tuning notes).
func (h *hub) spawnTachyne(players map[int32]*tracked, chunks [][2]int32, counts *[catCount]int) {
	spawnPersistent := h.tick.Load()%creatureSpawnMod == 0
	attempts := (len(chunks) + spawnAttemptBatch - 1) / spawnAttemptBatch
	for cat := 0; cat < catCount; cat++ {
		if cat == catMonster && h.rules.Difficulty == diffPeaceful {
			continue
		}
		if cat == catCreature && !spawnPersistent {
			continue // vanilla: persistent-category spawns only every 400 ticks
		}
		cap := categoryCap[cat] * len(chunks) / spawnChunkArea
		for a := 0; a < attempts && counts[cat] < cap; a++ {
			h.spawnAttempt(players, cat, chunks, &counts[cat], cap)
		}
	}
}

// spawnAttempt is one vanilla spawnCategoryForPosition: a random column in a
// random loaded chunk, a random height through the whole column (this is
// what populates caves), then a pack that random-walks around the anchor
// with every member re-checked for ground, distance and light.
func (h *hub) spawnAttempt(players map[int32]*tracked, cat int, chunks [][2]int32, count *int, cap int) {
	c := chunks[h.rng.Intn(len(chunks))]
	x := int(c[0])*16 + h.rng.Intn(16)
	z := int(c[1])*16 + h.rng.Intn(16)
	surface := h.world.SurfaceFeet(x, z)
	y := worldgen.MinY + h.rng.Intn(surface+2-worldgen.MinY) // vanilla: uniform [minY, surface+1]
	if worldgen.Collides(h.world.At(x, y, z)) {
		return // vanilla: anchor inside a solid block aborts the attempt
	}
	pool := h.spawnPool(cat, x, y, z)
	if len(pool) == 0 {
		return
	}
	sd, ok := h.rollSpawner(pool)
	if !ok {
		return
	}
	pack := sd.min + h.rng.Intn(sd.max-sd.min+1)
	spawned := 0
	for i := 0; i < pack; i++ {
		x += h.rng.Intn(6) - h.rng.Intn(6)
		z += h.rng.Intn(6) - h.rng.Intn(6)
		if !h.ownedBlock(x, z) {
			continue
		}
		if d := h.nearestPlayerSq(players, float64(x)+0.5, float64(y), float64(z)+0.5); d <= 576 ||
			(categoryDespawnDist[cat] > 0 && d > float64(categoryDespawnDist[cat]*categoryDespawnDist[cat])) {
			continue // vanilla: never within 24 blocks of a player, never where it would instantly despawn
		}
		if h.nearWorldSpawn(x, y, z) {
			continue // vanilla: never within 24 blocks of world spawn
		}
		if !h.spawnPositionOK(cat, sd.etype, x, y, z) {
			continue
		}
		sky, block := h.world.LightAt(x, y, z) // cached in world, invalidated by edits
		if !h.spawnRulesOK(cat, sd.etype, x, y, z, sky, block) {
			continue
		}
		h.spawnNatural(players, cat, sd.etype, x, y, z)
		*count++
		if spawned++; spawned >= maxSpawnCluster || *count >= cap {
			return
		}
	}
}

// spawnPool picks the weighted species list for a category at a position.
func (h *hub) spawnPool(cat, x, y, z int) []spawnerEntry {
	biome := h.world.BiomeAt(x, z)
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
func (h *hub) spawnPositionOK(cat, etype, x, y, z int) bool {
	at := h.world.At(x, y, z)
	// Drowned use vanilla's IN_WATER placement even though they are monsters:
	// water at the anchor with a non-solid block above (SpawnPlacementType
	// IN_WATER = fluid is water && block above is not a redstone conductor).
	if etype == entityDrowned {
		return worldgen.IsWater(at) && !worldgen.Collides(h.world.At(x, y+1, z))
	}
	switch cat {
	case catWaterCreature, catWaterAmbient:
		return worldgen.IsWater(at) && worldgen.IsWater(h.world.At(x, y+1, z))
	}
	if worldgen.IsWater(at) || worldgen.IsLava(at) ||
		worldgen.Collides(at) || worldgen.Collides(h.world.At(x, y+1, z)) {
		return false
	}
	below := h.world.At(x, y-1, z)
	return worldgen.Collides(below) && !worldgen.IsThinFloor(below)
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
func (h *hub) spawnRulesOK(cat, etype, x, y, z int, sky, block uint8) bool {
	switch cat {
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
			if !worldgen.IsWater(h.world.At(x, y-1, z)) {
				return false
			}
			if isRiverBiome(h.world.BiomeAt(x, z)) {
				return h.rng.Intn(15) == 0
			}
			return h.rng.Intn(40) == 0 && y < worldgen.SeaLevel-5
		}
		return true
	case catCreature: // vanilla Animal.checkAnimalSpawnRules: grass + light > 8
		switch h.world.At(x, y-1, z) {
		case worldgen.GrassBlock, worldgen.Dirt, worldgen.SnowBlock, worldgen.Sand:
		default:
			return false
		}
		// vanilla Animal.isBrightEnoughToSpawn: getRawBrightness(pos, 0) > 8 — the
		// TRUE raw light with no time-of-day darkening, so a sky-lit surface (15)
		// permits animal spawns day or night.
		return h.rawBrightness(sky, block, 0) > 8
	case catAmbient: // vanilla Bat.checkBatSpawnRules
		return y < h.world.SurfaceFeet(x, z) && h.rng.Intn(2) == 0 &&
			h.rawBrightness(sky, block, -1) <= h.rng.Intn(4)
	case catWaterCreature: // squid/dolphin near the surface band, glow squid any cave depth
		return y < 30 || (y >= worldgen.SeaLevel-13 && y <= worldgen.SeaLevel)
	case catWaterAmbient: // vanilla surface-water band
		return y >= worldgen.SeaLevel-13 && y <= worldgen.SeaLevel
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
func (h *hub) spawnNatural(players map[int32]*tracked, cat, etype, x, y, z int) {
	fx, fy, fz := float64(x)+0.5, float64(y), float64(z)+0.5
	switch cat {
	case catMonster:
		h.spawnHostileY(players, etype, fx, fy, fz)
	case catWaterCreature, catWaterAmbient:
		h.spawnSpecies(players, etype, 0, fx, fy+0.5, fz)
	default:
		m := h.spawnMob(players, etype, fx, fy, fz)
		h.applySpecies(players, m)
	}
}

// nearestPlayerSq is the 3D squared distance to the closest overworld player.
func (h *hub) nearestPlayerSq(players map[int32]*tracked, x, y, z float64) float64 {
	best := math.Inf(1)
	for _, t := range players {
		if t.dim != 0 {
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

// despawnSweep is vanilla Mob.checkDespawn for every non-persistent mob:
// instant beyond the category's despawn distance (water ambient 64, others
// 128), and past 32 blocks an idle clock runs — after 30 idle seconds each
// second has a ≈2.5% chance (the 1 Hz form of vanilla's per-tick 1/800).
func (h *hub) despawnSweep(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.dying > 0 || h.spawnExempt(m) {
			continue
		}
		cat := mobSpawnCategory(m)
		dist := categoryDespawnDist[cat]
		if dist < 0 {
			continue // creatures are persistent
		}
		best := math.Inf(1)
		for _, t := range players {
			if t.dim != m.dim {
				continue
			}
			if d := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d < best {
				best = d
			}
		}
		switch {
		case best > float64(dist*dist): // includes "no player in this dimension"
			h.removeMob(players, m)
		case best > 32*32:
			if m.idleSecs++; m.idleSecs > 30 && h.rng.Intn(40) == 0 {
				h.removeMob(players, m)
			}
		default:
			m.idleSecs = 0
		}
	}
}

// herdTopUp approximates vanilla's chunk-generation creature herds: our
// chunks regenerate on the fly and mobs don't persist, so exploration and
// restarts would leave the countryside bare without it. Every 30 s, if the
// nearby creature population is thin, seed one vanilla farm pack out of
// sight of a random player.
func (h *hub) herdTopUp(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning {
		return
	}
	var pick *tracked
	for _, t := range players {
		if t.dim == 0 {
			pick = t
			break
		}
	}
	if pick == nil {
		return
	}
	near := 0
	for _, m := range h.mobs {
		if m.dim != 0 || m.dying > 0 || h.spawnExempt(m) || mobSpawnCategory(m) != catCreature {
			continue
		}
		if (m.x-pick.x)*(m.x-pick.x)+(m.z-pick.z)*(m.z-pick.z) < 96*96 {
			near++
		}
	}
	if near >= 8 {
		return
	}
	ang := h.rng.Float64() * 2 * math.Pi
	dist := spawnMinDist + h.rng.Intn(56)
	cx := int(pick.x) + int(math.Cos(ang)*float64(dist))
	cz := int(pick.z) + int(math.Sin(ang)*float64(dist))
	if !h.ownedBlock(cx, cz) || !h.spawnableAnimal(cx, cz) {
		return
	}
	pool := h.spawnPool(catCreature, cx, h.world.MobFeet(cx, cz), cz)
	sd, ok := h.rollSpawner(pool)
	if !ok {
		return
	}
	occupied := map[[2]int]bool{}
	pack := sd.min + h.rng.Intn(sd.max-sd.min+1)
	for i := 0; i < pack; i++ {
		x, z := h.spreadSpawn(cx, cz, occupied)
		h.spawnAnimal(players, sd.etype, x, z)
	}
}
