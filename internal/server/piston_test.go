package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// pistonEast builds a piston state facing east (extended=false).
func pistonEast(sticky bool) uint32 {
	base := uint32(pistonMin)
	if sticky {
		base = stickyPistonMin
	}
	info, _ := worldgen.InfoForState(base + 6) // default-ish: find unextended north
	s := worldgen.SetProperty(info, base+6, "facing", "east")
	return setBoolProp(s, "extended", false)
}

func TestPistonPushesColumn(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x+2, y, z, worldgen.Stone)
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block")) // power from the side
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if !boolProp(w.At(x, y, z), "extended") || !isPistonHead(w.At(x+1, y, z)) {
		t.Fatalf("piston should extend: base=%d front=%d", w.At(x, y, z), w.At(x+1, y, z))
	}
	if w.At(x+2, y, z) != worldgen.Stone || w.At(x+3, y, z) != worldgen.Stone {
		t.Fatalf("both stones should shift east: %d %d", w.At(x+2, y, z), w.At(x+3, y, z))
	}
	// Cut power: retract, head gone, stones stay.
	w.SetBlock(x, y, z-1, worldgen.Stone)
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if boolProp(w.At(x, y, z), "extended") || w.At(x+1, y, z) != worldgen.Air {
		t.Fatalf("piston should retract: base=%d front=%d", w.At(x, y, z), w.At(x+1, y, z))
	}
}

func TestPistonBlockedByObsidian(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, obsidianState)
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if boolProp(w.At(x, y, z), "extended") {
		t.Fatal("piston must not extend into obsidian")
	}
}

func TestPistonPushLimit(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	for i := 1; i <= 13; i++ { // 13 > the 12-block cap
		w.SetBlock(x+i, y, z, worldgen.Stone)
	}
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if boolProp(w.At(x, y, z), "extended") {
		t.Fatal("13 blocks exceed the push limit")
	}
}

func TestStickyPistonPullsBack(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(true))
	w.SetBlock(x+1, y, z, worldgen.Stone)
	lever := setBoolProp(uint32((worldgen.BlockBase("lever") + 9)), "powered", false)
	w.SetBlock(x, y, z-1, lever)
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 4)
	if !isPistonHead(w.At(x+1, y, z)) || w.At(x+2, y, z) != worldgen.Stone {
		t.Fatalf("sticky should extend + push: %d %d", w.At(x+1, y, z), w.At(x+2, y, z))
	}
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 4)
	if w.At(x+1, y, z) != worldgen.Stone || w.At(x+2, y, z) != worldgen.Air {
		t.Fatalf("sticky retract should pull the stone back: %d %d", w.At(x+1, y, z), w.At(x+2, y, z))
	}
}
