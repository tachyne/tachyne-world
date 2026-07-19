package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestComparatorThroughBlock: a comparator reads a container's fullness even
// when a solid block sits between them.
func TestComparatorThroughBlock(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	info, _ := worldgen.InfoForState(comparatorMin)
	comp := worldgen.SetProperty(info, comparatorMin, "facing", "north")
	comp = worldgen.SetProperty(info, comp, "mode", "compare")

	onHub(t, h, func() {
		pos := blockPos{0, 70, 0}
		dx, dz := facingDelta("north")
		back := blockPos{pos.x + dx, pos.y, pos.z + dz}   // solid block behind
		far := blockPos{back.x + dx, back.y, back.z + dz} // container beyond it
		w.SetBlock(pos.x, pos.y, pos.z, comp)
		w.SetBlock(back.x, back.y, back.z, worldgen.Stone) // a solid full block
		w.SetBlock(far.x, far.y, far.z, worldgen.BlockBase("chest"))

		// Empty chest → no signal.
		h.chests[far] = &chest{}
		h.updateComparator(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if h.compOut[pos] != 0 {
			t.Fatalf("empty chest through a block gave signal %d", h.compOut[pos])
		}

		// Fill the chest → the comparator reads it through the stone, after its
		// vanilla 2-tick delay (first call schedules the flip, the second applies).
		h.chests[far].slots[0] = invStack{item: 1, count: 64}
		h.updateComparator(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		h.tick.Store(h.tick.Load() + comparatorDelay)
		h.updateComparator(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if h.compOut[pos] <= 0 {
			t.Errorf("comparator read %d through a solid block, want > 0", h.compOut[pos])
		}
	})
}
