package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDispenseMoreBehaviors covers the added dispense-table entries: wind
// charge, water-bottle→mud, glass-bottle fill, wither-skull placement, and
// armor-stand equipping.
func TestDispenseMoreBehaviors(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := eastDispenser(t)
	pos := blockPos{5, 70, 5}
	front := blockPos{6, 70, 5}

	load := func(st invStack) *invStack {
		w.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		h.bins[pos] = b
		return &b.slots[0]
	}

	onHub(t, h, func() {
		// Wind charge → a wind_charge projectile flies out, one consumed.
		h.arrows = map[int32]*arrowEntity{}
		s := load(invStack{item: itemWindCharge, count: 2})
		h.ejectFromBin(h.playersRef, pos, state)
		if len(h.arrows) != 1 {
			t.Fatalf("wind charge: %d projectiles, want 1", len(h.arrows))
		}
		if s.count != 1 {
			t.Errorf("wind charge count %d, want 1", s.count)
		}

		// Water bottle onto dirt → mud, bottle becomes glass.
		w.SetBlock(front.x, front.y, front.z, worldgen.Dirt)
		s = load(potionStack(potWater))
		h.ejectFromBin(h.playersRef, pos, state)
		if w.At(front.x, front.y, front.z) != worldgen.Mud {
			t.Errorf("dirt ahead should have become mud")
		}
		if s.item != itemGlassBottle {
			t.Errorf("water bottle should have emptied to glass, got item %d", s.item)
		}
		// A water bottle onto stone (not convertable) is tossed instead.
		w.SetBlock(front.x, front.y, front.z, worldgen.Stone)
		before := len(h.items)
		load(potionStack(potWater))
		h.ejectFromBin(h.playersRef, pos, state)
		if len(h.items) != before+1 {
			t.Errorf("non-mud target should toss the bottle: items %d want %d", len(h.items), before+1)
		}

		// Glass bottle facing water → a water bottle in the slot.
		w.SetBlock(front.x, front.y, front.z, worldgen.Water)
		s = load(invStack{item: itemGlassBottle, count: 1})
		h.ejectFromBin(h.playersRef, pos, state)
		if s.item != itemPotion || s.potion != potWater {
			t.Errorf("glass bottle should fill to a water bottle: %+v", *s)
		}
		if w.At(front.x, front.y, front.z) != worldgen.Water {
			t.Errorf("filling a bottle must not drain the water source")
		}

		// Wither skull into an empty cell → the skull block is placed.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		s = load(invStack{item: itemWitherSkull, count: 2})
		h.ejectFromBin(h.playersRef, pos, state)
		if w.At(front.x, front.y, front.z) != witherSkullBlock {
			t.Errorf("wither skull should have been placed ahead")
		}
		if s.count != 1 {
			t.Errorf("one skull should be consumed, left %d", s.count)
		}

		// Armor onto an armor stand in the cell ahead → equipped, one consumed.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		helmet := int32(itemByName["iron_helmet"])
		if standSlotFor(helmet) < 0 {
			t.Skip("iron_helmet not wearable in this build")
		}
		sd := &armorStand{eid: h.allocEID(), dim: 0,
			x: float64(front.x) + 0.5, y: float64(front.y), z: float64(front.z) + 0.5}
		h.armorStands[sd.eid] = sd
		s = load(invStack{item: helmet, count: 1})
		h.ejectFromBin(h.playersRef, pos, state)
		if sd.equip[standSlotFor(helmet)].item != helmet {
			t.Errorf("helmet should be equipped on the stand: %+v", sd.equip)
		}
		if s.item != 0 {
			t.Errorf("the equipped helmet should be consumed, left %+v", *s)
		}
	})
}
