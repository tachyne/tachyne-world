package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A cave vine growing down leaves a body with the berries its head had —
// not berries on every segment.
func TestCaveVineBodyKeepsTheHeadsBerries(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	cv := growingPlants[3]
	for _, berries := range []bool{false, true} {
		h.world.SetBlock(0, 201, 0, worldgen.Stone)
		h.world.SetBlock(0, 200, 0, cv.headAt(3, berries))
		h.world.SetBlock(0, 199, 0, worldgen.Air)
		for i := 0; i < 2000 && h.world.At(0, 199, 0) == worldgen.Air; i++ {
			h.tickGrowingPlant(players, 0, 0, 200, 0, h.world.At(0, 200, 0))
		}
		if h.world.At(0, 199, 0) == worldgen.Air {
			t.Fatal("the vine never grew")
		}
		if got := boolProp(h.world.At(0, 200, 0), "berries"); got != berries {
			t.Errorf("head berries=%v left a body with berries=%v", berries, got)
		}
	}
}
