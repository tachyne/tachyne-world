package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCopperWeathering(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world

	// The classifier resolves every stage of every family, including the
	// propertyless full blocks and the stateful stairs/slabs.
	for _, fam := range copperFamilies {
		for stage, name := range fam {
			lo, hi, ok := safeRange(name)
			if !ok {
				t.Fatalf("copper block %q missing", name)
			}
			for _, s := range []uint32{lo, hi} {
				cr, ok := copperOf(s)
				if !ok || cr.stage != stage {
					t.Fatalf("%s state %d classified %+v ok=%v, want stage %d", name, s, cr, ok, stage)
				}
			}
		}
	}

	onHub(t, h, func() {
		// An isolated copper block oxidizes one stage at a time all the way to
		// oxidized (no less-oxidized neighbour to halt it).
		base := worldgen.BlockBase("copper_block")
		w.SetBlock(5, 100, 5, base)
		for i := 0; i < 20000; i++ {
			cur := w.At(5, 100, 5)
			cr, _ := copperOf(cur)
			if cr.stage == 3 {
				break
			}
			h.tickCopper(h.playersRef, 5, 100, 5, cur)
		}
		if cr, _ := copperOf(w.At(5, 100, 5)); cr.stage != 3 {
			t.Errorf("isolated copper reached stage %d, want 3 (oxidized)", cr.stage)
		}

		// A less-oxidized neighbour halts oxidation entirely: a fresh copper
		// block next to an exposed one never advances.
		fresh := worldgen.BlockBase("copper_block")
		exposed := worldgen.BlockBase("exposed_copper")
		w.SetBlock(20, 100, 20, exposed)
		w.SetBlock(21, 100, 20, fresh) // neighbour one stage LESS oxidized than exposed
		for i := 0; i < 5000; i++ {
			h.tickCopper(h.playersRef, 20, 100, 20, w.At(20, 100, 20))
		}
		if got := w.At(20, 100, 20); got != exposed {
			cr, _ := copperOf(got)
			t.Errorf("exposed copper oxidized to stage %d despite a fresher neighbour", cr.stage)
		}

		// Property preservation: a stateful copper stair keeps its facing/half/
		// shape when it oxidizes. Drive an offset (non-base) state up a stage.
		slo, shi, _ := safeRange("cut_copper_stairs")
		mid := slo + (shi-slo)/2 // some oriented, waterlogged, etc. state
		w.SetBlock(30, 100, 30, mid)
		advanced := false
		for i := 0; i < 20000 && !advanced; i++ {
			h.tickCopper(h.playersRef, 30, 100, 30, w.At(30, 100, 30))
			if got := w.At(30, 100, 30); got != mid {
				elo, _, _ := safeRange("exposed_cut_copper_stairs")
				if got-elo != mid-slo {
					t.Errorf("stair oxidized to offset %d, want %d (properties not preserved)", got-elo, mid-slo)
				}
				advanced = true
			}
		}
		if !advanced {
			t.Error("cut copper stairs never oxidized")
		}
	})
}
