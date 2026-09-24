package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestItemWorksOutOfAPlacedBlock (bug #33): a poppy lying on a floor that is
// then built over must move out of the block, as vanilla's ItemEntity does
// (moveTowardsClosestSpace), not stay buried in it.
func TestItemWorksOutOfAPlacedBlock(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 182; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	it := h.spawnItem(players, itemByName["poppy"], 1, 0.3, 180, 0.6)
	if it == nil {
		t.Fatal("no item")
	}
	it.x, it.y, it.z, it.vx, it.vy, it.vz = 0.3, 180, 0.6, 0, 0, 0
	h.setBlockAt(players, 0, blockPos{0, 180, 0}, worldgen.BlockBase("oak_planks"))
	for i := 0; i < 60; i++ {
		h.tickItems(players)
	}
	cell := func() uint32 {
		return w.At(int(math.Floor(it.x)), int(math.Floor(it.y+itemHalfHeight)), int(math.Floor(it.z)))
	}
	if worldgen.Collides(cell()) {
		t.Fatalf("the poppy is still inside the planks at %.2f %.2f %.2f", it.x, it.y, it.z)
	}
	// Walled in on every side, it comes out of the top.
	it.x, it.y, it.z, it.vx, it.vy, it.vz = 1.5, 180, 1.5, 0, 0, 0
	for x := 0; x <= 2; x++ {
		for z := 0; z <= 2; z++ {
			w.SetBlock(x, 180, z, worldgen.BlockBase("oak_planks"))
		}
	}
	for i := 0; i < 60; i++ {
		h.tickItems(players)
	}
	if worldgen.Collides(cell()) || it.y < 181 || math.Floor(it.x) != 1 || math.Floor(it.z) != 1 {
		t.Fatalf("boxed in, the poppy should rise onto the top: %.2f %.2f %.2f", it.x, it.y, it.z)
	}
}
