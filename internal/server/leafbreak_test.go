package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A player felling the whole trunk (the onBlock path a dig takes) must send
// the distance recompute through the canopy: every leaf ends at 7 and the
// random tick then rots it.
func TestFelledTrunkRotsCanopy(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 700, 200, 700
	h.world.ForceLoad(x, z, 1)
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	for i := 1; i <= 3; i++ {
		leafAt(h.world, x+i, y, z, i)
	}
	h.world.SetBlock(x, y, z, worldgen.Air)
	h.onBlock(players, evBlock{x: x, y: y, z: z, dim: 0, state: worldgen.Air, by: 1, broken: worldgen.OakLog})
	runTicks(h, players, 1, 200)
	for i := 1; i <= 3; i++ {
		if _, d, _, _ := leafInfo(h.world.At(x+i, y, z)); d != 7 {
			t.Fatalf("leaf %d distance %d after the trunk fell, want 7", i, d)
		}
	}
	for i := 1; i <= 3; i++ {
		h.tickLeaf(players, 0, x+i, y, z, h.world.At(x+i, y, z))
	}
	for i := 1; i <= 3; i++ {
		if isAnyLeaf(h.world.At(x+i, y, z)) {
			t.Fatalf("leaf %d did not rot after the trunk fell", i)
		}
	}
}
