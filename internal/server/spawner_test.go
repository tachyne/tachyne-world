package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// findDungeon locates a generated dungeon near the origin.
func findDungeon(w *world.World) (worldgen.Dungeon, bool) {
	for x := -500; x <= 500; x += 48 {
		for z := -500; z <= 500; z += 48 {
			if d := w.Gen().DungeonIn(x, z); d.Exists {
				return d, true
			}
		}
	}
	return worldgen.Dungeon{}, false
}

func TestSpawnerSpawnsWhenPlayerNear(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
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
		if m.etype != want {
			t.Fatalf("spawner mob type %d, want %d", m.etype, want)
		}
		if m.y != float64(d.Y) {
			t.Fatalf("mob should spawn on the dungeon floor y=%d, got %v", d.Y, m.y)
		}
	}
	// Cooldown: an immediate second pass must not double-spawn.
	before := len(h.mobs)
	h.updateSpawners(players)
	if len(h.mobs) != before {
		t.Fatal("spawner must respect its cooldown")
	}
	// Mined-out spawner goes dead.
	h.spawnerNext = map[blockPos]uint64{}
	w.SetBlock(d.X, d.Y, d.Z, worldgen.Air)
	before = len(h.mobs)
	h.updateSpawners(players)
	if len(h.mobs) != before {
		t.Fatal("a mined spawner must not spawn")
	}
}

func TestDungeonChestLoot(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	d, ok := findDungeon(w)
	if !ok {
		t.Skip("no dungeon near origin for this seed")
	}
	c := &chest{}
	h.dungeonLoot(blockPos{d.ChestX, d.Y, d.ChestZ}, c)
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
	h.dungeonLoot(blockPos{d.ChestX, d.Y, d.ChestZ}, c2)
	if c.slots != c2.slots {
		t.Fatal("loot must be deterministic per chest")
	}
	// A non-dungeon position stays empty.
	c3 := &chest{}
	h.dungeonLoot(blockPos{d.ChestX + 1, d.Y, d.ChestZ}, c3)
	for _, st := range c3.slots {
		if st.item != 0 {
			t.Fatal("ordinary chests must not get dungeon loot")
		}
	}
}
