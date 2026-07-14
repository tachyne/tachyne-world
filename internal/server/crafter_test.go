package server

import "testing"

func TestCrafter(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	oakLog := int32(itemByName["oak_log"])

	// A crafter facing east (east_up orientation, index 9), triggered=false.
	state := crafterMin + 24 + uint32(9*2) + 1

	onHub(t, h, func() {
		pos := blockPos{5, 70, 5}
		w.SetBlock(pos.x, pos.y, pos.z, state)
		c := &bin{slots: make([]invStack, 9)}
		c.slots[0] = invStack{item: oakLog, count: 2} // 1 log → planks (shapeless)
		h.bins[pos] = c

		// Confirm the recipe resolves before asserting the craft.
		grid := make([]invStack, 9)
		grid[0] = invStack{item: oakLog, count: 1}
		wantItem, wantCount := matchRecipe(grid, 3)
		if wantItem == 0 {
			t.Error("oak_log has no crafting result — pick another test recipe")
			return
		}

		before := len(h.items)
		h.crafterCraft(h.playersRef, pos, state)

		if c.slots[0].count != 1 {
			t.Errorf("ingredient count %d after craft, want 1 (one consumed)", c.slots[0].count)
		}
		// The result ejected east into open air → a new item entity appears.
		if len(h.items) != before+1 {
			t.Fatalf("items %d after craft, want %d (result ejected)", len(h.items), before+1)
		}
		found := false
		for _, it := range h.items {
			if it.item == wantItem && it.count == wantCount {
				found = true
			}
		}
		if !found {
			t.Errorf("no ejected stack of item %d ×%d", wantItem, wantCount)
		}

		// Rising redstone edge triggers a craft via updateCrafter.
		c.slots[0] = invStack{item: oakLog, count: 1}
		w.SetBlock(pos.x+1, pos.y+1, pos.z, redstoneBlock) // power source (diagonal-safe: use a neighbour)
		w.SetBlock(pos.x, pos.y+1, pos.z, redstoneBlock)   // directly above → powers the crafter
		h.updateCrafter(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if crafterTriggered(w.At(pos.x, pos.y, pos.z)) != true {
			t.Error("crafter did not latch triggered on a rising edge")
		}
		if c.slots[0].count != 0 {
			t.Errorf("edge-triggered craft left %d ingredient, want 0", c.slots[0].count)
		}
	})
}
