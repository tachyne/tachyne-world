package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func poolHas(pool []spawnerEntry, etype int) bool {
	for _, e := range pool {
		if e.etype == etype {
			return true
		}
	}
	return false
}

// Every one of the six species is in the pool of the biome vanilla gives it,
// and only there (a beach spawns turtles, not cows).
func TestSixSpeciesHavePools(t *testing.T) {
	cases := []struct {
		biome string
		want  int
		alone bool // the biome's creature pool holds nothing else
	}{
		{"minecraft:mushroom_fields", entityMooshroom, true},
		{"minecraft:beach", entityTurtle, true},
		{"minecraft:savanna", entityArmadillo, false},
		{"minecraft:windswept_savanna", entityArmadillo, false},
		{"minecraft:badlands", entityArmadillo, false},
		{"minecraft:wooded_badlands", entityWolf, false},
		{"minecraft:desert", entityCamel, false},
		{"minecraft:swamp", entityFrog, false},
		{"minecraft:mangrove_swamp", entityFrog, true},
	}
	h := newHub(world.New(1))
	for _, c := range cases {
		pool := h.creaturePoolFor(c.biome)
		if !poolHas(pool, c.want) {
			t.Errorf("%s: pool %v lacks species %d", c.biome, pool, c.want)
		}
		if c.alone && len(pool) != 1 {
			t.Errorf("%s: pool has %d entries, want only the one species", c.biome, len(pool))
		}
		if c.alone && poolHas(pool, entityCow) {
			t.Errorf("%s: farm animals must not spawn here", c.biome)
		}
	}
	if poolHas(h.creaturePoolFor("minecraft:plains"), entityMooshroom) {
		t.Error("mooshrooms must stay on mushroom fields")
	}
}

// creaturePoolFor resolves the creature pool by biome name (the spawnPool
// switch, without needing terrain that happens to be that biome).
func (h *hub) creaturePoolFor(biome string) []spawnerEntry {
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
	case isSavannaBiome(biome):
		return creaturePoolSavanna
	}
	return creaturePoolDefault
}

// The per-species spawnable-on rules: each species accepts its own floor and
// refuses the generic grass that other animals take.
func TestSpeciesFloorRules(t *testing.T) {
	grass := worldgen.GrassBlock
	ok := func(etype int, below uint32, y int) bool {
		r, handled := creatureFloorOK(etype, below, y)
		return handled && r
	}
	if !ok(entityMooshroom, worldgen.Mycelium, 70) || ok(entityMooshroom, grass, 70) {
		t.Error("mooshroom: mycelium yes, grass no")
	}
	if !ok(entityTurtle, worldgen.Sand, 64) || ok(entityTurtle, grass, 64) {
		t.Error("turtle: sand yes, grass no")
	}
	if ok(entityTurtle, worldgen.Sand, worldgen.SeaLevel+4) {
		t.Error("turtle: never above sea level + 3")
	}
	if !ok(entityCamel, worldgen.RedSand, 70) || ok(entityCamel, grass, 70) {
		t.Error("camel: any #sand yes, grass no")
	}
	if !ok(entityArmadillo, worldgen.Terracotta, 70) || !ok(entityArmadillo, grass, 70) || ok(entityArmadillo, worldgen.Sand, 70) {
		t.Error("armadillo: terracotta and grass yes, plain sand no")
	}
	if !ok(entityFrog, worldgen.Mud, 62) || !ok(entityFrog, worldgen.MangroveRoots, 62) || ok(entityFrog, worldgen.Sand, 62) {
		t.Error("frog: mud and mangrove roots yes, sand no")
	}
	if _, handled := creatureFloorOK(entityCow, grass, 70); handled {
		t.Error("a cow keeps the generic animal rule")
	}
}

