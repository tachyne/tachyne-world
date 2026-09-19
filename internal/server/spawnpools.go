package server

import "log"

// The natural-spawn pools are vanilla's own biome data (MobSpawnSettings:
// spawnpools_gen.go, baked from the canonical jar's worldgen/biome files):
// per biome, per category, the weighted (species, pack min, pack max)
// entries NaturalSpawner draws from, and the biome's creature-generation
// probability for the chunk-generation packs. The hand-written family
// pools in spawn.go remain only as the fallback for a biome name the data
// does not know.

// spawnCatByName maps the data's category names to the engine's.
var spawnCatByName = map[string]int{
	"monster":                    catMonster,
	"creature":                   catCreature,
	"ambient":                    catAmbient,
	"water_creature":             catWaterCreature,
	"water_ambient":              catWaterAmbient,
	"axolotls":                   catAxolotls,
	"underground_water_creature": catUndergroundWater,
}

// biomeSpawns is one biome's resolved pools.
type biomeSpawns struct {
	prob  float32
	pools [catCount][]spawnerEntry
	costs map[int]biomeSpawnCost // MobSpawnCost by species (the soul sand valley's and warped forest's charges)
}

// biomeSpawnPools resolves the baked data to entity ids once; a species the
// engine does not know is dropped (and logged) rather than failing the boot.
var biomeSpawnPools = func() map[string]biomeSpawns {
	out := make(map[string]biomeSpawns, len(biomeSpawnDefs))
	unknown := map[string]bool{}
	for biome, def := range biomeSpawnDefs {
		bs := biomeSpawns{prob: def.prob}
		for _, c := range def.costs {
			if id, ok := entityByName[c.etype]; ok {
				if bs.costs == nil {
					bs.costs = map[int]biomeSpawnCost{}
				}
				bs.costs[id] = c
			}
		}
		for _, r := range def.rows {
			cat, ok := spawnCatByName[r.cat]
			if !ok {
				continue // "misc" is never a natural-spawn category
			}
			id, ok := entityByName[r.etype]
			if !ok {
				unknown[r.etype] = true
				continue
			}
			bs.pools[cat] = append(bs.pools[cat], spawnerEntry{etype: id, weight: r.weight, min: r.min, max: r.max})
		}
		out[biome] = bs
	}
	for name := range unknown {
		log.Printf("spawn pools: species %q in the biome data is unknown to this engine; skipped", name)
	}
	return out
}()

// biomeSpawnPool is a biome's pool for a category, ok when the data knows
// the biome.
func biomeSpawnPool(biome string, cat int) ([]spawnerEntry, bool) {
	bs, ok := biomeSpawnPools[biome]
	if !ok {
		return nil, false
	}
	return bs.pools[cat], true
}

// spawnCostFor is MobSpawnSettings.getMobSpawnCost for a species in a biome.
func spawnCostFor(biome string, etype int) (biomeSpawnCost, bool) {
	bs, ok := biomeSpawnPools[biome]
	if !ok || bs.costs == nil {
		return biomeSpawnCost{}, false
	}
	c, ok := bs.costs[etype]
	return c, ok
}

// fortressSpawnPool is NetherFortressStructure.FORTRESS_ENEMIES (fortress.go's
// table) as a spawner pool: inside a fortress piece the monster pool is the
// garrison's, not the biome's.
var fortressSpawnPool = func() []spawnerEntry {
	out := make([]spawnerEntry, 0, len(fortressPool))
	for _, e := range fortressPool {
		out = append(out, spawnerEntry{etype: e.etype, weight: e.weight, min: e.min, max: e.max})
	}
	return out
}()

// biomeCreatureProbability is MobSpawnSettings.getCreatureProbability: the
// geometric-loop probability of the chunk-generation packs (0.1 unless the
// biome says otherwise — the badlands and snowy plains are sparser).
func biomeCreatureProbability(biome string) float32 {
	if bs, ok := biomeSpawnPools[biome]; ok {
		return bs.prob
	}
	return creatureGenProbability
}

// maxSpawnClusterFor is Mob.getMaxSpawnClusterSize: how many of a species
// one spawn position may place across its packs (the default four; fish
// school in eights, wolves in eights, the horse family in sixes, and
// ghasts, happy ghasts and pillagers come alone).
func maxSpawnClusterFor(etype int) int {
	switch etype {
	case entityCod, entitySalmon, entityTropicalFish, entityPufferfish, entityWolf:
		return 8
	case entityHorse, entityDonkey, entityMule, entitySkeletonHorse, entityZombieHorse, entityCamel:
		return 6
	case entityGhast, entityHappyGhast, entityPillager:
		return 1
	}
	return maxSpawnCluster
}

// packBabyChance is AgeableMob.finalizeSpawn's group data: after the first
// member of a natural pack, each further ageable animal is born a baby at
// this chance — one in twenty by default, every rabbit after the first,
// and never for the species whose group data says no babies (axolotl, fox,
// wolf, parrot). Monsters and the non-ageable are 0.
func packBabyChance(etype int) float32 {
	if etype == entityRabbit {
		return 1
	}
	if packBabySpecies[etype] {
		return 0.05
	}
	return 0
}

// packBabySpecies is the ageable animals of the biome pools whose group
// data keeps the default baby chance (the axolotl, fox, wolf and parrot
// say no; the rabbit is its own case).
var packBabySpecies = map[int]bool{
	entityCow: true, entityPig: true, entitySheep: true, entityChicken: true, entityMooshroom: true,
	entityHorse: true, entityDonkey: true, entityLlama: true, entityGoat: true, entityPanda: true,
	entityPolarBear: true, entityTurtle: true, entityFrog: true, entityArmadillo: true, entityCamel: true,
	entityStrider: true,
}
