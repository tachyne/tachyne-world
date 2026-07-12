package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// repFacing builds a repeater state with a given facing (input side).
func repFacing(facing string) uint32 {
	info, _ := worldgen.InfoForState(uint32((worldgen.BlockBase("repeater") + 3)))
	return worldgen.SetProperty(info, uint32((worldgen.BlockBase("repeater") + 3)), "facing", facing)
}

func TestRepeaterRefreshesAndDelays(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, worldgen.BlockBase("redstone_block")) // redstone block source
	w.SetBlock(x+1, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	// facing=west → input from the west (toward the source), output east.
	w.SetBlock(x+2, y, z, repFacing("west"))
	w.SetBlock(x+3, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	w.SetBlock(x+4, y, z, lampOff)
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 12)
	if !boolProp(w.At(x+2, y, z), "powered") {
		t.Fatalf("repeater should power: in-wire=%d state=%d", wirePower(w.At(x+1, y, z)), w.At(x+2, y, z))
	}
	if p := wirePower(w.At(x+3, y, z)); p != 15 {
		t.Fatalf("repeater must refresh the signal to 15, output wire has %d", p)
	}
	if w.At(x+4, y, z) != lampOn {
		t.Fatal("lamp past the repeater should light")
	}
	// Cut the source: repeater drops after its delay, lamp goes out.
	w.SetBlock(x, y, z, worldgen.Stone)
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 30)
	if boolProp(w.At(x+2, y, z), "powered") || w.At(x+4, y, z) != lampOff {
		t.Fatal("repeater and lamp should drop when the source goes")
	}
}

func TestRepeaterBlocksReverseFlow(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	// Source on the OUTPUT side: nothing should flow backward.
	w.SetBlock(x, y, z, worldgen.BlockBase("redstone_block"))
	w.SetBlock(x+1, y, z, repFacing("east")) // input from the east — the wrong side
	w.SetBlock(x+2, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 8)
	if boolProp(w.At(x+1, y, z), "powered") {
		t.Fatal("repeater fed from its output side must stay off")
	}
}

func TestComparatorModes(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	// Rear signal 15 via a redstone block behind (facing=west → rear at west).
	w.SetBlock(x, y, z, worldgen.BlockBase("redstone_block"))
	info, _ := worldgen.InfoForState(uint32((worldgen.BlockBase("comparator") + 1)))
	comp := worldgen.SetProperty(info, uint32((worldgen.BlockBase("comparator") + 1)), "facing", "west")
	w.SetBlock(x+1, y, z, comp)
	w.SetBlock(x+2, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	// Side signal 15: torch south of the comparator.
	w.SetBlock(x+1, y-1, z+1, worldgen.Stone)
	w.SetBlock(x+1, y, z+1, uint32(worldgen.BlockBase("redstone_torch")))
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 6)
	// Compare mode: rear(15) >= side(15) → out 15.
	if p := wirePower(w.At(x+2, y, z)); p != 15 {
		t.Fatalf("compare mode should pass 15 to the wire, got %d", p)
	}
	// Subtract mode: 15-15 = 0.
	h.useRedstone1b(players, blockPos{x + 1, y, z}, w.At(x+1, y, z))
	stepTicks(h, players, 6)
	if p := wirePower(w.At(x+2, y, z)); p != 0 {
		t.Fatalf("subtract mode 15-15 should output 0, wire has %d", p)
	}
}

func TestObserverPulsesOnChange(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	// Observer watching west (the cell at x-1), output east into a wire.
	info, _ := worldgen.InfoForState(uint32((worldgen.BlockBase("observer") + 5)))
	obs := worldgen.SetProperty(info, uint32((worldgen.BlockBase("observer") + 5)), "facing", "west")
	w.SetBlock(x, y, z, obs)
	w.SetBlock(x+1, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 3) // seed obsSeen
	w.SetBlock(x-1, y, z, worldgen.Stone)
	h.scheduleAround(blockPos{x - 1, y, z}, 1)
	stepTicks(h, players, 2)
	if !boolProp(w.At(x, y, z), "powered") {
		t.Fatal("observer must pulse when the watched block changes")
	}
	if p := wirePower(w.At(x+1, y, z)); p != 15 {
		t.Fatalf("observer back should emit 15 to the wire, got %d", p)
	}
	stepTicks(h, players, 8)
	if boolProp(w.At(x, y, z), "powered") || wirePower(w.At(x+1, y, z)) != 0 {
		t.Fatal("observer pulse must expire")
	}
}

func TestPlatePressAndRelease(t *testing.T) {
	h, w, _, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, stonePlateOff)
	w.SetBlock(x+1, y, z, lampOff)
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y)+0.06, float64(z)+0.5
	players := map[int32]*tracked{1: pl}
	h.updatePlates(players)
	stepTicks(h, players, 4)
	if w.At(x, y, z) != stonePlateOn || w.At(x+1, y, z) != lampOn {
		t.Fatalf("standing on a plate must press it + light the lamp: plate=%d lamp=%d",
			w.At(x, y, z), w.At(x+1, y, z))
	}
	pl.x += 3 // step off
	h.updatePlates(players)
	stepTicks(h, players, 20)
	if w.At(x, y, z) != stonePlateOff || w.At(x+1, y, z) != lampOff {
		t.Fatalf("plate must release when empty: plate=%d lamp=%d", w.At(x, y, z), w.At(x+1, y, z))
	}
}

func TestDaylightDetectorFollowsSun(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, daylightWith(false, 0))
	h.dayTime.Store(6000) // noon
	h.schedule(blockPos{x, y, z}, 1)
	stepTicks(h, players, 3)
	if p := daylightPower(w.At(x, y, z)); p != 15 {
		t.Fatalf("noon should read 15, got %d", p)
	}
	// Invert: noon reads 0.
	h.useRedstone1b(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 3)
	if p := daylightPower(w.At(x, y, z)); p != 0 {
		t.Fatalf("inverted noon should read 0, got %d", p)
	}
}
