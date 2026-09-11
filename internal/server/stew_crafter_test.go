package server

import "testing"

// TestCrafterKeepsSuspiciousStew: a crafter that assembles a suspicious stew
// ejects it with its flower, and a tossed one keeps it too.
func TestCrafterKeepsSuspiciousStew(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := crafterMin + 24 + uint32(9*2) + 1 // facing east, untriggered
	onHub(t, h, func() {
		pos := blockPos{5, 70, 5}
		w.SetBlock(pos.x, pos.y, pos.z, state)
		c := &bin{slots: make([]invStack, 9)}
		c.slots[0] = invStack{item: itemByName["bowl"], count: 1}
		c.slots[1] = invStack{item: itemByName["red_mushroom"], count: 1}
		c.slots[2] = invStack{item: itemByName["brown_mushroom"], count: 1}
		c.slots[3] = invStack{item: itemByName["dandelion"], count: 1}
		h.bins[simPos{blockPos: pos}] = c
		res := crafterResult(c)
		if res.item != itemSuspiciousStew || res.stew == 0 {
			t.Fatalf("the preview should be a suspicious stew with a flower: %+v", res)
		}
		h.crafterCraft(h.playersRef, simPos{blockPos: pos}, state)
		found := false
		for _, it := range h.items {
			if it.item == itemSuspiciousStew {
				found = true
				if it.stew != res.stew || it.stack().stew != res.stew {
					t.Fatalf("the ejected stew lost its flower: entity %d stack %d want %d", it.stew, it.stack().stew, res.stew)
				}
			}
		}
		if !found {
			t.Fatal("no stew ejected")
		}
		// A player tossing one keeps it as well.
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		pl.x, pl.y, pl.z = 20.5, 70, 20.5
		h.tossItem(players, pl, invStack{item: itemSuspiciousStew, count: 1, stew: res.stew})
		for _, it := range h.items {
			if it.thrower == pl.p.eid && it.stew != res.stew {
				t.Fatalf("the tossed stew lost its flower: %d", it.stew)
			}
		}
	})
}
