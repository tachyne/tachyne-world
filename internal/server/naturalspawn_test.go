package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSlimeChunkOracle pins isSlimeChunk to java.util.Random ground truth:
// the listed chunks are exactly the slime chunks in [-3,3]² for two seeds
// (generated with a real JVM from the vanilla seedSlimeChunk formula).
func TestSlimeChunkOracle(t *testing.T) {
	slime := map[[3]int64]bool{
		{1, -3, -3}: true, {1, -3, 0}: true, {1, -2, 2}: true, {1, 0, 3}: true, {1, 2, -1}: true,
		{4506419895, -2, -1}: true, {4506419895, -1, 0}: true, {4506419895, 1, -2}: true,
	}
	for _, seed := range []int64{1, 4506419895} {
		for x := int32(-3); x <= 3; x++ {
			for z := int32(-3); z <= 3; z++ {
				want := slime[[3]int64{seed, int64(x), int64(z)}]
				if got := isSlimeChunk(seed, x, z); got != want {
					t.Errorf("seed %d chunk %d,%d: slime=%v want %v", seed, x, z, got, want)
				}
			}
		}
	}
}

// TestSkyDarken: vanilla's SKY_LIGHT_LEVEL curve — clear noon 0, night 11,
// and storms darken the daytime sky (the daytime-storm-spawn mechanic).
func TestSkyDarken(t *testing.T) {
	h := newHub(world.New(1))
	h.dayTime.Store(6000) // noon
	if d := h.skyDarken(); d != 0 {
		t.Fatalf("noon skyDarken = %d, want 0", d)
	}
	h.dayTime.Store(18000) // midnight
	if d := h.skyDarken(); d != 11 {
		t.Fatalf("midnight skyDarken = %d, want 11", d)
	}
	h.dayTime.Store(6000)
	h.rainLevel, h.thunderLevel = 1, 1 // full thunderstorm at noon
	if d := h.skyDarken(); d < 5 {
		t.Fatalf("storm noon skyDarken = %d, want >= 5", d)
	}
}

