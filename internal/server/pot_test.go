package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestFlowerPotGivesThePlantBack: FlowerPotBlock.useWithoutItem hands the
// plant to the player's inventory (a drop only when there is no room), and a
// filled pot clicked with another pottable plant keeps what it has.
func TestFlowerPotGivesThePlantBack(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 3, 70, 3
	poppy := pottedByItem[int32(itemByName["poppy"])]
	w.SetBlock(x, y, z, poppy)

	p.setHotbarSlot(0, itemByName["dandelion"]) // pottable: the pot keeps its poppy
	p.held = 0
	s.usePot(p, x, y, z, w.Block(x, y, z), 1)
	if w.Block(x, y, z) != poppy {
		t.Fatalf("a filled pot clicked with a pottable plant changed: %d", w.Block(x, y, z))
	}

	p.setHotbarSlot(0, 0) // empty hand: the poppy comes back
	s.usePot(p, x, y, z, w.Block(x, y, z), 2)
	if w.Block(x, y, z) != flowerPotState {
		t.Fatalf("the pot should be empty: %d", w.Block(x, y, z))
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := false
		onHub(t, h, func() {
			tr := h.playersRef[p.eid]
			for _, sl := range tr.inv.slots {
				if sl.item == int32(itemByName["poppy"]) && sl.count > 0 {
					got = true
				}
			}
		})
		if got {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the poppy never reached the inventory")
		}
		time.Sleep(20 * time.Millisecond)
	}
	onHub(t, h, func() {
		for _, it := range h.items {
			if it.item == int32(itemByName["poppy"]) {
				t.Error("the poppy was dropped although the inventory had room")
			}
		}
	})
	_ = worldgen.Air
}
