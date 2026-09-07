package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A slime block in the line drags its side neighbours along; honey and slime
// refuse each other; glazed terracotta moves only away from the piston; a
// torch in the way breaks; a chest stops the piston.
// noFloor clears the pad under the slime's cells so it has nothing below to
// stick to (a slime on the ground would try to take the ground along).
func noFloor(w *world.World, x, y, z int) {
	w.SetBlock(x+1, y-1, z, worldgen.Air)
	w.SetBlock(x+2, y-1, z, worldgen.Air)
}

func TestPistonStructureResolver(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	power := worldgen.BlockBase("redstone_block")
	stone := uint32(worldgen.Stone)
	glazed := worldgen.BlockBase("white_glazed_terracotta")
	chest := worldgen.BlockBase("chest")
	torch := worldgen.BlockBase("torch")

	// Slime ahead with a stone beside it: both move east.
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, slimeMin)
	w.SetBlock(x+1, y+1, z, stone)
	w.SetBlock(x, y, z-1, power)
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if w.At(x+2, y, z) != slimeMin || w.At(x+2, y+1, z) != stone || w.At(x+1, y+1, z) != worldgen.Air {
		t.Fatalf("slime should drag the stone beside it: %d %d %d", w.At(x+2, y, z), w.At(x+2, y+1, z), w.At(x+1, y+1, z))
	}

	// Honey beside slime does not come along.
	h2, w2, players2, x, y, z := redSetup(t)
	noFloor(w2, x, y, z)
	w2.SetBlock(x, y, z, pistonEast(false))
	w2.SetBlock(x+1, y, z, slimeMin)
	w2.SetBlock(x+1, y+1, z, honeyMin)
	w2.SetBlock(x, y, z-1, power)
	h2.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h2, players2, 4)
	if w2.At(x+2, y, z) != slimeMin || w2.At(x+1, y+1, z) != honeyMin {
		t.Fatalf("honey must not stick to slime: %d %d", w2.At(x+2, y, z), w2.At(x+1, y+1, z))
	}

	// Glazed terracotta beside a slime is push-only: it does not move sideways.
	h3, w3, players3, x, y, z := redSetup(t)
	noFloor(w3, x, y, z)
	w3.SetBlock(x, y, z, pistonEast(false))
	w3.SetBlock(x+1, y, z, slimeMin)
	w3.SetBlock(x+1, y+1, z, glazed)
	w3.SetBlock(x, y, z-1, power)
	h3.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h3, players3, 4)
	if w3.At(x+2, y, z) != slimeMin || w3.At(x+1, y+1, z) != glazed {
		t.Fatalf("glazed terracotta beside a slime stays: %d %d", w3.At(x+2, y, z), w3.At(x+1, y+1, z))
	}

	// A torch at the end of the line breaks and the line still moves.
	h4, w4, players4, x, y, z := redSetup(t)
	w4.SetBlock(x, y, z, pistonEast(false))
	w4.SetBlock(x+1, y, z, stone)
	w4.SetBlock(x+2, y, z, torch)
	w4.SetBlock(x, y, z-1, power)
	h4.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h4, players4, 4)
	if w4.At(x+2, y, z) != stone || !boolProp(w4.At(x, y, z), "extended") {
		t.Fatalf("a torch should break for the push: front=%d extended=%v", w4.At(x+2, y, z), boolProp(w4.At(x, y, z), "extended"))
	}

	// A chest (block entity) blocks the push entirely.
	h5, w5, players5, x, y, z := redSetup(t)
	w5.SetBlock(x, y, z, pistonEast(false))
	w5.SetBlock(x+1, y, z, stone)
	w5.SetBlock(x+2, y, z, chest)
	w5.SetBlock(x, y, z-1, power)
	h5.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h5, players5, 4)
	if boolProp(w5.At(x, y, z), "extended") || w5.At(x+1, y, z) != stone {
		t.Fatal("a chest must stop the piston")
	}
}

// A sticky piston pulling a slime block brings the block stuck to it too, and
// a mob standing where a block arrives is carried along.
func TestStickyPullsSlimeChainAndShovesMobs(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	stone := uint32(worldgen.Stone)
	lever := setBoolProp(uint32(worldgen.BlockBase("lever")+9), "powered", false)
	w.SetBlock(x, y, z, pistonEast(true))
	w.SetBlock(x+1, y, z, slimeMin)
	w.SetBlock(x+1, y+1, z, stone)
	w.SetBlock(x, y, z-1, lever)
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 4)
	if w.At(x+2, y, z) != slimeMin || w.At(x+2, y+1, z) != stone {
		t.Fatalf("extend should push slime and its rider: %d %d", w.At(x+2, y, z), w.At(x+2, y+1, z))
	}
	// A mob standing in the cell the slime is about to return to gets shoved west.
	m := h.spawnHostileY(players, entityZombie, float64(x+1)+0.5, float64(y), float64(z)+0.5)
	if m == nil {
		t.Fatal("no zombie")
	}
	m.x, m.y, m.z = float64(x+1)+0.5, float64(y), float64(z)+0.5
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 4)
	if w.At(x+1, y, z) != slimeMin || w.At(x+1, y+1, z) != stone || w.At(x+2, y, z) != worldgen.Air {
		t.Fatalf("retract should pull slime and its rider: %d %d %d", w.At(x+1, y, z), w.At(x+1, y+1, z), w.At(x+2, y, z))
	}
	if int(m.x) != x {
		t.Fatalf("the zombie should be shoved to x=%d, at %.1f", x, m.x)
	}
}
