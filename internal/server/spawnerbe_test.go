package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// spawnerPad is a lit grass field at y 179 with open air above it, loaded,
// and a survival player standing beside the origin.
func spawnerPad(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 3)
	for x := -10; x <= 30; x++ {
		for z := -10; z <= 10; z++ {
			h.world.SetBlock(x, 178, z, worldgen.Stone)
			h.world.SetBlock(x, 179, z, worldgen.GrassBlock)
			for y := 180; y < 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	h.allocEID()
	pl.x, pl.y, pl.z = 3.5, 180, 3.5
	return h, players, pl
}

func countKind(h *hub, etype int) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype == etype && m.dying == 0 {
			n++
		}
	}
	return n
}

// A spawner a player placed and gave a pig spawn egg ticks as BaseSpawner:
// the fresh delay of 20 runs down first, then a round puts pigs in within
// spawnRange of the cage and re-arms it at 200..799; out of a player's range
// it sleeps, and broken it is forgotten.
func TestPlacedSpawnerWithEggSpawns(t *testing.T) {
	h, players, pl := spawnerPad(t)
	cage := blockPos{0, 180, 0}
	h.setBlockAt(players, 0, cage, spawnerBlock)
	pl.p.setHotbarSlot(0, int32(itemByName["pig_spawn_egg"]))
	pl.inv.slots[0] = invStack{item: int32(itemByName["pig_spawn_egg"]), count: 1}
	h.eggSpawner(players, evEggSpawner{eid: pl.p.eid, x: cage.x, y: cage.y, z: cage.z})
	if !h.placedSpawner(0, cage) {
		t.Fatal("the egg did not give the spawner its SpawnData")
	}
	h.updatePlacedSpawners(players)
	if n := countKind(h, entityPig); n != 0 {
		t.Fatalf("%d pigs before the fresh delay of 20 ran out", n)
	}
	h.updatePlacedSpawners(players)
	n := countKind(h, entityPig)
	if n < 1 || n > spawnerCount {
		t.Fatalf("a round spawned %d pigs, want 1..%d", n, spawnerCount)
	}
	for _, m := range h.mobs {
		if m.etype != entityPig {
			continue
		}
		if m.hostile {
			t.Error("a spawner's pig came out hostile")
		}
		if dx, dz := m.x-0.5, m.z-0.5; dx < -4 || dx > 4 || dz < -4 || dz > 4 {
			t.Errorf("a pig at %.1f,%.1f from the cage, past spawnRange 4", dx, dz)
		}
	}
	sp := simPos{blockPos: cage}
	if d := h.spawnerDelays[sp]; d < spawnerMinDelay || d >= spawnerMinDelay+spawnerDelaySpan {
		t.Errorf("re-armed delay %d, want 200..799", d)
	}
	// Nobody near: the delay holds.
	d := h.spawnerDelays[sp]
	pl.x = 30.5
	h.updatePlacedSpawners(players)
	if h.spawnerDelays[sp] != d {
		t.Error("a spawner with no player in range counted its delay down")
	}
	// Broken: the block entity goes with it.
	h.setBlockAt(players, 0, cage, worldgen.Air)
	if h.placedSpawner(0, cage) {
		t.Error("a broken spawner kept its SpawnData")
	}
	if _, ok := h.spawnerDelays[sp]; ok {
		t.Error("a broken spawner kept its delay")
	}
}

// The cap: with maxNearbyEntities of the kind already round the cage a round
// spawns nothing and re-arms.
func TestPlacedSpawnerCap(t *testing.T) {
	h, players, _ := spawnerPad(t)
	cage := blockPos{0, 180, 0}
	h.setBlockAt(players, 0, cage, spawnerBlock)
	h.setSpawnerEntity(simPos{blockPos: cage}, "pig")
	h.spawnerDelays[simPos{blockPos: cage}] = 0
	for i := 0; i < spawnerMobCap; i++ {
		h.spawnMob(players, entityPig, 2.5, 180, float64(i)-2.5)
	}
	h.updatePlacedSpawners(players)
	if n := countKind(h, entityPig); n != spawnerMobCap {
		t.Fatalf("%d pigs, want the cap %d untouched", n, spawnerMobCap)
	}
	if h.spawnerDelays[simPos{blockPos: cage}] < spawnerMinDelay {
		t.Error("a capped round should re-arm the spawner")
	}
}

