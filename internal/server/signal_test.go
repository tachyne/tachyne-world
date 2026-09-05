package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Strong ("direct") power through solid blocks — the vanilla SignalGetter
// model. Every scenario here is one a player builds in the first hour and
// the engine got wrong before signal.go: a lever on the far side of a wall,
// dust ending in a block, a torch under a block.

func withProps(t *testing.T, base uint32, props map[string]string) uint32 {
	t.Helper()
	info, ok := worldgen.InfoForState(base)
	if !ok {
		t.Fatalf("no property info for state %d", base)
	}
	s := base
	for k, v := range props {
		s = worldgen.SetProperty(info, s, k, v)
	}
	return s
}

func wallLever(t *testing.T, facing string) uint32 {
	return withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "wall", "facing": facing, "powered": "false"})
}
func floorLever(t *testing.T) uint32 {
	return withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "facing": "north", "powered": "false"})
}
func dust(t *testing.T) uint32 {
	return withProps(t, worldgen.BlockBase("redstone_wire"), map[string]string{"power": "0"})
}

// A lever mounted on a wall drives the block it hangs from; that block then
// powers the lamp on its far side — and the lamp above it.
func TestLeverBehindWallPowersLampBeyond(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)     // the wall
	w.SetBlock(x, y, z, wallLever(t, "west")) // lever on the wall's WEST face: facing west, attached to the east
	w.SetBlock(x+2, y, z, lampOff)            // far side
	w.SetBlock(x+1, y+1, z, lampOff)          // on top of the wall
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+2, y, z) != lampOn {
		t.Fatal("lamp on the far side of the wall should light: the wall carries the lever's direct power")
	}
	if w.At(x+1, y+1, z) != lampOn {
		t.Fatal("lamp on top of the powered wall should light")
	}
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+2, y, z) != lampOff || w.At(x+1, y+1, z) != lampOff {
		t.Fatal("lamps should go out when the lever opens")
	}
}

// Dust ending in a solid block powers it strongly: a lamp beyond lights —
// but dust beyond does NOT (the block's input was dust, and dust is silent
// while dust computes; wire→block→wire never carries in vanilla).
func TestDustIntoBlockPowersLampButNotDust(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, floorLever(t))
	w.SetBlock(x+1, y, z, dust(t))
	w.SetBlock(x+2, y, z, dust(t))
	w.SetBlock(x+3, y, z, worldgen.Stone)
	w.SetBlock(x+4, y, z, lampOff)
	w.SetBlock(x+3, y+1, z, dust(t)) // dust on top of the block: a stair step from x+2, not a pass-through
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 16)
	if w.At(x+4, y, z) != lampOn {
		t.Fatalf("lamp beyond the block should light (dust %d %d)", wirePower(w.At(x+1, y, z)), wirePower(w.At(x+2, y, z)))
	}
	if p := wirePower(w.At(x+3, y+1, z)); p != 13 {
		t.Fatalf("dust on top of the block is a diagonal step from the 14 beside it: want 13, has %d", p)
	}
	// Swap the lamp for dust, with the stair-step dust removed so nothing but
	// the block itself could feed it: still 0.
	w.SetBlock(x+3, y+1, z, worldgen.Air)
	w.SetBlock(x+4, y, z, dust(t))
	h.scheduleSignalAround(blockPos{x + 3, y + 1, z})
	h.scheduleSignalAround(blockPos{x + 4, y, z})
	stepTicks(h, players, 8)
	if p := wirePower(w.At(x+4, y, z)); p != 0 {
		t.Fatalf("dust beyond a dust-powered block must stay 0, has %d", p)
	}
}

// A redstone torch strongly powers the block ABOVE it, and never the block
// it stands on.
func TestTorchPowersBlockAboveNotBelow(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, rsTorchMin) // lit floor torch on the stone pad
	w.SetBlock(x, y+1, z, worldgen.Stone)
	w.SetBlock(x+1, y+1, z, lampOff) // beside the block above the torch
	w.SetBlock(x+1, y-1, z, lampOff) // beside the block the torch stands on
	h.scheduleSignalAround(blockPos{x, y, z})
	stepTicks(h, players, 8)
	if w.At(x+1, y+1, z) != lampOn {
		t.Fatal("lamp beside the block above a torch should light: the torch drives that block strongly")
	}
	if w.At(x+1, y-1, z) != lampOff {
		t.Fatal("lamp beside the torch's support must stay dark: a torch never powers its own support")
	}
	if !torchLit(w.At(x, y, z)) {
		t.Fatal("torch went out: it powered its own support and inverted")
	}
}

// A block of redstone powers what touches it, but drives nothing strongly:
// a solid block beside it stays inert.
func TestRedstoneBlockDoesNotPowerThroughSolid(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, redstoneBlock)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x+2, y, z, lampOff)
	w.SetBlock(x, y+1, z, lampOff) // touching the redstone block itself
	h.scheduleSignalAround(blockPos{x, y, z})
	stepTicks(h, players, 8)
	if w.At(x, y+1, z) != lampOn {
		t.Fatal("lamp touching a redstone block should light")
	}
	if w.At(x+2, y, z) != lampOff {
		t.Fatal("lamp beyond a solid block next to a redstone block must stay dark: no direct signal")
	}
}

// A repeater's output drives the block in front of it strongly.
func TestRepeaterIntoBlockPowersLampBeyond(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, floorLever(t))
	w.SetBlock(x+1, y, z, dust(t))
	w.SetBlock(x+2, y, z, withProps(t, worldgen.BlockBase("repeater"),
		map[string]string{"facing": "west", "delay": "1", "powered": "false", "locked": "false"})) // input west, output east
	w.SetBlock(x+3, y, z, worldgen.Stone)
	w.SetBlock(x+4, y, z, lampOff)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 20)
	if w.At(x+4, y, z) != lampOn {
		t.Fatalf("lamp beyond the block in front of the repeater should light (repeater powered=%v)",
			boolProp(w.At(x+2, y, z), "powered"))
	}
}

// Glass is a full cube but vanilla exempts it: nothing conducts through it.
func TestGlassDoesNotConduct(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("glass"))
	w.SetBlock(x, y, z, wallLever(t, "west"))
	w.SetBlock(x+2, y, z, lampOff)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+2, y, z) != lampOff {
		t.Fatal("lamp beyond glass must stay dark: glass never conducts")
	}
}

// The conductor rule, per state.
func TestConductsFollowsVanilla(t *testing.T) {
	slab := worldgen.BlockBase("oak_slab")
	cases := []struct {
		name string
		s    uint32
		want bool
	}{
		{"stone", worldgen.Stone, true},
		{"air", worldgen.Air, false},
		{"glass", worldgen.BlockBase("glass"), false},
		{"bottom slab", withProps(t, slab, map[string]string{"type": "bottom"}), false},
		{"double slab", withProps(t, slab, map[string]string{"type": "double"}), true},
		{"soul sand (not a full cube, conducts anyway)", worldgen.BlockBase("soul_sand"), true},
		{"redstone block", redstoneBlock, false},
		{"oak leaves", worldgen.BlockBase("oak_leaves"), false},
		{"lit lamp", lampOn, true},
		{"piston base", worldgen.BlockBase("piston"), false},
	}
	for _, c := range cases {
		if got := conducts(c.s); got != c.want {
			t.Errorf("%s: conducts = %v, want %v", c.name, got, c.want)
		}
	}
}
