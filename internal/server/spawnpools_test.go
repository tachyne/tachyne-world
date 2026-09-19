package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The natural-spawn pools are vanilla's biome data: plains cows come in
// fours at weight eight, lush caves carry the axolotl pool, the glow squid
// is its own capped category, badlands packs are rarer, and a biome the
// data does not know falls back to the family pools.
func TestBiomeSpawnPools(t *testing.T) {
	pool, ok := biomeSpawnPool("minecraft:plains", catCreature)
	if !ok {
		t.Fatal("plains should come from the data")
	}
	found := false
	for _, e := range pool {
		if e.etype == entityCow {
			found = e.weight == 8 && e.min == 4 && e.max == 4
		}
	}
	if !found {
		t.Fatalf("plains cows: weight 8 in packs of 4, got %+v", pool)
	}
	if p, _ := biomeSpawnPool("minecraft:lush_caves", catAxolotls); len(p) != 1 || p[0].etype != entityAxolotl {
		t.Fatalf("lush caves axolotls: %+v", p)
	}
	if p, _ := biomeSpawnPool("minecraft:lush_caves", catUndergroundWater); len(p) != 1 || p[0].etype != entityGlowSquid {
		t.Fatalf("lush caves glow squid: %+v", p)
	}
	if mobSpawnCategory(&mob{etype: entityGlowSquid}) != catUndergroundWater || categoryCap[catUndergroundWater] != 5 {
		t.Fatal("the glow squid is UNDERGROUND_WATER_CREATURE, capped at five")
	}
	if biomeCreatureProbability("minecraft:badlands") != 0.03 || biomeCreatureProbability("minecraft:plains") != 0.1 {
		t.Fatal("creature_spawn_probability comes from the biome")
	}
	if _, ok := biomeSpawnPool("minecraft:not_a_biome", catCreature); ok {
		t.Fatal("an unknown biome is not in the data")
	}
	if maxSpawnClusterFor(entityCod) != 8 || maxSpawnClusterFor(entityHorse) != 6 || maxSpawnClusterFor(entityGhast) != 1 || maxSpawnClusterFor(entityCow) != 4 {
		t.Fatal("per-species spawn cluster sizes")
	}
	if packBabyChance(entityRabbit) != 1 || packBabyChance(entityWolf) != 0 || packBabyChance(entityCow) != 0.05 || packBabyChance(entityZombie) != 0 {
		t.Fatal("pack baby chances")
	}
}

// The local mob cap: with two players far apart, a crowd of cows around one
// blocks creature spawns in that player's chunks and nobody else's; a chunk
// no player is close to spawns nothing.
func TestLocalMobCap(t *testing.T) {
	h := newHub(world.New(1))
	a, b := testTracked(), testTracked()
	a.x, a.y, a.z = 0.5, 64, 0.5
	b.x, b.y, b.z = 1000.5, 64, 0.5
	b.p = newPlayer(2, "second", [16]byte{})
	players := map[int32]*tracked{1: a, 2: b}
	for i := 0; i < categoryCap[catCreature]; i++ {
		h.spawnMob(players, entityCow, float64(i*3)+0.5, 180, 20.5)
	}
	h.buildLocalCaps(players)
	if h.localCapAllows(catCreature, [2]int32{1, 1}) {
		t.Fatal("the first player's chunks are at the creature cap")
	}
	if !h.localCapAllows(catMonster, [2]int32{1, 1}) {
		t.Fatal("another category is unaffected")
	}
	if !h.localCapAllows(catCreature, [2]int32{62, 1}) {
		t.Fatal("the second player's chunks are free")
	}
	if h.localCapAllows(catCreature, [2]int32{30, 30}) {
		t.Fatal("a chunk with no player within 128 blocks spawns nothing")
	}
	h.localCapAdd(catCreature, 1000, 0)
	for i, p := range h.localCaps.players {
		if p == b && h.localCaps.counts[i][catCreature] != 1 {
			t.Fatal("a spawn near the second player counts for them")
		}
	}
}

// cullSpawnCows removes only the wild cows near the origin.
func TestCullSpawnCows(t *testing.T) {
	s := newMobStore("")
	s.m.Chunks = map[string][]savedMob{
		"0,0":   {{Etype: entityCow, X: 5, Z: 5}, {Etype: entityCow, X: 6, Z: 5, Tamed: true}, {Etype: entitySheep, X: 7, Z: 5}},
		"20,20": {{Etype: entityCow, X: 325, Z: 325}},
		"1,1":   {{Etype: entityCow, X: 20, Z: 20, CustomName: "Daisy"}},
	}
	before, after := s.cullSpawnCows(entityCow, 160)
	if before != 5 || after != 4 {
		t.Fatalf("cull %d -> %d, want 5 -> 4", before, after)
	}
	if len(s.m.Chunks["0,0"]) != 2 || len(s.m.Chunks["20,20"]) != 1 || len(s.m.Chunks["1,1"]) != 1 {
		t.Fatalf("kept the tamed, the named, the sheep and the far cow: %+v", s.m.Chunks)
	}
}
