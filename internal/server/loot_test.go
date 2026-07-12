package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestRollDropsGrassSeedsRate(t *testing.T) {
	h := newHub(world.New(1))
	const n = 100000
	seeds := 0
	for i := 0; i < n; i++ {
		for _, d := range h.rollDrops(worldgen.ShortGrass) {
			if d.item == itemWheatSeeds {
				seeds++
			}
		}
	}
	rate := float64(seeds) / n
	if rate < 0.11 || rate > 0.14 { // vanilla 1/8 = 0.125
		t.Errorf("grass seed drop rate %.3f, want ~0.125", rate)
	}
}

func TestRollDropsFixed(t *testing.T) {
	h := newHub(world.New(1))
	if d := h.rollDrops(worldgen.Stone); len(d) != 1 || d[0].item != itemCobble {
		t.Errorf("stone should drop cobblestone, got %v", d)
	}
	if d := h.rollDrops(worldgen.GrassBlock); len(d) != 1 || d[0].item != itemDirt {
		t.Errorf("grass block should drop dirt, got %v", d)
	}
	// Gravel always yields exactly one item — flint or gravel.
	for i := 0; i < 50; i++ {
		d := h.rollDrops(worldgen.Gravel)
		if len(d) != 1 || (d[0].item != itemGravel && d[0].item != itemFlint) {
			t.Fatalf("gravel drop unexpected: %v", d)
		}
	}
}

func TestItemSpawnAndDespawn(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	it := h.spawnItem(players, itemWheatSeeds, 1, 8.5, 0, 8.5)
	if it == nil || h.items[it.eid] == nil {
		t.Fatal("item should be registered")
	}
	if h.spawnItem(players, 0, 1, 0, 0, 0) != nil {
		t.Error("empty item id should not spawn")
	}
	h.updateItems(players) // not yet expired
	if h.items[it.eid] == nil {
		t.Fatal("item despawned too early")
	}
	h.tick.Store(uint64(itemDespawnTicks) + 1)
	h.updateItems(players)
	if h.items[it.eid] != nil {
		t.Error("item should have despawned after its lifetime")
	}
}
