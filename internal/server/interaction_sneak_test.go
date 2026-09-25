package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ServerPlayerGameMode.useItemOn: sneaking with an item in hand places it
// against a usable block instead of using the block (bug #26); sneaking
// empty-handed still uses the block.
func TestSneakPlacesAgainstUsableBlocks(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	trap := worldgen.BlockBase("oak_trapdoor")
	w.SetBlock(x, y, z, trap)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.sneaking = true

	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if w.Block(x, y, z) != trap {
		t.Error("a sneaking player holding stone operated the trapdoor")
	}
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("a sneaking player holding stone did not place it against the trapdoor")
	}

	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if w.Block(x, y, z) == trap {
		t.Error("sneaking empty-handed did not operate the trapdoor, as vanilla's does")
	}
}
