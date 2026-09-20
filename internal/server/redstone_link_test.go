package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Dust laid beside dust links at once, unpowered — Legion hit this in game:
// "I can place redstone dust but it does not link until activated". The
// connection shape was only rewritten when a dust's POWER changed, so two
// unpowered dusts sat side by side as separate dots.
func TestDustLinksWhenPlaced(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	dust := worldgen.BlockBase("redstone_wire")
	w.SetBlock(x, y, z, dust)
	h.rsSchedule(blockPos{x, y, z}, 1)
	stepSculk(h, players, 4)
	lone := w.At(x, y, z)

	w.SetBlock(x+1, y, z, dust) // a neighbour arrives
	h.rsSchedule(blockPos{x + 1, y, z}, 1)
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepSculk(h, players, 6)
	after := w.At(x, y, z)
	if after == lone {
		t.Error("the first dust never re-linked when the second was placed")
	}
	info, _ := worldgen.InfoForState(after)
	if worldgen.GetProperty(info, after, "east") == "none" {
		t.Errorf("it should connect east to its neighbour, got %q", worldgen.GetProperty(info, after, "east"))
	}
}
