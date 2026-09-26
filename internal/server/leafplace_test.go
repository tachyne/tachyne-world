package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bug #43: leaves a player places must persist. LeavesBlock.getStateForPlacement
// sets PERSISTENT, and a persistent leaf never decays. (The test server's
// player is in creative, as the reporters were.)
func TestPlacedHedgeLeafStaysPersistent(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 7, 180, 7
	w.SetBlock(x, y, z, worldgen.Stone)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["flowering_azalea_leaves"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if !pollUntil(3*time.Second, func() bool { return isAnyLeaf(w.At(x, y+1, z)) }) {
		t.Fatal("the leaf was not placed")
	}
	time.Sleep(300 * time.Millisecond) // let any neighbour/shape update run
	if _, d, persistent, _ := leafInfo(w.At(x, y+1, z)); !persistent {
		t.Errorf("a placed leaf must be persistent: distance %d, state %d", d, w.At(x, y+1, z))
	}
}
