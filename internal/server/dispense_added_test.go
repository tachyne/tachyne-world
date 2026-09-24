package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDispenseAddedBehaviors covers the dispense-table entries added for full
// vanilla coverage: egg variants, spectral/tipped arrows, the powder-snow
// bucket, and armor-stand placement.
func TestDispenseAddedBehaviors(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := eastDispenser(t)
	pos := blockPos{5, 70, 5}
	front := blockPos{6, 70, 5}

	load := func(st invStack) *invStack {
		w.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		h.bins[simPos{blockPos: pos}] = b
		return &b.slots[0]
	}

	onHub(t, h, func() {
		// Blue and brown eggs throw as an egg projectile, just like a plain egg.
		for _, egg := range []int32{itemBlueEgg, itemBrownEgg} {
			h.arrows = map[int32]*arrowEntity{}
			load(invStack{item: egg, count: 1})
			h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
			if len(h.arrows) != 1 {
				t.Fatalf("egg %d: %d projectiles, want 1", egg, len(h.arrows))
			}
		}

		// A spectral arrow flies as itself and makes what it hits glow.
		h.arrows = map[int32]*arrowEntity{}
		load(invStack{item: itemSpectralArr, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.arrows) != 1 {
			t.Fatalf("spectral arrow: %d projectiles, want 1", len(h.arrows))
		}
		for _, a := range h.arrows {
			if a.tipped {
				t.Error("a spectral arrow should not be tipped")
			}
			if a.etype != entitySpectralArrow || a.glow != spectralGlowSecs {
				t.Errorf("a dispensed spectral arrow flew as %d with glow %d", a.etype, a.glow)
			}
		}

		// A tipped arrow flies and carries its potion's effects onto a hit.
		h.arrows = map[int32]*arrowEntity{}
		load(invStack{item: itemTippedArrow, count: 1, potion: potPoison})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.arrows) != 1 {
			t.Fatalf("tipped arrow: %d projectiles, want 1", len(h.arrows))
		}
		for _, a := range h.arrows {
			if !a.tipped || a.potion != potPoison {
				t.Errorf("tipped arrow should carry its potion: tipped=%v potion=%d", a.tipped, a.potion)
			}
		}

		// The powder-snow bucket pours a powder-snow block and empties to a bucket.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		s := load(invStack{item: itemPowderBucket, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if w.At(front.x, front.y, front.z) != powderSnowBlock {
			t.Errorf("powder-snow bucket should place powder snow ahead")
		}
		if s.item != itemBucket {
			t.Errorf("powder-snow bucket should empty to a bucket, got item %d", s.item)
		}

		// An armor stand item spawns a stand on the cell ahead and is consumed.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		before := len(h.armorStands)
		s = load(invStack{item: itemArmorStand, count: 1})
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		if len(h.armorStands) != before+1 {
			t.Errorf("armor stand should spawn a stand: %d want %d", len(h.armorStands), before+1)
		}
		if s.count != 0 {
			t.Errorf("armor stand should be consumed, count %d", s.count)
		}
	})
}

// DropperBlock: a potion a dropper pushes into a chest keeps its brew.
func TestDropperKeepsItemData(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	info, _ := worldgen.InfoForState(dropperMin)
	state := worldgen.SetProperty(info, dropperMin, "facing", "east")
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	onHub(t, h, func() {
		w.SetBlock(pos.x, pos.y, pos.z, state)
		w.SetBlock(front.x, front.y, front.z, worldgen.BlockID("chest"))
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: itemByName["potion"], count: 2, potion: potPoison}
		h.bins[simPos{blockPos: pos}] = b
		h.ejectFromBin(h.playersRef, simPos{blockPos: pos}, state)
		got := h.containerSlots(simPos{blockPos: front})
		if got == nil || got[0].item != itemByName["potion"] || got[0].potion != potPoison {
			t.Errorf("the chest received %+v, want a poison potion", got)
		}
	})
}
