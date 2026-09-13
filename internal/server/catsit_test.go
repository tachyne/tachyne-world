package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCatSitsOnChest: a tamed, idle cat near a closed chest walks onto it
// and sits; a player opening the chest gets it up.
func TestCatSitsOnChest(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	w.SetBlock(3, 180, 0, chestStateMin)
	pl.x, pl.y, pl.z = 0, 180, -4
	c := h.spawnMob(players, entityCat, 0.5, 180, 0.5)
	c.tamed, c.owner = true, pl.p.eid
	c.sitNext = 0
	if !h.catSitStep(players, c) || c.sitBlock != (blockPos{3, 180, 0}) {
		t.Fatalf("the cat should head for the chest: %+v", c.sitBlock)
	}
	for i := 0; i < 200 && !c.sitPose; i++ {
		c.x += c.vx
		c.z += c.vz
		if c.x > 3 {
			c.y = 181 // up onto the chest
		}
		h.catSitStep(players, c)
	}
	if !c.sitPose {
		t.Fatalf("the cat should be sitting on the chest: at %.2f,%.2f try %d", c.x, c.z, c.sitTry)
	}
	pl.winKind, pl.winPos = winChest, simPos{dim: 0, blockPos: blockPos{3, 180, 0}}
	if h.catSitStep(players, c) || c.sitPose || c.sitBlock != (blockPos{}) {
		t.Fatal("an opened chest gets the cat up")
	}
	// A bed's head does not qualify, its foot does; a cold furnace does not.
	if h.catSitTarget(0, blockPos{3, 180, 0}) {
		t.Fatal("an open chest is not a seat")
	}
	pl.winKind = winPlayer
	w.SetBlock(3, 180, 0, furnaceStateMin)
	info, _ := worldgen.InfoForState(furnaceStateMin)
	if worldgen.GetProperty(info, furnaceStateMin, "lit") == "true" {
		t.Skip("furnace base is lit")
	}
	if h.catSitTarget(0, blockPos{3, 180, 0}) {
		t.Fatal("a cold furnace is not a seat")
	}
	w.SetBlock(3, 180, 0, worldgen.SetProperty(info, furnaceStateMin, "lit", "true"))
	if !h.catSitTarget(0, blockPos{3, 180, 0}) {
		t.Fatal("a lit furnace is")
	}
}
