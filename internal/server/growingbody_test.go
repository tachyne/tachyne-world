package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// GrowingPlantBodyBlock.updateShape: cutting the top off a kelp stalk leaves
// the body below it as the new head, which grows on; a weeping vine broken
// part way down does the same, downward. A body with more of its plant
// ahead stays body.
func TestCutGrowingPlantGrowsANewHead(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 40, 180, 40
	kelp := growingPlants[0]
	w.SetBlock(x, y-1, z, worldgen.Stone)
	for i := 0; i < 4; i++ {
		w.SetBlock(x, y+i, z, kelp.body)
	}
	w.SetBlock(x, y+4, z, kelp.headAt(3, false))
	w.SetBlock(x, y+5, z, worldgen.WaterBase)

	h.setBlockAt(players, 0, blockPos{x, y + 3, z}, worldgen.WaterBase) // the top length is cut
	if g, ok := growingPlantOf(w.At(x, y+2, z)); !ok || g.headLo != kelp.headLo {
		t.Fatalf("the body under the cut is %d, want a kelp head", w.At(x, y+2, z))
	}
	if w.At(x, y+1, z) != kelp.body {
		t.Errorf("a body with kelp above it changed: %d", w.At(x, y+1, z))
	}

	var weeping growingPlant
	for _, g := range growingPlants {
		if g.dy < 0 && g.berryStride == 1 {
			weeping = g
		}
	}
	const vx = x + 4
	w.SetBlock(vx, y+5, z, worldgen.Stone)
	for i := 1; i <= 3; i++ {
		w.SetBlock(vx, y+5-i, z, weeping.body)
	}
	w.SetBlock(vx, y+1, z, weeping.headAt(0, false))
	h.setBlockAt(players, 0, blockPos{vx, y + 2, z}, worldgen.Air) // snapped part way down
	if g, ok := growingPlantOf(w.At(vx, y+3, z)); !ok || g.headLo != weeping.headLo {
		t.Fatalf("the vine above the break is %d, want a weeping-vines head", w.At(vx, y+3, z))
	}
}
