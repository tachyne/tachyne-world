package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A dispenser waits vanilla's 4-tick delay after its rising edge before it
// ejects — it must not fire on the same tick the edge is detected.
func TestDispenserFireDelay(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, dispEast(dispenserMin))
	pos := blockPos{x, y, z}
	h.bins[pos] = &bin{slots: make([]invStack, 9)}
	h.bins[pos].slots[0] = invStack{item: itemArrowAmmo, count: 1}
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(pos, 1) // edge detected at tick 1 → scheduled to fire at tick 5

	stepTicks(h, players, 4) // reach tick 4: still within the delay
	if len(h.arrows) != 0 {
		t.Fatalf("dispenser fired before its 4-tick delay elapsed (%d arrows)", len(h.arrows))
	}
	stepTicks(h, players, 2) // reach tick 6: the delay has passed
	if len(h.arrows) != 1 {
		t.Fatalf("dispenser should fire once the delay elapses, have %d arrows", len(h.arrows))
	}
}

// Quasi-connectivity: a redstone signal at the block directly ABOVE the
// dispenser powers it, even with no direct signal to the dispenser itself.
func TestDispenserQuasiConnectivity(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, dispEast(dispenserMin))
	pos := blockPos{x, y, z}
	h.bins[pos] = &bin{slots: make([]invStack, 9)}
	h.bins[pos].slots[0] = invStack{item: itemArrowAmmo, count: 1}
	// Redstone block two above → powers the cell above the dispenser (y+1), but
	// is not adjacent to the dispenser itself. Vanilla QC still triggers it.
	w.SetBlock(x, y+2, z, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(pos, 1)

	stepTicks(h, players, 8) // edge + 4-tick delay, with margin
	if len(h.arrows) != 1 {
		t.Fatalf("quasi-connectivity: a signal above the dispenser should fire it, have %d arrows", len(h.arrows))
	}
}
