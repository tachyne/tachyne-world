package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// findDungeon locates a generated dungeon with a chest near the origin.
func findDungeon(w *world.World) (worldgen.Dungeon, bool) {
	for x := -256; x <= 256; x += 32 {
		for z := -256; z <= 256; z += 32 {
			for _, d := range w.Gen().DungeonsNear(x, z, 16) {
				if len(d.Chests) > 0 {
					return d, true
				}
			}
		}
	}
	return worldgen.Dungeon{}, false
}

func TestSpawnerSpawnsWhenPlayerNear(t *testing.T) {
	w := world.New(7)
	h := newTestHub(w)
	d, ok := findDungeon(w)
	if !ok {
		t.Skip("no dungeon near origin for this seed")
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(d.X)+2, float64(d.Y), float64(d.Z)
	players := map[int32]*tracked{1: pl}
	// The dungeon chunk must be materialized so the spawner block reads back.
	w.At(d.X, d.Y, d.Z)
	if w.At(d.X, d.Y, d.Z) != worldgen.BlockBase("spawner") {
		t.Fatalf("expected a spawner block at (%d,%d,%d), got %d", d.X, d.Y, d.Z, w.At(d.X, d.Y, d.Z))
	}
	h.updateSpawners(players)
	if len(h.mobs) == 0 {
		t.Fatal("an active spawner should spawn mobs")
	}
	want := dungeonMobs[d.Mob%3]
	for _, m := range h.mobs {
		if m.mount != 0 {
			continue // a spawner finalizes its spawns too: a spider may carry a skeleton
		}
		if m.etype != want {
			t.Fatalf("spawner mob type %d, want %d", m.etype, want)
		}
		if m.y < float64(d.Y) || m.y > float64(d.Y+1) {
			t.Fatalf("mob should spawn in the room, y %d..%d (BaseSpawner: y ±1, clear of blocks), got %v", d.Y, d.Y+1, m.y)
		}
		if dx, dz := m.x-float64(d.X)-0.5, m.z-float64(d.Z)-0.5; dx < -4 || dx > 4 || dz < -4 || dz > 4 {
			t.Fatalf("mob spawned %.1f,%.1f from the cage, past spawnRange 4", dx, dz)
		}
	}
	// Cooldown: an immediate second pass must not double-spawn.
	before := len(h.mobs)
	h.updateSpawners(players)
	if len(h.mobs) != before {
		t.Fatal("spawner must respect its cooldown")
	}
	// Mined-out spawner goes dead.
	h.spawnerNext = map[simPos]uint64{}
	w.SetBlock(d.X, d.Y, d.Z, worldgen.Air)
	before = len(h.mobs)
	h.updateSpawners(players)
	if len(h.mobs) != before {
		t.Fatal("a mined spawner must not spawn")
	}
}

// BaseSpawner.isNearPlayer asks the spawner's own level: a player in the
// Nether at a dungeon's overworld coordinates does not wake it.
func TestDungeonSpawnerIgnoresOtherDimensions(t *testing.T) {
	w := world.New(7)
	h := newTestHub(w)
	d, ok := findDungeon(w)
	if !ok {
		t.Skip("no dungeon near origin for this seed")
	}
	pl := testTracked()
	pl.dim = dimNether
	pl.x, pl.y, pl.z = float64(d.X)+2, float64(d.Y), float64(d.Z)
	players := map[int32]*tracked{1: pl}
	w.At(d.X, d.Y, d.Z)
	h.updateSpawners(players)
	if len(h.mobs) != 0 {
		t.Fatal("a Nether player woke an overworld dungeon spawner")
	}
}

func TestDungeonChestLoot(t *testing.T) {
	w := world.New(7)
	h := newTestHub(w)
	d, ok := findDungeon(w)
	if !ok {
		t.Skip("no dungeon near origin for this seed")
	}
	at := d.Chests[0]
	c := &chest{}
	h.fillStructureChest(blockPos{at[0], at[1], at[2]}, c)
	items := 0
	for _, st := range c.slots {
		if st.item != 0 {
			items++
		}
	}
	if items < 2 {
		t.Fatalf("dungeon chest should hold loot, has %d stacks", items)
	}
	// Deterministic: same chest fills the same way.
	c2 := &chest{}
	h.fillStructureChest(blockPos{at[0], at[1], at[2]}, c2)
	if c.slots != c2.slots {
		t.Fatal("loot must be deterministic per chest")
	}
	// A non-dungeon position stays empty.
	c3 := &chest{}
	h.fillStructureChest(blockPos{d.X, d.Y + 1, d.Z}, c3) // over the cage: no chest
	for _, st := range c3.slots {
		if st.item != 0 {
			t.Fatal("ordinary chests must not get dungeon loot")
		}
	}
}
