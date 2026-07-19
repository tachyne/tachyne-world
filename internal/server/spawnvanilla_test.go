package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestVanillaSeedChunksOnceAndBudget: the chunk-generation pass seeds at most
// chunkSeedBudget new chunks per tick, seeds every chunk exactly once, and never
// re-seeds a chunk it has already handled.
func TestVanillaSeedChunksOnceAndBudget(t *testing.T) {
	h := newHub(world.New(1))
	h.vanillaSpawner = true
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}

	chunkSet := map[[2]int32]bool{}
	for x := int32(0); x < 20; x++ {
		chunkSet[[2]int32{x, 0}] = true
	}
	var counts [catCount]int

	h.seedChunkGeneration(players, chunkSet, &counts)
	if len(h.seededChunks) != chunkSeedBudget {
		t.Fatalf("first tick should seed exactly the budget %d, got %d", chunkSeedBudget, len(h.seededChunks))
	}
	for i := 0; i < 5; i++ { // drain the remainder over later ticks
		h.seedChunkGeneration(players, chunkSet, &counts)
	}
	if len(h.seededChunks) != 20 {
		t.Fatalf("every chunk should eventually seed once, got %d", len(h.seededChunks))
	}
	before := len(h.seededChunks)
	h.seedChunkGeneration(players, chunkSet, &counts)
	if len(h.seededChunks) != before {
		t.Fatal("an already-seeded chunk must never re-seed")
	}
}

// TestVanillaSpawnerModeIsolation: only vanilla mode runs chunk-generation
// seeding — the default tachyne sampler never touches the seeded set.
func TestVanillaSpawnerModeIsolation(t *testing.T) {
	for _, vanilla := range []bool{false, true} {
		h := newHub(world.New(1))
		h.vanillaSpawner = vanilla
		h.dayTime.Store(18000)
		pl := testTracked()
		pl.x, pl.y, pl.z = 0.5, 64, 0.5
		players := map[int32]*tracked{1: pl}
		for i := 0; i < 30; i++ {
			h.tick.Store(uint64(i))
			h.naturalSpawn(players)
		}
		if vanilla && len(h.seededChunks) == 0 {
			t.Error("vanilla mode should seed chunks as land loads")
		}
		if !vanilla && len(h.seededChunks) != 0 {
			t.Error("tachyne mode must not run chunk-generation seeding")
		}
	}
}

// TestNearWorldSpawnExclusion: the 24-block no-spawn ring around world spawn.
func TestNearWorldSpawnExclusion(t *testing.T) {
	h := newHub(world.New(1))
	if h.nearWorldSpawn(0, 0, 0) {
		t.Fatal("with no world spawn set there is no exclusion")
	}
	h.hasWorldSpawn = true
	h.worldSpawnX, h.worldSpawnY, h.worldSpawnZ = 100, 64, 100
	if !h.nearWorldSpawn(105, 64, 100) {
		t.Fatal("a point within 24 of world spawn must be excluded")
	}
	if h.nearWorldSpawn(130, 64, 100) {
		t.Fatal("a point beyond 24 (horizontally) of world spawn must be allowed")
	}
	if h.nearWorldSpawn(100, 100, 100) {
		t.Fatal("a point 36 blocks BELOW world spawn must be allowed (3D distance)")
	}
}

// TestVanillaSpawnerFillsCaves: the exact-vanilla per-tick loop (one attempt per
// chunk) still populates caves through the full-column Y roll and respects the
// scaled monster cap — the same guarantees as the default sampler.
func TestVanillaSpawnerFillsCaves(t *testing.T) {
	h := newHub(world.New(1))
	h.vanillaSpawner = true
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(18000) // midnight: caves and surface both eligible
	for i := 0; i < 400; i++ {
		h.tick.Store(uint64(i))
		h.naturalSpawn(players)
	}
	monsters, below := 0, 0
	for _, m := range h.mobs {
		if m.hostile {
			monsters++
			if m.y < 50 {
				below++
			}
		}
	}
	if monsters == 0 {
		t.Fatal("the vanilla per-tick loop must spawn monsters")
	}
	capN := categoryCap[catMonster] * (13 * 13) / spawnChunkArea
	if monsters > capN {
		t.Fatalf("monster count %d exceeded the scaled cap %d", monsters, capN)
	}
	if below == 0 {
		t.Fatal("the full-column Y roll must populate caves")
	}
}