// Spawn rules with the SPAWNER reason: a zombie cage in full daylight on the
// open grass spawns nothing and stays armed (delay 0); the same cage roofed
// over and dark spawns.
func TestPlacedSpawnerMonsterNeedsDark(t *testing.T) {
	h, players, _ := spawnerPad(t)
	h.dayTime.Store(6000)
	cage := blockPos{0, 180, 0}
	h.setBlockAt(players, 0, cage, spawnerBlock)
	h.setSpawnerEntity(simPos{blockPos: cage}, "zombie")
	h.spawnerDelays[simPos{blockPos: cage}] = 0
	for i := 0; i < 5; i++ {
		h.updatePlacedSpawners(players)
	}
	if n := countKind(h, entityZombie); n != 0 {
		t.Fatalf("%d zombies came out of a spawner in full daylight", n)
	}
	if d := h.spawnerDelays[simPos{blockPos: cage}]; d != 0 {
		t.Errorf("a round that spawned nothing re-armed to %d; vanilla retries", d)
	}
	// Pitch dark: the rules pass.
	if !h.spawnerDarkEnough(0, 0, 0) {
		t.Error("a zombie should spawn in the dark")
	}
	if h.spawnerDarkEnough(0, 0, 1) {
		t.Error("any block light stops an overworld spawner's monsters")
	}
	if !h.spawnerDarkEnough(dimNether, 0, 7) || h.spawnerDarkEnough(dimNether, 0, 8) {
		t.Error("the Nether's monster light test is a fixed 7 with no block-light limit")
	}
}

// /setblock with SpawnData and Delay makes a working cage, and /clone
// carries the cage's mob — a dungeon's own included — to the copy.
func TestSetblockAndCloneSpawner(t *testing.T) {
	h, players, pl := spawnerPad(t)
	nbt := map[string]any{"SpawnData": map[string]any{"entity": map[string]any{"id": "minecraft:cow"}}, "Delay": int64(0)}
	cage := blockPos{0, 180, 0}
	h.applySetBlocks(players, evSetBlocks{eid: pl.p.eid, dim: 0, from: cage, to: cage, state: spawnerBlock,
		mode: "replace", single: true, nbt: nbt})
	sp := simPos{blockPos: cage}
	if h.spawnerEntityAt(sp) != "cow" {
		t.Fatalf("setblock SpawnData: the cage spawns %q", h.spawnerEntityAt(sp))
	}
	if d, ok := h.spawnerDelays[sp]; !ok || d != 0 {
		t.Fatalf("setblock Delay: %d (%v)", d, ok)
	}
	h.updatePlacedSpawners(players)
	if countKind(h, entityCow) == 0 {
		t.Fatal("a /setblock'd spawner with Delay 0 should spawn on its first pass")
	}
	// A plain /setblock spawner over it is a new, empty cage.
	h.applySetBlocks(players, evSetBlocks{eid: pl.p.eid, dim: 0, from: cage, to: cage, state: worldgen.Stone, mode: "replace", single: true})
	h.applySetBlocks(players, evSetBlocks{eid: pl.p.eid, dim: 0, from: cage, to: cage, state: spawnerBlock, mode: "replace", single: true})
	if h.placedSpawner(0, cage) {
		t.Fatal("a spawner set with no data should be an empty cage")
	}
	// Clone a cow cage.
	h.setSpawnerEntity(sp, "cow")
	dst := blockPos{8, 180, 0}
	if msg := h.runClone(players, nil, cloneReq{begin: cage, end: cage, dest: dst}); msg != "" {
		t.Fatalf("clone: %s", msg)
	}
	if h.spawnerEntityAt(simPos{blockPos: dst}) != "cow" || !h.placedSpawner(0, dst) {
		t.Fatalf("the clone spawns %q", h.spawnerEntityAt(simPos{blockPos: dst}))
	}
}

// A dungeon spawner given an egg becomes a block entity of its own: the
// dungeon pass leaves it to updatePlacedSpawners, so it never runs twice.
func TestEggedDungeonSpawnerTicksOnce(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	d, ok := findDungeon(w)
	if !ok {
		t.Skip("no dungeon near origin for this seed")
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(d.X)+2, float64(d.Y), float64(d.Z)
	players := map[int32]*tracked{1: pl}
	w.At(d.X, d.Y, d.Z)
	pos := simPos{blockPos: blockPos{d.X, d.Y, d.Z}}
	if got := h.spawnerEntityAt(pos); got != entityRegistryName(dungeonMobs[d.Mob%3]) {
		t.Fatalf("the dungeon cage reads as %q", got)
	}
	h.setSpawnerEntity(pos, "pig")
	h.updateSpawners(players)
	if len(h.mobs) != 0 {
		t.Fatal("the dungeon pass ran a cage that is now a block entity")
	}
}

// Delays survive a restart, with a spent (0) delay kept apart from none.
func TestSpawnerDelaysPersist(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	s := newContainerStore(path)
	in := map[simPos]int{{blockPos: blockPos{1, 2, 3}}: 0, {dim: dimNether, blockPos: blockPos{-4, 70, 9}}: 431}
	s.recordSpawnerDelays(in)
	s.flush()
	got := newContainerStore(path).loadSpawnerDelays()
	if len(got) != 2 || got[simPos{blockPos: blockPos{1, 2, 3}}] != 0 || got[simPos{dim: dimNether, blockPos: blockPos{-4, 70, 9}}] != 431 {
		t.Fatalf("delays after reload: %v", got)
	}
}
