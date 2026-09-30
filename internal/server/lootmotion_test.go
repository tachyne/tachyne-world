package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A dying mob's loot pops out of it (Entity.spawnAtLocation): the drop
// appears at the mob's feet with the ItemEntity constructor's 0.2 hop and a
// push of up to 0.1 sideways, and falls from there — it used to appear on
// the floor under a mob that died in the air, with no motion at all.
func TestMobLootPopsOutAndFalls(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	cow := h.spawnMob(players, entityCow, 0.5, 190, 0.5)
	if cow == nil {
		t.Fatal("no cow spawned")
	}
	h.despawnMob(players, cow) // the death: loot and all
	beef := int32(itemByName["beef"])
	var it *itemEntity
	for _, x := range h.items {
		if x.item == beef {
			it = x
		}
	}
	if it == nil {
		t.Fatal("a dead cow drops beef")
	}
	if it.y != 190 || it.x != 0.5 || it.z != 0.5 {
		t.Fatalf("the beef should appear at the cow's feet (0.5, 190, 0.5), got (%.3f, %.3f, %.3f)", it.x, it.y, it.z)
	}
	if it.vy != 0.2 || math.Abs(it.vx) > 0.1 || math.Abs(it.vz) > 0.1 {
		t.Fatalf("the beef should leave with the 0.2 hop and a small push, got (%.3f, %.3f, %.3f)", it.vx, it.vy, it.vz)
	}
	for i := 0; i < 200; i++ {
		h.tickItems(players)
	}
	if it.y != 180 {
		t.Fatalf("the beef should fall to the floor at y=180, it is at %.3f", it.y)
	}
	if d := math.Hypot(it.x-0.5, it.z-0.5); d > 3 {
		t.Fatalf("the beef drifted %.2f blocks from where the cow died", d)
	}
}
