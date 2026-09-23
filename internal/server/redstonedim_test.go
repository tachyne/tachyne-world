package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone runs in the dimension the block lives in: a lever, dust and lamp
// laid in the Nether light up there, the overworld at the same coordinates
// stays exactly as it was, and a scheduled update at Nether coordinates
// never writes an overworld block (it used to).
func TestRedstoneRunsInTheNether(t *testing.T) {
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
	x, y, z := 40, 200, 40 // a pad in the Nether's empty air, well above its terrain
	nw.ForceLoad(x, z, 1)
	for dx := -1; dx < 8; dx++ {
		for dz := -1; dz < 2; dz++ {
			nw.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			nw.SetBlock(x+dx, y, z+dz, worldgen.Air)
			nw.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	lever := setBoolProp(worldgen.BlockBase("lever")+9, "powered", false) // a wall lever facing north
	nw.SetBlock(x, y, z+1, worldgen.Stone)
	nw.SetBlock(x, y, z, lever)
	for i := 1; i <= 4; i++ {
		nw.SetBlock(x+i, y, z, worldgen.BlockBase("redstone_wire")+1160)
	}
	nw.SetBlock(x+5, y, z, lampOff)
	over := make([]uint32, 7)
	for i := range over {
		over[i] = w.At(x+i, y, z) // the overworld column at the same coordinates, before
	}
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+2.5

	h.inDim(pl.dim, func() { h.toggleLever(players, blockPos{x, y, z}, nw.At(x, y, z)) }) // what evUseRedstone does for a Nether player
	stepTicks(h, players, 12)
	if nw.At(x+5, y, z) != lampOn {
		t.Fatalf("the Nether lamp should light: dust %d %d %d %d, lamp %d",
			wirePower(nw.At(x+1, y, z)), wirePower(nw.At(x+2, y, z)), wirePower(nw.At(x+3, y, z)), wirePower(nw.At(x+4, y, z)), nw.At(x+5, y, z))
	}
	for i := range over {
		if w.At(x+i, y, z) != over[i] {
			t.Fatalf("the overworld column changed at %d: %d → %d", i, over[i], w.At(x+i, y, z))
		}
	}
	// A bare scheduled update at Nether coordinates of a block that only the
	// redstone path handles: nothing in the overworld moves.
	h.scheduleIn(dimNether, blockPos{x + 2, y, z}, 1)
	stepTicks(h, players, 3)
	for i := range over {
		if w.At(x+i, y, z) != over[i] {
			t.Fatalf("a Nether update wrote the overworld at %d", i)
		}
	}
	if h.rsDim != 0 {
		t.Fatalf("the simulation dimension must be restored to the overworld, is %d", h.rsDim)
	}
}
