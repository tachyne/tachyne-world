package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A piston in the Nether moves Nether blocks and leaves the overworld alone.
// The whole piston path — the structure resolver, the moving cells, the
// blocks they land as, the drops and the schedule — read and wrote dimension
// zero regardless of where the piston stood, so a Nether piston rewrote the
// overworld at the same coordinates and animated nothing where it actually
// was.
func TestPistonRunsInTheNether(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	nw, err := world.NewNether(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.nether = nw
	pl := testTracked()
	pl.dim = dimNether
	players := map[int32]*tracked{1: pl}

	x, y, z := 60, 200, 60 // a pad in the Nether's empty air
	for dx := -1; dx < 8; dx++ {
		for dz := -1; dz < 2; dz++ {
			nw.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			nw.SetBlock(x+dx, y, z+dz, worldgen.Air)
			nw.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	// A sticky piston facing east with a stone in front of it, and a lever on
	// the block behind to drive it.
	pist := worldgen.BlockBase("sticky_piston")
	info, _ := worldgen.InfoForState(pist)
	pist = setBoolProp(worldgen.SetProperty(info, pist, "facing", "east"), "extended", false)
	nw.SetBlock(x, y, z, pist)
	nw.SetBlock(x+1, y, z, worldgen.Stone)
	nw.SetBlock(x-1, y, z, worldgen.BlockBase("redstone_block"))

	before := make([]uint32, 6)
	for i := range before {
		before[i] = w.At(x+i, y, z) // the overworld column at the same coordinates
	}

	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+2.5
	h.scheduleIn(dimNether, blockPos{x, y, z}, 1)
	stepTicks(h, players, 20)

	if got := nw.At(x+2, y, z); got != worldgen.Stone {
		t.Errorf("the Nether piston should have pushed the stone to x+2, got %d", got)
	}
	if !boolProp(nw.At(x, y, z), "extended") {
		t.Error("the Nether piston should be extended")
	}
	for i := range before {
		if got := w.At(x+i, y, z); got != before[i] {
			t.Errorf("the overworld at x+%d changed from %d to %d — the Nether piston wrote through it",
				i, before[i], got)
		}
	}
	// And its moving-cell records are filed under the Nether, not the overworld.
	for key := range h.movingBlocks {
		if key.dim != dimNether {
			t.Errorf("a moving cell was recorded in dimension %d", key.dim)
		}
	}
}
