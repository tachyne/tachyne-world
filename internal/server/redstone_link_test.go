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

// A dust line either side of a repeater goes dark when the source is cut —
// the second half of Legion's report ("redstone does not visually turn off
// when a repeater is activated"). The powered property is what the client
// renders, so every dust must read 0 again, not just the lamp.
func TestDustGoesDarkThroughARepeater(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	dust := worldgen.BlockBase("redstone_wire")
	w.SetBlock(x, y, z, worldgen.BlockBase("redstone_block"))
	for i := 1; i <= 4; i++ {
		w.SetBlock(x+i, y, z, dust)
	}
	w.SetBlock(x+5, y, z, repFacing("west"))
	for i := 6; i <= 10; i++ {
		w.SetBlock(x+i, y, z, dust)
	}
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	for i := 1; i <= 10; i++ {
		h.rsSchedule(blockPos{x + i, y, z}, 1)
	}
	stepTicks(h, players, 20)
	if p := wirePower(w.At(x+6, y, z)); p != 15 {
		t.Fatalf("the repeater should refresh the line to 15, got %d", p)
	}
	w.SetBlock(x, y, z, worldgen.Stone)
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 40)
	if boolProp(w.At(x+5, y, z), "powered") {
		t.Error("the repeater should have dropped")
	}
	for i := 1; i <= 10; i++ {
		if i == 5 {
			continue
		}
		if p := wirePower(w.At(x+i, y, z)); p != 0 {
			t.Errorf("dust %d blocks along still reads %d", i, p)
		}
	}
}
