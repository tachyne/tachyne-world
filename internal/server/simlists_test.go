package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The simulation queue runs as vanilla's two tick lists: a cell holding a
// fluid is on the fluid list, anything else on the block list, and each
// has its own cap. With the block list's budget spent, the block updates
// past it wait — in order, first in line next tick — while the fluid list
// still runs in full.
func TestSimQueueFluidAndBlockListsCapSeparately(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	players := map[int32]*tracked{}
	x, y, z := 2000, 180, 2000
	w.ForceLoad(x, z, 2)
	for dx := -1; dx <= 5; dx++ { // a stone slab to hold everything
		for dz := -1; dz <= 7; dz++ {
			for dy := -1; dy <= 1; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	var blocks, fluids []blockPos
	for i := 0; i < 5; i++ {
		blocks = append(blocks, blockPos{x + i, y, z})
	}
	for i := 0; i < 3; i++ {
		p := blockPos{x + i*2, y, z + 5} // a sealed source in the stone: nothing to flow to
		w.SetBlock(p.x, p.y, p.z, worldgen.WaterBase)
		fluids = append(fluids, p)
	}
	age := h.tick.Load() + 1
	for i := range blocks {
		h.pending[age] = append(h.pending[age], simPos{blockPos: blocks[i]})
		if i < len(fluids) {
			h.pending[age] = append(h.pending[age], simPos{blockPos: fluids[i]})
		}
	}
	h.tick.Store(age)
	h.runSimUpdates(players, age, 2) // room for two block updates
	next := h.pending[age+1]
	if len(next) < 3 {
		t.Fatalf("the block updates past the cap should wait for the next tick: %v", next)
	}
	for i, want := range blocks[2:] {
		if next[i].blockPos != want {
			t.Errorf("waiting update %d is %v, want %v (in order, at the front)", i, next[i].blockPos, want)
		}
	}
	for _, sp := range next {
		for _, f := range fluids {
			if sp.blockPos == f {
				t.Errorf("fluid tick %v was held back by the block list's cap", f)
			}
		}
	}
}