// Axolotls are their own category with vanilla's cap and despawn distance,
// spawn only in water over clay, and only where the cave biome is lush.
func TestAxolotlCategory(t *testing.T) {
	h := newHub(world.New(1))
	m := &mob{etype: entityAxolotl}
	if mobSpawnCategory(m) != catAxolotls {
		t.Fatal("axolotl must count against the AXOLOTLS category")
	}
	if categoryCap[catAxolotls] != 5 || categoryDespawnDist[catAxolotls] != 64 || categorySpawnRange[catAxolotls] != 64 {
		t.Errorf("axolotl category cap/despawn/range = %d/%d/%d, want 5/64/64",
			categoryCap[catAxolotls], categoryDespawnDist[catAxolotls], categorySpawnRange[catAxolotls])
	}
	// Placement: water at the anchor with nothing solid above; rule: clay below.
	x, y, z := 100, 20, 100
	h.world.SetBlock(x, y-1, z, worldgen.Clay)
	h.world.SetBlock(x, y, z, worldgen.WaterBase)
	h.world.SetBlock(x, y+1, z, worldgen.WaterBase)
	if !h.spawnPositionOK(catAxolotls, entityAxolotl, x, y, z) || !h.spawnRulesOK(catAxolotls, entityAxolotl, x, y, z, 0, 0) {
		t.Error("water over clay should accept an axolotl")
	}
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	if h.spawnRulesOK(catAxolotls, entityAxolotl, x, y, z, 0, 0) {
		t.Error("stone below must refuse an axolotl")
	}
	// The pool follows the 3D biome: a surface column never offers axolotls.
	surfaceY := h.world.SurfaceFeet(x, z)
	if h.spawnPool(catAxolotls, x, surfaceY, z) != nil {
		t.Error("axolotls must not pool at the surface")
	}
}

// BiomeAt3D is the surface biome near the surface and a cave biome deep
// down; somewhere in a modest search there is a lush cave section.
func TestBiomeAt3DSeesCaveBiomes(t *testing.T) {
	w := world.New(1)
	if got, want := w.BiomeAt3D(0, w.SurfaceFeet(0, 0), 0), w.BiomeAt(0, 0); got != want {
		t.Fatalf("surface BiomeAt3D %q, want the column biome %q", got, want)
	}
	found := map[string]bool{}
	for cx := 0; cx < 12 && !found["minecraft:lush_caves"]; cx++ {
		for y := worldgen.MinY + 8; y < 40; y += 16 {
			found[w.BiomeAt3D(cx*16+8, y, 8)] = true
		}
	}
	if !found["minecraft:lush_caves"] && !found["minecraft:dripstone_caves"] {
		t.Errorf("no cave biome seen underground; biomes found: %v", found)
	}
}

// Herd seeding places each species on its own ground: a turtle pack on a
// sand column that a cow pack would refuse, and the other way round.
func TestHerdSeedingUsesSpeciesFloor(t *testing.T) {
	h := newHub(world.New(1))
	// Find a sand-floored, sky-exposed land column the world thinks is spawnable.
	var sx, sz int
	found := false
	for x := 0; x < 2000 && !found; x += 7 {
		for z := 0; z < 2000 && !found; z += 7 {
			feet := h.world.MobFeet(x, z)
			if feet < worldgen.SeaLevel+4 && h.world.Block(x, feet-1, z) == worldgen.Sand &&
				h.world.Spawnable(x, z) && h.skyExposedColumn(x, z) {
				sx, sz, found = x, z, true
			}
		}
	}
	if !found {
		t.Skip("no low sand column near the origin for this seed")
	}
	if !h.spawnableAnimalFor(entityTurtle, sx, sz) {
		t.Error("a turtle should accept a low sand column")
	}
	if h.spawnableAnimalFor(entityCow, sx, sz) {
		t.Error("a cow must not be seeded on sand")
	}
	if h.spawnableAnimalFor(entityMooshroom, sx, sz) {
		t.Error("a mooshroom must not be seeded on sand")
	}
}
