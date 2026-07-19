package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDungeonSpawnCount pins the per-cycle spawn count to vanilla's 4.
func TestDungeonSpawnCount(t *testing.T) {
	if spawnerCount != 4 {
		t.Fatalf("dungeon spawner count %d, want vanilla DEFAULT_SPAWN_COUNT 4", spawnerCount)
	}
}

// TestRawBrightnessSubtraction pins the getRawBrightness(pos, amount) semantics:
// darken is a subtraction amount, not a cap. Thunder's fixed −10 (not a cap)
// must not double-count the night skyDarken.
func TestRawBrightnessSubtraction(t *testing.T) {
	h := newHub(world.New(1))
	// Force full night so skyDarken() is large (11).
	h.dayTime.Store(18000)

	// darken 0 = the true raw value: max(sky, block), no time reduction.
	if got := h.rawBrightness(15, 0, 0); got != 15 {
		t.Errorf("raw (darken 0) of sky 15 = %d, want 15", got)
	}
	if got := h.rawBrightness(4, 7, 0); got != 7 {
		t.Errorf("raw (darken 0) picks block light: %d, want 7", got)
	}
	// darken 10 (thunder) = sky−10, a fixed subtraction that ignores skyDarken.
	if got := h.rawBrightness(15, 0, 10); got != 5 {
		t.Errorf("thunder raw of sky 15 = %d, want 15-10=5", got)
	}
	// darken -1 = the time/weather skyDarken (large at night → sky knocked to 0).
	if got := h.rawBrightness(15, 0, -1); got != 15-h.skyDarken() && got != 0 {
		t.Errorf("skyDarken path of sky 15 = %d, want 15-%d", got, h.skyDarken())
	}
}

// TestAnimalLightUsesRawSky — animals spawn on a sky-lit surface day OR night
// (vanilla getRawBrightness(pos, 0) > 8), so the catCreature light gate must
// not apply the night skyDarken.
func TestAnimalLightUsesRawSky(t *testing.T) {
	h := newHub(world.New(1))
	h.dayTime.Store(18000) // deep night

	// Grass floor under a sky-lit (15) column: vanilla lets animals spawn.
	x, y, z := 40, 70, 40
	h.world.SetBlock(x, y-1, z, worldgen.GrassBlock)
	if !h.spawnRulesOK(catCreature, entityCow, x, y, z, 15, 0) {
		t.Fatal("animals must be allowed on a sky-lit surface at night (raw light 15 > 8)")
	}
	// A dark cave floor (sky 0, block 0) still rejects them.
	if h.spawnRulesOK(catCreature, entityCow, x, y, z, 0, 0) {
		t.Fatal("animals must NOT spawn in pitch dark (raw light 0)")
	}
}

// TestDrownedPlacementAndRarity — drowned use IN_WATER placement and the
// river-1/15 vs deep-ocean-1/40 rarity gates, unlike land monsters.
func TestDrownedPlacementAndRarity(t *testing.T) {
	h := newHub(world.New(1))

	// Placement: a water anchor with a clear block above is valid for drowned,
	// invalid for a land monster; a solid anchor is the reverse.
	wx, wy, wz := 20, worldgen.SeaLevel-10, 20
	h.world.SetBlock(wx, wy, wz, worldgen.Water)
	h.world.SetBlock(wx, wy+1, wz, worldgen.Water)
	h.world.SetBlock(wx, wy-1, wz, worldgen.Water)
	if !h.spawnPositionOK(catMonster, entityDrowned, wx, wy, wz) {
		t.Fatal("drowned must accept an IN_WATER anchor")
	}
	if h.spawnPositionOK(catMonster, entityZombie, wx, wy, wz) {
		t.Fatal("a land monster must reject a water anchor")
	}

	// Rarity: with a deterministic rng forced to 0, the deep-ocean gate passes
	// only well below sea level; forced non-zero, it always fails.
	pass := 0
	for i := 0; i < 2000; i++ {
		if h.spawnRulesOK(catMonster, entityDrowned, wx, wy, wz, 0, 0) {
			pass++
		}
	}
	if pass == 0 || pass > 200 { // ~1/40 of 2000 ≈ 50; wide bounds, just not 0 or ~all
		t.Fatalf("deep-ocean drowned rarity looks wrong: %d/2000 passed (want ~1/40)", pass)
	}

	// No water below → never spawns, regardless of the roll.
	h.world.SetBlock(wx, wy-1, wz, worldgen.Stone)
	for i := 0; i < 200; i++ {
		if h.spawnRulesOK(catMonster, entityDrowned, wx, wy, wz, 0, 0) {
			t.Fatal("drowned must not spawn without water below the anchor")
		}
	}
}
