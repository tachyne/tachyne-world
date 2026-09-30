package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MovingPistonBlock.useWithoutItem: a click on a moving cell whose block
// entity is there PASSes, so the held block is placed against it; only an
// orphaned cell (no block entity) takes the click and is cleared.
func TestClickOnALiveMovingCellPasses(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1360, 180, 1360
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y, z, movingPistonState([3]int{1, 0, 0}, false))
	key := simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}
	onHub(t, h, func() {
		h.putMoving(key, movingBlock{moved: worldgen.Stone, facing: [3]int{1, 0, 0}, extending: true, due: h.tick.Load() + 100000})
	})
	selectSlot(p, 0)
	p.setHotbarSlot(0, itemByName["stone"])

	if s.tryUseBlock(p, false, x, y, z, 0, 1, 0.5, 1, 0.5) {
		t.Fatal("a click on a live moving cell was taken; vanilla PASSes it to the held item")
	}
	s.handlePlace(p, placeBody(x, y, z, 1)) // the top face
	if got := w.Block(x, y+1, z); got != worldgen.Stone {
		t.Fatalf("the held stone was not placed against the live moving cell: %s", describeState(got))
	}

	w.SetBlock(x, y+1, z, worldgen.Air)
	onHub(t, h, func() { h.dropMoving(key) })
	if !s.tryUseBlock(p, false, x, y, z, 0, 1, 0.5, 1, 0.5) {
		t.Fatal("a click on an orphaned moving cell should be taken (it clears the cell)")
	}
}

// The sessions' mirror follows the hub's moving cells: set while a pushed
// block slides, gone once it lands.
func TestMovingCellMirrorFollowsThePush(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	cell := blockPos{x + 2, y, z}
	live := false
	for i := 0; i < 6 && !live; i++ {
		stepTicks(h, players, 1)
		live = isMovingPiston(w.At(cell.x, cell.y, cell.z)) && h.movingCellLive(dimOverworld, cell)
	}
	if !live {
		t.Fatalf("the pushed stone's moving cell was never mirrored live (cell %s)", describeState(w.At(cell.x, cell.y, cell.z)))
	}
	stepTicks(h, players, 6)
	if w.At(cell.x, cell.y, cell.z) != worldgen.Stone {
		t.Fatalf("the stone did not land: %s", describeState(w.At(cell.x, cell.y, cell.z)))
	}
	if h.movingCellLive(dimOverworld, cell) {
		t.Fatal("the landed cell is still mirrored as moving")
	}
}
