package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Using armour in hand puts it on: an empty slot takes it and the hand
// empties; a worn piece comes back to the hand; a stack of two leaves one
// in hand and sends the old piece to the inventory; a bound piece stays;
// a plain item does nothing.
func TestEquipArmourOnUse(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.playersRef = players
	held := pl.p.heldSlot()
	iron := int32(itemByName["iron_chestplate"])
	diamond := int32(itemByName["diamond_chestplate"])
	pl.inv.slots[held] = invStack{item: iron, count: 1}
	h.onEquipHeld(players, evEquipHeld{eid: pl.p.eid})
	if pl.armor[1].item != iron || pl.inv.slots[held].item != 0 {
		t.Fatalf("iron chestplate: worn %+v held %+v", pl.armor[1], pl.inv.slots[held])
	}
	if pl.armorPoints() == 0 {
		t.Error("worn armour should count")
	}
	pl.inv.slots[held] = invStack{item: diamond, count: 1}
	h.onEquipHeld(players, evEquipHeld{eid: pl.p.eid})
	if pl.armor[1].item != diamond || pl.inv.slots[held].item != iron {
		t.Errorf("swap: worn %d held %d", pl.armor[1].item, pl.inv.slots[held].item)
	}
	pl.inv.slots[held] = invStack{item: iron, count: 2}
	h.onEquipHeld(players, evEquipHeld{eid: pl.p.eid})
	if pl.armor[1].item != iron || pl.inv.slots[held].count != 1 {
		t.Errorf("stack of two: worn %d held %+v", pl.armor[1].item, pl.inv.slots[held])
	}
	found := false
	for _, s := range pl.inv.slots {
		if s.item == diamond {
			found = true
		}
	}
	if !found {
		t.Error("the old chestplate should land in the inventory")
	}
	pl.armor[1] = withEnch(invStack{item: diamond, count: 1}, enchBindingCurse, 1)
	pl.inv.slots[held] = invStack{item: iron, count: 1}
	h.onEquipHeld(players, evEquipHeld{eid: pl.p.eid})
	if pl.armor[1].item != diamond {
		t.Error("a cursed piece stays on")
	}
	pl.inv.slots[held] = invStack{item: int32(itemByName["stick"]), count: 1}
	if equipSlotOnUse(pl.inv.slots[held].item) >= 0 {
		t.Error("a stick is not equippable")
	}
	if equipSlotOnUse(int32(itemElytra)) != 1 || equipSlotOnUse(int32(itemByName["leather_boots"])) != 3 {
		t.Error("elytra go on the chest, boots on the feet")
	}
	if equipSlotOnUse(itemCarvedPumpkin) >= 0 {
		t.Error("a carved pumpkin is not swappable on use")
	}
}

// The F key swaps the held item with the off-hand, stacks whole.
func TestSwapHands(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	sword := invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	shield := invStack{item: itemByName["shield"], count: 1, dmg: 7}
	pl.inv.slots[pl.p.heldSlot()] = sword
	pl.offhand = shield

	h.onSwapHands(players, evSwapHands{eid: pl.p.eid})
	if pl.inv.slots[pl.p.heldSlot()] != shield || pl.offhand != sword {
		t.Fatalf("the two should have changed places: held=%+v off=%+v", pl.inv.slots[pl.p.heldSlot()], pl.offhand)
	}
	// Swapping back restores the original, enchantments and damage intact.
	h.onSwapHands(players, evSwapHands{eid: pl.p.eid})
	if pl.inv.slots[pl.p.heldSlot()] != sword || pl.offhand != shield {
		t.Error("swapping twice should be a no-op")
	}
	// A spectator's hands are not swapped.
	pl.gamemode = gmSpectator
	h.onSwapHands(players, evSwapHands{eid: pl.p.eid})
	if pl.inv.slots[pl.p.heldSlot()] != sword {
		t.Error("a spectator swaps nothing")
	}
}
