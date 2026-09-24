package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// DiodeBlock: an input pulse shorter than the repeater's delay is not lost —
// the scheduled tick still turns the output on, and it stays on one delay.
func TestRepeaterExtendsShortPulse(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lever := withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "facing": "east", "powered": "false"})
	w.SetBlock(x, y, z, lever)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_wire")+1160)
	rep := repFacing("west")
	ri, _ := worldgen.InfoForState(rep)
	rep = worldgen.SetProperty(ri, rep, "delay", "4")
	w.SetBlock(x+2, y, z, rep)
	delay := repeaterDelay(rep)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 3)                                 // dust carries the signal a tick at a time here; still well under the 8-tick delay
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z)) // a short pulse
	onTicks, seenOn, seenOff := 0, false, false
	for i := 0; i < 40; i++ {
		stepTicks(h, players, 1)
		if boolProp(w.At(x+2, y, z), "powered") {
			onTicks++
			seenOn = true
		} else if seenOn {
			seenOff = true
			break
		}
	}
	if !seenOn || !seenOff {
		t.Fatalf("the repeater should pass the pulse: on=%v off=%v", seenOn, seenOff)
	}
	if onTicks != delay {
		t.Fatalf("the output pulse should last one delay (%d ticks), got %d", delay, onTicks)
	}
}

// RedstoneLampBlock: lights at once, goes dark four ticks after the power
// leaves, and stays lit if the power returns within those four.
func TestLampGoesDarkFourTicksLater(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lever := withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "facing": "east", "powered": "false"})
	w.SetBlock(x, y, z, lever)
	w.SetBlock(x+1, y, z, lampOff)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 2)
	if w.At(x+1, y, z) != lampOn {
		t.Fatal("the lamp lights at once")
	}
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 3)
	if w.At(x+1, y, z) != lampOn {
		t.Fatal("the lamp should still be lit three ticks after the power left")
	}
	stepTicks(h, players, 2)
	if w.At(x+1, y, z) != lampOff {
		t.Fatal("the lamp should be dark four ticks after the power left")
	}
	// Power back within the window: it never goes dark.
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 2)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 2)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 6)
	if w.At(x+1, y, z) != lampOn {
		t.Fatal("power returning within four ticks keeps the lamp lit")
	}
}

// A pressure plate releases getPressedTime after the last thing stood on
// it: twenty ticks, or ten for the weighted plates.
func TestPlateReleasesAfterPressedTime(t *testing.T) {
	for _, c := range []struct {
		plate string
		ticks int
	}{{"oak_pressure_plate", 20}, {"light_weighted_pressure_plate", 10}, {"heavy_weighted_pressure_plate", 10}} {
		h, w, players, x, y, z := redSetup(t)
		w.SetBlock(x, y, z, worldgen.BlockID(c.plate))
		it := h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y), float64(z)+0.5, 0, 0, 0)
		h.inDim(0, func() { h.updatePlatesIn(players, 0) })
		if platePower(w.At(x, y, z)) == 0 {
			t.Fatalf("%s: the item presses the plate", c.plate)
		}
		delete(h.items, it.eid)
		for i := 0; i < c.ticks-1; i++ {
			h.tick.Add(1)
			h.inDim(0, func() { h.updatePlatesIn(players, 0) })
			if platePower(w.At(x, y, z)) == 0 {
				t.Fatalf("%s released %d ticks after the item left, want %d", c.plate, i+1, c.ticks)
			}
		}
		h.tick.Add(1)
		h.inDim(0, func() { h.updatePlatesIn(players, 0) })
		if platePower(w.At(x, y, z)) != 0 {
			t.Fatalf("%s should release at %d ticks", c.plate, c.ticks)
		}
	}
}
