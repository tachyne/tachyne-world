package server

import "testing"

// TestHopperUnderFurnacePullsOutputOnly: a hopper below a furnace must draw
// only the output slot, never the smelting input or the fuel.
func TestHopperUnderFurnacePullsOutputOnly(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	coal := int32(itemByName["coal"])
	ingot := int32(itemByName["iron_ingot"])
	rawIron := int32(itemByName["raw_iron"])

	onHub(t, h, func() {
		furnacePos := blockPos{5, 71, 5}
		hopperPos := blockPos{5, 70, 5}
		w.SetBlock(furnacePos.x, furnacePos.y, furnacePos.z, furnaceStateMin)
		f := &furnace{kind: cookFurnace}
		f.slots[furnaceInput] = invStack{item: rawIron, count: 8}
		f.slots[furnaceFuel] = invStack{item: coal, count: 8}
		f.slots[furnaceOutput] = invStack{item: ingot, count: 3}
		h.furnaces[furnacePos] = f

		hopper := hopperMin // enabled, facing down (into air → no push)
		w.SetBlock(hopperPos.x, hopperPos.y, hopperPos.z, hopper)
		h.updateHopper(h.playersRef, hopperPos, hopper)

		b := h.bins[hopperPos]
		if b == nil || b.slots[0].item != ingot || b.slots[0].count != 1 {
			t.Fatalf("hopper should have pulled 1 ingot, got %+v", b)
		}
		if f.slots[furnaceInput].count != 8 || f.slots[furnaceInput].item != rawIron {
			t.Error("hopper stole the smelting input")
		}
		if f.slots[furnaceFuel].count != 8 || f.slots[furnaceFuel].item != coal {
			t.Error("hopper stole the fuel")
		}
		if f.slots[furnaceOutput].count != 2 {
			t.Errorf("furnace output %d, want 2 (one pulled)", f.slots[furnaceOutput].count)
		}
	})
}

// TestHopperIntoFurnaceFuelOnlyFuel: a hopper feeding a furnace from the side
// puts only burnable items into the fuel slot.
func TestHopperIntoFurnaceFuelOnlyFuel(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	coal := int32(itemByName["coal"])
	cobble := int32(itemByName["cobblestone"])

	onHub(t, h, func() {
		furnacePos := blockPos{5, 71, 5}
		hopperPos := blockPos{6, 71, 5} // west of the furnace
		w.SetBlock(furnacePos.x, furnacePos.y, furnacePos.z, furnaceStateMin)
		h.furnaces[furnacePos] = &furnace{kind: cookFurnace}

		hopper := hopperMin + 3 // enabled, facing west (dx -1) into the furnace side
		w.SetBlock(hopperPos.x, hopperPos.y, hopperPos.z, hopper)
		b := &bin{slots: make([]invStack, 5)}
		b.slots[0] = invStack{item: cobble, count: 4} // non-fuel, first in line
		b.slots[1] = invStack{item: coal, count: 4}   // fuel
		h.bins[hopperPos] = b

		// One push cycle: cobblestone is rejected by the fuel slot, coal goes in.
		h.hopperPush(h.playersRef, hopperPos, hopper, b)
		f := h.furnaces[furnacePos]
		if f.slots[furnaceFuel].item != coal || f.slots[furnaceFuel].count != 1 {
			t.Fatalf("fuel slot %+v, want 1 coal", f.slots[furnaceFuel])
		}
		if b.slots[0].item != cobble || b.slots[0].count != 4 {
			t.Error("cobblestone was wrongly pushed into the fuel slot")
		}
	})
}
