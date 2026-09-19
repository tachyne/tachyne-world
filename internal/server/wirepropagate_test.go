package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A line of dust carries a signal end to end within the tick it changes,
// and drops it the same way — not one block per tick.
func TestDustPropagatesWithinTheTick(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	for dx := 0; dx < 9; dx++ { // a longer floor for the run
		for dz := -1; dz <= 1; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
		}
	}
	lever := withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "facing": "east", "powered": "false"})
	w.SetBlock(x, y, z, lever)
	for i := 1; i <= 8; i++ {
		w.SetBlock(x+i, y, z, worldgen.BlockBase("redstone_wire")+1160)
	}
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 1)
	if p := wirePower(w.At(x+8, y, z)); p != 15-7 {
		t.Fatalf("the far dust should carry %d one tick after the lever, has %d (near %d)", 15-7, p, wirePower(w.At(x+1, y, z)))
	}
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 1)
	for i := 1; i <= 8; i++ {
		if p := wirePower(w.At(x+i, y, z)); p != 0 {
			t.Fatalf("dust %d should be dark one tick after the lever opened, has %d", i, p)
		}
	}
}
