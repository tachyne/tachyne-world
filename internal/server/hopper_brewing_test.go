package server

import "testing"

// A hopper above a brewing stand feeds the ingredient slot only; one at the
// side fills empty bottle slots with bottles and the fuel slot with blaze
// powder, never the ingredient; one below draws bottles but never fuel.
func TestHopperFeedsBrewingStandByFace(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	wart := int32(itemByName["nether_wart"])
	cobble := int32(itemByName["cobblestone"])

	onHub(t, h, func() {
		standPos := blockPos{5, 71, 5}
		w.SetBlock(standPos.x, standPos.y, standPos.z, brewStandMin)
		stand := &bin{slots: make([]invStack, 5)}
		h.bins[simPos{blockPos: standPos}] = stand

		// From above: nether wart goes to slot 3; cobblestone goes nowhere.
		abovePos := blockPos{5, 72, 5}
		w.SetBlock(abovePos.x, abovePos.y, abovePos.z, hopperMin) // facing down
		above := &bin{slots: make([]invStack, 5)}
		above.slots[0] = invStack{item: cobble, count: 1}
		above.slots[1] = invStack{item: wart, count: 2}
		h.bins[simPos{blockPos: abovePos}] = above
		h.hopperPush(h.playersRef, simPos{blockPos: abovePos}, hopperMin, above)
		if stand.slots[3].item != wart || stand.slots[3].count != 1 {
			t.Fatalf("the wart should land in the ingredient slot: %+v", stand.slots)
		}
		if above.slots[0].count != 1 {
			t.Error("cobblestone must not enter a brewing stand")
		}

		// From the side: a water bottle fills bottle slot 0, blaze powder the
		// fuel slot, and nether wart is refused.
		sidePos := blockPos{6, 71, 5}
		side := hopperMin + 3 // facing west into the stand
		w.SetBlock(sidePos.x, sidePos.y, sidePos.z, side)
		sb := &bin{slots: make([]invStack, 5)}
		sb.slots[0] = invStack{item: wart, count: 1}
		sb.slots[1] = potionStack(potWater)
		sb.slots[2] = invStack{item: itemBlazePowder, count: 1}
		h.bins[simPos{blockPos: sidePos}] = sb
		h.hopperPush(h.playersRef, simPos{blockPos: sidePos}, side, sb)
		h.hopperPush(h.playersRef, simPos{blockPos: sidePos}, side, sb)
		h.hopperPush(h.playersRef, simPos{blockPos: sidePos}, side, sb)
		if stand.slots[0].item != itemPotion || stand.slots[4].item != itemBlazePowder {
			t.Fatalf("side hopper should fill a bottle slot and the fuel: %+v", stand.slots)
		}
		if sb.slots[0].count != 1 || stand.slots[3].count != 1 {
			t.Error("a side hopper must not feed the ingredient slot")
		}

		// From below: bottles come out, the blaze powder stays.
		belowPos := blockPos{5, 70, 5}
		w.SetBlock(belowPos.x, belowPos.y, belowPos.z, hopperMin)
		below := &bin{slots: make([]invStack, 5)}
		h.bins[simPos{blockPos: belowPos}] = below
		h.hopperPull(h.playersRef, simPos{blockPos: belowPos}, below)
		if below.slots[0].item != itemPotion {
			t.Fatalf("a hopper below should draw the bottle: %+v", below.slots)
		}
		stand.slots[0] = invStack{}
		h.hopperPull(h.playersRef, simPos{blockPos: belowPos}, below)
		if stand.slots[4].item != itemBlazePowder {
			t.Error("a hopper below must never take the fuel")
		}
		if stand.slots[3].count != 1 {
			t.Error("a hopper below must not take the wart (only a glass bottle leaves slot 3)")
		}
	})
}
