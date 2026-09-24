package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestShulkerBoxOpeningTripsObserverAndLifts: a shulker box's lid sends
// neighbour and shape updates as it starts and stops (ShulkerBoxBlockEntity.
// doNeighborUpdates), so an observer watching the box pulses when it is
// opened; and the rising lid pushes a mob standing on it upward.
func TestShulkerBoxOpeningTripsObserverAndLifts(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	box := worldgen.BlockBase("shulker_box")
	info, _ := worldgen.InfoForState(box)
	box = worldgen.SetProperty(info, box, "facing", "up")
	w.SetBlock(x, y, z, box)
	w.SetBlock(x+1, y, z, withProps(t, observerMin, map[string]string{"facing": "west", "powered": "false"}))
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 8) // the observer takes its first look
	sheep := h.spawnMob(players, entitySheep, float64(x)+0.5, float64(y+1), float64(z)+0.5)
	sheep.statik = true
	startY := sheep.y

	opener := survPlayer(h)
	players[opener.p.eid] = opener
	opener.winKind, opener.winPos = winChest, simPos{dim: 0, blockPos: blockPos{x, y, z}}
	h.lidEvent(players, opener.winPos)

	pulsed := false
	for i := 0; i < 14 && !pulsed; i++ {
		stepTicks(h, players, 1)
		h.tickShulkerLids(players)
		pulsed = boolProp(w.At(x+1, y, z), "powered")
	}
	if !pulsed {
		t.Fatal("an observer watching a shulker box should pulse when it opens")
	}
	if sheep.y <= startY {
		t.Fatalf("the opening lid should lift the sheep on top: %.2f → %.2f", startY, sheep.y)
	}
}
