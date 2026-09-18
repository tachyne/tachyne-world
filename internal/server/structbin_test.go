package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A jungle temple's dispenser is loaded with arrows from its own table the
// first time anything touches it, so the tripwire trap fires; any other
// dispenser starts empty.
func TestJungleTempleDispenserLoot(t *testing.T) {
	h := newHub(world.New(5))
	g := h.world.Gen()
	var tp worldgen.JungleTemple
	found := false
	for r := 0; r < 40 && !found; r++ {
		for cx := -r; cx <= r && !found; cx++ {
			for _, cz := range []int{-r, r} {
				if tt := g.JungleTempleIn(cx*512+8, cz*512+8); tt.Exists {
					tp, found = tt, true
					break
				}
			}
		}
	}
	if !found {
		t.Skip("no jungle temple within the scan for seed 5")
	}
	state := worldgen.BlockID("dispenser")
	arrow := int32(itemByName["arrow"])
	for _, d := range tp.Dispensers() {
		pos := simPos{dim: dimOverworld, blockPos: blockPos{d[0], d[1], d[2]}}
		c := h.binAt(pos, state)
		total := 0
		for _, s := range c.slots {
			if s.count > 0 && s.item != arrow {
				t.Fatalf("a trap dispenser holds %d, not arrows", s.item)
			}
			total += s.count
		}
		if total < 2 || total > 14 {
			t.Fatalf("trap at %v holds %d arrows, want one or two rolls of 2–7", d, total)
		}
		if h.binAt(pos, state) != c {
			t.Fatal("the bin is not kept")
		}
	}
	plain := h.binAt(simPos{dim: dimOverworld, blockPos: blockPos{tp.X + 40, 70, tp.Z + 40}}, state)
	for _, s := range plain.slots {
		if s.count > 0 {
			t.Fatal("a dispenser outside the temple started with items")
		}
	}
}
