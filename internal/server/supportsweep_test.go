package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An engine-driven block change (not a player's edit) drops what leaned on
// the old block: a torch loses its floor, a crop its farmland.
func TestEngineBlockChangeDropsUnsupported(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1100, 180, 1100
	flatFloor(h.world, x, y, z, 4)
	h.world.SetBlock(x, y, z, worldgen.BlockID("torch"))
	h.world.SetBlock(x+2, y-1, z, worldgen.BlockID("farmland"))
	h.world.SetBlock(x+2, y, z, worldgen.BlockID("wheat"))
	h.world.SetBlock(x, y, z+2, worldgen.Stone)
	h.world.SetBlock(x, y, z+3, withProps(t, worldgen.BlockID("wall_torch"), map[string]string{"facing": "south"}))

	h.setBlockAt(players, 0, blockPos{x, y - 1, z}, worldgen.Air)      // the torch's floor goes
	h.setBlockAt(players, 0, blockPos{x + 2, y - 1, z}, worldgen.Dirt) // the farmland reverts
	h.setBlockAt(players, 0, blockPos{x, y, z + 2}, worldgen.Air)      // the wall torch's wall goes
	if got := h.world.At(x, y, z); got != worldgen.Air {
		t.Fatalf("torch should fall with its floor, got %d", got)
	}
	if got := h.world.At(x+2, y, z); got != worldgen.Air {
		t.Fatalf("wheat should pop when its farmland reverts, got %d", got)
	}
	if got := h.world.At(x, y, z+3); got != worldgen.Air {
		t.Fatalf("wall torch should fall with its wall, got %d", got)
	}
	torches, seeds := 0, 0
	for _, it := range h.items {
		switch it.item {
		case itemByName["torch"]:
			torches += it.count
		case itemByName["wheat_seeds"]:
			seeds += it.count
		}
	}
	if torches != 2 || seeds == 0 {
		t.Fatalf("dropped blocks should leave their loot: torches=%d seeds=%d", torches, seeds)
	}
	if h.supportSweep {
		t.Fatal("the sweep guard must reset")
	}
}
