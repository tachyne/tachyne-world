package server

import (
	"github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// The server's side of vanilla's menu rules. The client declares what it
// moved (the 1.17+ click packet carries its predicted slot changes); the
// engine already refuses what does not conserve items. These add
// Slot.mayPlace — which slots take what — and the ResultSlot quick-move.

// slotAccepts is Slot.mayPlace for the choosy slots: a result slot never
// takes a placement, a furnace's fuel slot wants fuel (or a bucket), the
// enchanting table's second slot lapis, a beacon its payment, the armour
// slots their piece, a horse its saddle and barding, a loom banners, dyes
// and patterns, a smithing table a template. Taking out is always allowed.
func (h *hub) slotAccepts(t *tracked, slot int16, st invStack) bool {
	if st.item == 0 || st.count == 0 {
		return true
	}
	switch t.winKind {
	case winFurnace:
		switch slot {
		case 1:
			return cookerFuelTicks(0, st.item) > 0 || st.item == itemBucket
		case 2:
			return false
		}
	case winEnchant:
		switch slot {
		case 0:
			return st.count == 1
		case 1:
			return st.item == itemLapisLazuli
		}
	case winBeacon:
		if slot == 0 {
			return beaconPayment[st.item] && st.count == 1
		}
	case winPlayer:
		if slot >= 5 && slot <= 8 {
			want := []int{attach.EquipHead, attach.EquipChest, attach.EquipLegs, attach.EquipFeet}[slot-5]
			return standSlotFor(st.item) == want && st.count == 1
		}
	case winHorse:
		switch slot {
		case 0:
			return st.item == itemSaddle && st.count == 1
		case 1:
			return (horseArmorItems[st.item] || carpetItems[st.item]) && st.count == 1
		}
	case winLoom:
		switch slot {
		case 0:
			return bannerItems[st.item]
		case 1:
			_, dye := dyeColorOf[st.item]
			return dye
		case 2:
			_, pattern := loomPatternItems[st.item]
			return pattern
		case 3:
			return false
		}
	case winSmith:
		switch slot {
		case 0:
			_, trim := protocol.SmithingTrimTemplate[st.item]
			return trim || st.item == protocol.SmithingUpgradeTemplate
		case 3:
			return false
		}
	case winAnvil, winGrind, winCarto, winTrade:
		if slot == 2 {
			return false
		}
	case winStonecut:
		if slot == 1 {
			return false
		}
	}
	return st.count <= stackCap(st.item)
}

// resultTake is what a take of a result slot does with the result: onto
// the cursor (merging a same stack), or on a shift-click into the
// inventory (ResultSlot's quick-move). false: nowhere to put it.
func (h *hub) resultTake(t *tracked, res invStack, mode int32) bool {
	if mode == 1 { // QUICK_MOVE
		if t.inv.roomFor(res) < res.count {
			return false
		}
		changed, _ := t.inv.addStack(res)
		for _, s := range changed {
			h.sendSlot(t, s)
		}
		return true
	}
	switch {
	case t.cursor.item == 0:
		t.cursor = res
	case t.cursor.item == res.item && t.cursor.dmg == res.dmg && t.cursor.ench == res.ench && t.cursor.name == res.name &&
		t.cursor.mapID == res.mapID && t.cursor.count+res.count <= stackCap(res.item):
		t.cursor.count += res.count
	default:
		return false
	}
	return true
}

// roomFor counts how many of a stack the inventory can still take.
func (inv *inventory) roomFor(st invStack) int {
	room := 0
	cap := stackCap(st.item)
	for i := range inv.slots {
		s := &inv.slots[i]
		switch {
		case s.item == 0 || s.count == 0:
			room += cap
		case s.item == st.item && s.dmg == st.dmg && s.ench == st.ench && s.name == st.name && s.mapID == st.mapID:
			room += cap - s.count
		}
	}
	return room
}

// canTakeResult is the check before a take consumes its inputs: a plain
// click needs an empty cursor (or a same stack with room), a shift-click
// needs inventory room for the whole result.
func (h *hub) canTakeResult(t *tracked, res invStack, mode int32) bool {
	if mode == 1 {
		return t.inv.roomFor(res) >= res.count
	}
	return t.cursor.item == 0 || (t.cursor.item == res.item && t.cursor.dmg == res.dmg && t.cursor.ench == res.ench &&
		t.cursor.name == res.name && t.cursor.mapID == res.mapID && t.cursor.count+res.count <= stackCap(res.item))
}
