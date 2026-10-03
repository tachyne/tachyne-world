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
		h.bins[simPos{blockPos: pos}] = c

		// Confirm the recipe resolves before asserting the craft.
		grid := make([]invStack, 9)
		grid[0] = invStack{item: oakLog, count: 1}
		wantItem, wantCount := matchRecipe(grid, 3)
		if wantItem == 0 {
			t.Error("oak_log has no crafting result — pick another test recipe")
			return
		}

		before := len(h.items)
		h.crafterCraft(h.playersRef, simPos{blockPos: pos}, state)

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
		h.updateCrafter(h.playersRef, simPos{blockPos: pos}, w.At(pos.x, pos.y, pos.z))
		if crafterTriggered(w.At(pos.x, pos.y, pos.z)) != true {
			t.Error("crafter did not latch triggered on a rising edge")
		}
		// CrafterBlock crafts on the tick it scheduled (4 later), not at the edge.
		if c.slots[0].count != 1 || !h.hasScheduledTick(pos) {
			t.Errorf("the craft should wait for the crafter's tick: ingredient %d, scheduled %v", c.slots[0].count, h.hasScheduledTick(pos))
		}
		h.redstoneTick(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if c.slots[0].count != 0 {
			t.Errorf("edge-triggered craft left %d ingredient, want 0", c.slots[0].count)
		}
	})
}

// CrafterBlockEntity.serverTick: a craft puts the arm out (CRAFTING) for
// craftingTicksRemaining = 6 — the block entity's tick that same game tick
// takes one, so the arm is back five ticks later — and a neighbour's update
// does not pull it in early.
func TestCrafterArmIsTheBlockEntityTicker(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pos := blockPos{x, y + 1, z}
	state := crafterMin + 24 + uint32(9*2) + 1 // east_up, not crafting
	h.setBlockAt(players, 0, pos, state)
	c := &bin{slots: make([]invStack, 9)}
	c.slots[0] = invStack{item: itemByName["oak_log"], count: 4}
	h.bins[simPos{blockPos: pos}] = c
	crafting := func() bool { return (w.At(pos.x, pos.y, pos.z)-crafterMin)/24 == 0 }

	h.crafterTick(players, simPos{blockPos: pos}, w.At(pos.x, pos.y, pos.z))
	if !crafting() {
		t.Fatal("the arm did not go out on the craft")
	}
	h.tickBlockEntityTickers(players) // the block entities' pass of the craft's own tick
	h.updateCrafter(players, simPos{blockPos: pos}, w.At(pos.x, pos.y, pos.z))
	for i := 1; i < crafterAnimTicks-1; i++ {
		stepTicks(h, players, 1)
		if !crafting() {
			t.Fatalf("the arm came in after %d ticks", i)
		}
	}
	stepTicks(h, players, 1)
	if crafting() {
		t.Fatalf("the arm is still out %d ticks after the craft", crafterAnimTicks-1)
	}
}
