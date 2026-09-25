package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestEndPortalTakesMobsAndItems: EndPortalBlock.entityInside sends any
// entity that can use a portal, not only players — into the End onto the
// obsidian pad at (100,50,0), and from the End to the world spawn.
func TestEndPortalTakesMobsAndItems(t *testing.T) {
	h := newHub(world.New(1))
	h.end = world.New(3)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 10, 180, 10
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.EndPortalBlock)
	cow := h.spawnMob(players, entityCow, float64(x)+0.5, float64(y), float64(z)+0.5)
	it := h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y), float64(z)+0.5, 0, 0, 0)
	it.x, it.y, it.z = float64(x)+0.5, float64(y), float64(z)+0.5
	h.updateEndPortalEntities(players)
	if cow.dim != dimEnd || cow.x != 100.5 || cow.y != 50 {
		t.Fatalf("the cow should arrive in the End at (100.5,50): dim %d at %.1f %.1f", cow.dim, cow.x, cow.y)
	}
	if it.dim != dimEnd {
		t.Fatalf("the stick should go through too: dim %d", it.dim)
	}
	if got := h.end.At(100, 48, 0); got != obsidianBlock {
		t.Fatalf("the arrival pad should be obsidian, got state %d", got)
	}
	// And back out of the End to the world spawn.
	h.end.SetBlock(100, 50, 0, worldgen.EndPortalBlock)
	cow.portalCool = 0
	h.updateEndPortalEntities(players)
	if cow.dim != dimOverworld {
		t.Fatalf("a cow in the End's portal goes to the overworld spawn: dim %d", cow.dim)
	}
}