// TestDarkEnoughToSpawn: caves (sky 0, block 0) always pass; a torch (block
// light) is absolute protection; the noon surface never passes; the noon
// surface UNDER A THUNDERSTORM sometimes does (vanilla storm spawns).
func TestDarkEnoughToSpawn(t *testing.T) {
	h := newHub(world.New(1))
	h.dayTime.Store(6000)
	for i := 0; i < 200; i++ {
		if !h.darkEnoughToSpawn(0, 0) {
			t.Fatal("a pitch-dark cave must always allow monster spawns")
		}
		if h.darkEnoughToSpawn(0, 1) {
			t.Fatal("any block light must block monster spawns (overworld limit 0)")
		}
		if h.darkEnoughToSpawn(15, 0) {
			t.Fatal("the noon surface must never spawn monsters")
		}
	}
	// midnight surface: passes sometimes (sky gate ≈17/32 × brightness roll)
	h.dayTime.Store(18000)
	hits := 0
	for i := 0; i < 2000; i++ {
		if h.darkEnoughToSpawn(15, 0) {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("the midnight surface must allow some monster spawns")
	}
	// noon under a full thunderstorm: the capped sky term lets some through
	h.dayTime.Store(6000)
	h.rainLevel, h.thunderLevel = 1, 1
	h.raining, h.thundering = true, true
	hits = 0
	for i := 0; i < 2000; i++ {
		if h.darkEnoughToSpawn(15, 0) {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("a daytime thunderstorm must allow some surface monster spawns")
	}
}

// TestSpawnCategoriesAndDespawn: category classification and the vanilla
// despawn distances (fish at 64, monsters/ambient at 128, creatures never).
func TestSpawnCategoriesAndDespawn(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	players := map[int32]*tracked{1: pl}

	cow := h.spawnMob(players, entityCow, 40, 64, 0.5)
	bat := h.spawnMob(players, entityBat, 40, 64, 0.5)
	fish := h.spawnMob(players, entityCod, 40, 64, 0.5)
	zom := h.spawnHostileY(players, entityZombie, 40, 64, 0.5)
	if mobSpawnCategory(cow) != catCreature || mobSpawnCategory(bat) != catAmbient ||
		mobSpawnCategory(fish) != catWaterAmbient || mobSpawnCategory(zom) != catMonster {
		t.Fatal("category classification wrong")
	}

	// 40 blocks out: nothing hard-despawns (fish limit is 64, monster 128)
	h.despawnSweep(players)
	if _, ok := h.mobs[fish.eid]; !ok {
		t.Fatal("a fish at 40 blocks must stay")
	}
	// 70 blocks: past the water-ambient 64 → the fish goes, the zombie stays
	for _, m := range []*mob{cow, bat, fish, zom} {
		m.x = 70
	}
	h.despawnSweep(players)
	if _, alive := h.mobs[fish.eid]; alive {
		t.Fatal("water-ambient mobs hard-despawn beyond 64 blocks")
	}
	if _, alive := h.mobs[zom.eid]; !alive {
		t.Fatal("monsters stay inside 128 blocks")
	}
	// 200 blocks: monster and bat go, the creature is persistent forever
	for _, m := range []*mob{cow, bat, zom} {
		m.x = 200
	}
	h.despawnSweep(players)
	if _, alive := h.mobs[zom.eid]; alive {
		t.Fatal("monsters hard-despawn beyond 128 blocks")
	}
	if _, alive := h.mobs[bat.eid]; alive {
		t.Fatal("ambient mobs hard-despawn beyond 128 blocks")
	}
	if _, alive := h.mobs[cow.eid]; !alive {
		t.Fatal("creatures are persistent and never despawn")
	}
}

// TestSpawnPools: biome routing (husk desert, stray snow, drowned oceans)
// and sane vanilla pack ranges.
func TestSpawnPools(t *testing.T) {
	h := newHub(world.New(1))
	has := func(pool []spawnerEntry, etype int) bool {
		for _, e := range pool {
			if e.etype == etype {
				return true
			}
		}
		return false
	}
	if !has(h.spawnPool(catMonster, 0, 64, 0), entityZombie) && !has(h.spawnPool(catMonster, 0, 64, 0), entityDrowned) {
		t.Fatal("the monster pool must have a zombie-family entry")
	}
	if !has(monsterPoolDesert, entityHusk) || has(monsterPoolDesert, entityStray) {
		t.Fatal("desert pool: husks yes, strays no")
	}
	if !has(monsterPoolSnowy, entityStray) {
		t.Fatal("snowy pool must have strays")
	}
	if !has(monsterPoolOcean, entityDrowned) || has(monsterPoolOcean, entityZombie) {
		t.Fatal("ocean pool: drowned replace zombies (vanilla)")
	}
	for _, pool := range [][]spawnerEntry{monsterPoolDefault, creaturePoolDefault, ambientPool} {
		for _, e := range pool {
			if e.min < 1 || e.max < e.min {
				t.Fatalf("bad pack range for etype %d: %d..%d", e.etype, e.min, e.max)
			}
		}
	}
	// glow squid pool only below the cave-water depth
	if p := h.spawnPool(catWaterCreature, 0, 20, 0); !has(p, entityGlowSquid) {
		t.Fatal("deep water creature pool must be glow squid")
	}
}

// TestSpawnPositionRules: land mobs need solid ground and clear body space;
// water mobs need water.
func TestSpawnPositionRules(t *testing.T) {
	h := newHub(world.New(1))
	h.world.SetBlock(10, 99, 10, worldgen.Stone)
	h.world.SetBlock(10, 100, 10, worldgen.Air)
	h.world.SetBlock(10, 101, 10, worldgen.Air)
	if !h.spawnPositionOK(catMonster, 10, 100, 10) {
		t.Fatal("solid ground + two clear cells must be spawnable")
	}
	h.world.SetBlock(10, 101, 10, worldgen.Stone) // block at head height
	if h.spawnPositionOK(catMonster, 10, 100, 10) {
		t.Fatal("a mob-height obstruction must reject the position")
	}
	if h.spawnPositionOK(catWaterCreature, 10, 100, 10) {
		t.Fatal("water categories need water")
	}
}

// TestCaveMobsStayUnderground: the live bug from the first cluster deploy —
// mob physics re-seated every walker at the COLUMN SURFACE each tick and
// sprang fliers toward surface+hover, so cave-spawned zombies and bats
// teleported up into daylight. Seating must be relative to the mob's own
// height: a mob in a sealed cavity stays on its cave floor.
func TestCaveMobsStayUnderground(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}
	// Carve a sealed 5×3×5 cavity deep underground with a stone floor/ceiling.
	for x := 8; x <= 12; x++ {
		for z := 8; z <= 12; z++ {
			h.world.SetBlock(x, 9, z, worldgen.Stone) // floor
			for y := 10; y <= 12; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			h.world.SetBlock(x, 13, z, worldgen.Stone) // ceiling
		}
	}
	zom := h.spawnHostileY(players, entityZombie, 10.5, 10, 10.5)
	bat := h.spawnMob(players, entityBat, 10.5, 11, 10.5)
	h.applySpecies(players, bat)
	for i := 0; i < 200; i++ { // 10 seconds of mob updates
		h.updateMobs(players)
	}
	if zom.y > 13 {
		t.Fatalf("cave zombie was hoisted to the surface: y=%v", zom.y)
	}
	if bat.y > 13 {
		t.Fatalf("cave bat flew up through the rock: y=%v", bat.y)
	}
	if zom.y < 9 || bat.y < 9 {
		t.Fatalf("cave mobs fell through the floor: zombie=%v bat=%v", zom.y, bat.y)
	}
}

// TestCaveZombieDoesNotBurn: Wesley's second live catch — the daylight burn
// checked sky exposure from the COLUMN SURFACE, so a cave zombie ignited at
// noon through thirty blocks of stone. Exposure now scans from the mob's own
// height; a surface control zombie must still catch fire.
func TestCaveZombieDoesNotBurn(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false // isolate the two hand-placed zombies
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(6000) // noon

	// Sealed cavity deep underground.
	for x := 8; x <= 12; x++ {
		for z := 8; z <= 12; z++ {
			h.world.SetBlock(x, 9, z, worldgen.Stone)
			for y := 10; y <= 12; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			h.world.SetBlock(x, 13, z, worldgen.Stone)
		}
	}
	cave := h.spawnHostileY(players, entityZombie, 10.5, 10, 10.5)
	cave.burnDelay = 0
	// Control: a zombie on a dry, open-sky pillar (the generated column near
	// spawn is ocean — a submerged zombie is correctly doused, not a control).
	h.world.SetBlock(30, 99, 30, worldgen.Stone)
	h.world.SetBlock(30, 100, 30, worldgen.Air)
	h.world.SetBlock(30, 101, 30, worldgen.Air)
	open := h.spawnHostileY(players, entityZombie, 30.5, 100, 30.5)
	open.burnDelay = 0

	for i := 0; i < 3; i++ {
		h.updateHostiles(players)
		h.mobEnvironment(players)
	}
	if cave.burning {
		t.Fatal("a cave zombie must not burn at noon (rock blocks the sky)")
	}
	if !open.burning {
		t.Fatal("the open-sky control zombie must catch fire at noon")
	}
}

// TestSurfaceSeatingUnchanged: the height-relative seating must not change
// surface behavior — digging under a mob still drops it, placing under it
// still lifts it.
func TestSurfaceSeatingUnchanged(t *testing.T) {
	w := world.New(1)
	surf := w.MobFeet(20, 20)
	if got := w.MobFeetFrom(20, 20, surf); got != surf {
		t.Fatalf("surface mob re-seats at %d, want %d", got, surf)
	}
	w.SetBlock(20, surf, 20, worldgen.Stone) // block placed at its feet
	if got := w.MobFeetFrom(20, 20, surf); got != surf+1 {
		t.Fatalf("placed block must lift the mob: %d, want %d", got, surf+1)
	}
	w.SetBlock(20, surf, 20, worldgen.Air)
	w.SetBlock(20, surf-1, 20, worldgen.Air) // dig out the floor
	if got := w.MobFeetFrom(20, 20, surf); got >= surf {
		t.Fatalf("digging the floor must drop the mob: %d (was %d)", got, surf)
	}
}

// TestNaturalSpawnFillsCaves: with a player parked at midnight, the spawner
// must produce underground monsters within a simulated night — the
// cave-population mechanic the old surface-only spawner lacked entirely —
// and never exceed the scaled category cap.
func TestNaturalSpawnFillsCaves(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}
	h.dayTime.Store(18000)      // midnight: surface and caves both eligible
	for i := 0; i < 1200; i++ { // one simulated minute
		h.tick.Store(uint64(i))
		h.naturalSpawn(players)
	}
	monsters, below := 0, 0
	for _, m := range h.mobs {
		if m.hostile {
			monsters++
			if m.y < 50 { // well under the seed-1 surface near spawn — a cave spawn
				below++
			}
		}
	}
	if monsters == 0 {
		t.Fatal("a night of attempts must spawn monsters")
	}
	capN := categoryCap[catMonster] * (13 * 13) / spawnChunkArea // radius-6 window
	if monsters > capN {
		t.Fatalf("monster count %d exceeded the scaled cap %d", monsters, capN)
	}
	if below == 0 {
		t.Fatal("the full-column Y roll must populate caves, not just the surface")
	}
}

// TestResolvedItemConstants: item ids referenced by name must resolve — the
// hardcoded predecessors survived the 1.21.11 id migration as the wrong
// items (bow 841 became the goggle-icon ghast harness in every skeleton's
// hand; carved pumpkin 345 became spruce_fence and the enderman disguise
// silently stopped working).
func TestResolvedItemConstants(t *testing.T) {
	if itemBow == 0 || itemBow != int32(itemByName["bow"]) {
		t.Fatalf("itemBow = %d, want itemByName[bow] = %d", itemBow, itemByName["bow"])
	}
	if itemCarvedPumpkin == 0 || itemCarvedPumpkin != int32(itemByName["carved_pumpkin"]) {
		t.Fatalf("itemCarvedPumpkin = %d, want %d", itemCarvedPumpkin, itemByName["carved_pumpkin"])
	}
}
