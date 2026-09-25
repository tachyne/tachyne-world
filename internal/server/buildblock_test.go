package server

import (
	"math"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPlacementRefusedIntoAMob (bug #35): a block cannot go where it would
// overlap a mob (BlockItem.canPlace → isUnobstructed). A wall built through
// a sheep left the sheep standing inside the wall, drawn black.
func TestPlacementRefusedIntoAMob(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	y := 70
	for x := 1; x <= 8; x++ {
		for z := 1; z <= 5; z++ {
			w.SetBlock(x, y, z, worldgen.Stone)
			w.SetBlock(x, y+1, z, worldgen.Air)
			w.SetBlock(x, y+2, z, worldgen.Air)
		}
	}
	onHub(t, h, func() {
		m := h.spawnMob(h.playersRef, entitySheep, 3.2, float64(y+1), 3.5) // 0.9 wide: reaches into x=2 and x=3
		if m != nil {
			m.statik = true
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		found := false
		if b := h.bodies.Load(); b != nil {
			for _, bb := range *b {
				if math.Abs(bb.x-3.2) < 0.01 && bb.hw > 0.4 {
					found = true
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the hub never published the sheep's box")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, x := range []int{2, 3} { // both cells the sheep's box reaches
		s.handlePlace(p, placeBody(x, y, 3, 1))
		if got := w.Block(x, y+1, 3); got != worldgen.Air {
			t.Errorf("a block went into the sheep at x=%d: %d", x, got)
		}
	}
	s.handlePlace(p, placeBody(6, y, 3, 1)) // clear of it
	if got := w.Block(6, y+1, 3); got != worldgen.Stone {
		t.Fatalf("a block clear of the sheep should be placed: %d", got)
	}
}
