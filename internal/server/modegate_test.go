package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Adventure and spectator players may not change the world: no digging, no
// placing. Creative and survival still can.
func TestAdventureAndSpectatorCannotBuild(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	stone := worldgen.BlockBase("stone")
	p.setHotbarSlot(0, int32(itemByName["stone"]))
	p.held = 0

	for _, mode := range []int{gmAdventure, gmSpectator} {
		s.modes.set(p.name, mode)

		// Digging: the block stays.
		x, y, z := 20+mode, 70, 20
		w.SetBlock(x, y, z, stone)
		s.handleDig(p, digBody(digStartBreak, x, y, z))
		s.handleDig(p, digBody(digFinishBreak, x, y, z))
		if got := w.Block(x, y, z); got != stone {
			t.Fatalf("mode %d broke a block: state %d", mode, got)
		}

		// Placing: the cell above stays empty.
		w.SetBlock(x, y+1, z, worldgen.Air)
		s.handlePlace(p, placeBody(x, y, z, 1)) // face 1 = top
		if got := w.Block(x, y+1, z); got != worldgen.Air {
			t.Fatalf("mode %d placed a block: state %d", mode, got)
		}
	}

	// Creative still works, so the gate is not simply refusing everyone.
	s.modes.set(p.name, gmCreative)
	x, y, z := 30, 70, 20
	w.SetBlock(x, y, z, stone)
	s.handleDig(p, digBody(digStartBreak, x, y, z))
	if got := w.Block(x, y, z); got != worldgen.Air {
		t.Fatalf("creative should still break: state %d", got)
	}
}
