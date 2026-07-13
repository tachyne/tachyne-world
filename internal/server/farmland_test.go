package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// clearBox wipes the ±4×±1 farmland scan box to air so farmlandNearWater is
// deterministic regardless of the generated terrain under the test column.
func clearBox(w interface {
	SetBlock(x, y, z int, s uint32)
}, x, y, z int) {
	for dy := -1; dy <= 2; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
}

func TestFarmland(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world
	farmland := worldgen.BlockBase("farmland")

	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival

		// Hydration: dry soil (moisture 0) with a water source in range → 7.
		clearBox(w, 5, 100, 5)
		w.SetBlock(5, 100, 5, farmland) // moisture 0
		w.SetBlock(7, 100, 5, worldgen.WaterBase)
		h.farmlandRandomTick(h.playersRef, 5, 100, 5, w.At(5, 100, 5))
		if got := w.At(5, 100, 5); got != farmland+7 {
			t.Errorf("hydrate: moisture state %d, want %d", got-farmland, 7)
		}

		// Dehydration: moist soil (moisture 5), no water/rain → 4.
		clearBox(w, 5, 100, 40)
		w.SetBlock(5, 100, 40, farmland+5)
		h.farmlandRandomTick(h.playersRef, 5, 100, 40, w.At(5, 100, 40))
		if got := w.At(5, 100, 40); got != farmland+4 {
			t.Errorf("dehydrate: moisture state %d, want 4", got-farmland)
		}

		// Dry-out: bone-dry soil (moisture 0), nothing growing → reverts to dirt.
		clearBox(w, 5, 100, 80)
		w.SetBlock(5, 100, 80, farmland) // moisture 0
		h.farmlandRandomTick(h.playersRef, 5, 100, 80, w.At(5, 100, 80))
		if got := w.At(5, 100, 80); got != worldgen.Dirt {
			t.Errorf("dry-out: block %d, want dirt %d", got, worldgen.Dirt)
		}

		// Maintained: dry soil with wheat on top stays tilled (won't revert).
		clearBox(w, 5, 100, 120)
		w.SetBlock(5, 100, 120, farmland) // moisture 0
		w.SetBlock(5, 101, 120, worldgen.BlockBase("wheat"))
		h.farmlandRandomTick(h.playersRef, 5, 100, 120, w.At(5, 100, 120))
		if got := w.At(5, 100, 120); got != farmland {
			t.Errorf("maintained: block %d, want farmland (unchanged)", got)
		}

		// Trampling: a hard landing turns soil to dirt and pops the crop above.
		clearBox(w, 5, 100, 160)
		w.SetBlock(5, 100, 160, farmland+7)
		w.SetBlock(5, 101, 160, worldgen.BlockBase("wheat")+7) // mature crop
		h.tramplePlayer(h.playersRef, tr, 5, 100, 160, 10)     // dist 10 → prob ~9.5, certain
		if got := w.At(5, 100, 160); got != worldgen.Dirt {
			t.Errorf("trample: soil %d, want dirt", got)
		}
		if got := w.At(5, 101, 160); got != worldgen.Air {
			t.Errorf("trample: crop %d not popped", got)
		}
	})
}
