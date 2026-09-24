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

// A lever hanging under a piston pops off when the piston RETRACTS, and only
// then. This looks like a bug — Legion reported it from in game — but it is
// vanilla: retraction swaps the base for a moving_piston, whose shape is
// empty, so the lever's attachment face is no longer sturdy and its shape
// update drops it. Extension leaves a real piston block behind, so the lever
// survives that. Pinned so nobody "fixes" the quirk away.
func TestLeverUnderAPistonPopsOnRetractOnly(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pist := worldgen.BlockBase("sticky_piston")
	info, _ := worldgen.InfoForState(pist)
	pist = setBoolProp(worldgen.SetProperty(info, pist, "facing", "up"), "extended", false)
	w.SetBlock(x, y+1, z, pist)
	w.SetBlock(x, y+2, z, worldgen.Stone)

	lever := worldgen.BlockBase("lever")
	li, _ := worldgen.InfoForState(lever)
	lever = worldgen.SetProperty(li, worldgen.SetProperty(li, lever, "face", "ceiling"), "facing", "north")
	w.SetBlock(x, y, z, setBoolProp(lever, "powered", false))

	w.SetBlock(x, y, z, setBoolProp(lever, "powered", true))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 20)
	if !isSameBlock(w.At(x, y, z), lever) {
		t.Fatal("the lever should still hang under the extending piston")
	}
	if w.At(x, y+3, z) != worldgen.Stone {
		t.Fatalf("the piston should have pushed the stone up, got %d", w.At(x, y+3, z))
	}

	w.SetBlock(x, y, z, setBoolProp(lever, "powered", false))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 30)
	if w.At(x, y+2, z) != worldgen.Stone {
		t.Fatalf("the sticky piston should have pulled the stone back, got %d", w.At(x, y+2, z))
	}
	if w.At(x, y, z) != worldgen.Air {
		t.Fatalf("the lever should have popped off the moving base, got %d", w.At(x, y, z))
	}
}

// TestPistonShovesAWideMobClear (bug #32): a sheep is 0.9 wide, so its
// box can straddle the cell a pushed block lands in while its centre is
// outside it. Vanilla moves every entity whose box meets the moving block
// until it is clear; we once checked only the centre, leaving the sheep
// half inside the wall — drawn black by the client.
func TestPistonShovesAWideMobClear(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, worldgen.Stone)
	for dx := 1; dx <= 5; dx++ {
		w.SetBlock(x+dx, y-1, z, worldgen.Stone)
	}
	s := h.spawnMob(players, entitySheep, float64(x)+3.2, float64(y), float64(z)+0.5)
	s.baby = false
	s.rest = 1000
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if w.At(x+2, y, z) != worldgen.Stone {
		t.Fatalf("the stone should land at x+2: %d", w.At(x+2, y, z))
	}
	half := s.box().w / 2
	if s.x-half < float64(x+3)-1e-9 && s.x+half > float64(x+2) {
		t.Fatalf("sheep box %.3f..%.3f still overlaps the stone at %d..%d", s.x-half, s.x+half, x+2, x+3)
	}
}

// TestWaterDoesNotWashAMovingBlock (bug #34): a block sliding in a piston's
// moving cell is not something water can flow into (the cell is forced
// solid, and not #washed_away_by_fluids). Our water treated the shapeless
// cell as washable, replaced it, and the block it carried was never laid.
func TestWaterDoesNotWashAMovingBlock(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, worldgen.Stone)
	for dx := 0; dx <= 3; dx++ {
		w.SetBlock(x+dx, y-1, z+1, worldgen.Stone)
		w.SetBlock(x+dx, y, z+2, worldgen.Stone)
	}
	w.SetBlock(x+2, y, z+1, worldgen.WaterBase) // a source beside where the stone lands
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	moving := false
	for i := 0; i < 6 && !moving; i++ {
		stepTicks(h, players, 1)
		moving = isMovingPiston(w.At(x+2, y, z))
	}
	if !moving {
		t.Fatalf("the stone never started moving: %d", w.At(x+2, y, z))
	}
	h.scheduleIn(0, blockPos{x + 2, y, z + 1}, 1) // the water ticks while the stone slides
	stepTicks(h, players, 6)
	if got := w.At(x+2, y, z); got != worldgen.Stone {
		t.Fatalf("the pushed stone should land beside the water, got %d", got)
	}
}
