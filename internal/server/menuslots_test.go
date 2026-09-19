package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Slot.mayPlace on the server: fuel slots take fuel, lapis slots lapis,
// result slots nothing, armour slots their piece, a loom its banner, dye
// and pattern; and a placement may not exceed the item's stack cap.
func TestSlotRules(t *testing.T) {
	h := newHub(world.New(1))
	tr := testTracked()
	one := func(name string) invStack { return invStack{item: itemByName[name], count: 1} }

	tr.winKind = winFurnace
	if h.slotAccepts(tr, 1, one("stone")) || !h.slotAccepts(tr, 1, one("coal")) || !h.slotAccepts(tr, 1, one("bucket")) {
		t.Error("a furnace's fuel slot takes fuel (and a bucket), not stone")
	}
	if h.slotAccepts(tr, 2, one("iron_ingot")) || !h.slotAccepts(tr, 0, one("stone")) {
		t.Error("the result slot takes nothing; the input anything")
	}
	if !h.slotAccepts(tr, 2, invStack{}) {
		t.Error("taking out of a result slot is always allowed")
	}
	tr.winKind = winEnchant
	if h.slotAccepts(tr, 1, one("diamond")) || !h.slotAccepts(tr, 1, one("lapis_lazuli")) || h.slotAccepts(tr, 0, invStack{item: itemByName["diamond_sword"], count: 2}) {
		t.Error("enchanting: lapis in the lapis slot, one item in the item slot")
	}
	tr.winKind = winBeacon
	if h.slotAccepts(tr, 0, one("stone")) || !h.slotAccepts(tr, 0, one("emerald")) || h.slotAccepts(tr, 0, invStack{item: itemByName["emerald"], count: 2}) {
		t.Error("a beacon takes one payment item")
	}
	tr.winKind = winPlayer
	if !h.slotAccepts(tr, 5, one("iron_helmet")) || h.slotAccepts(tr, 6, one("iron_helmet")) || !h.slotAccepts(tr, 8, one("leather_boots")) || h.slotAccepts(tr, 7, one("stone")) {
		t.Error("armour slots take their piece only")
	}
	tr.winKind = winLoom
	if !h.slotAccepts(tr, 0, one("white_banner")) || h.slotAccepts(tr, 0, one("stone")) || !h.slotAccepts(tr, 1, one("red_dye")) || h.slotAccepts(tr, 1, one("stone")) || !h.slotAccepts(tr, 2, one("creeper_banner_pattern")) || h.slotAccepts(tr, 3, one("white_banner")) {
		t.Error("loom: banner, dye, pattern; nothing into the result")
	}
	tr.winKind = winChest
	if h.slotAccepts(tr, 0, invStack{item: itemByName["egg"], count: 17}) || !h.slotAccepts(tr, 0, invStack{item: itemByName["egg"], count: 16}) {
		t.Error("a placement may not exceed the item's stack cap (eggs 16)")
	}
}

// A hopper stacks eggs to 16 and tools to 1, never a flat 64.
func TestContainersUsePerItemCaps(t *testing.T) {
	slots := make([]invStack, 5)
	left := binInsert(slots, invStack{item: itemByName["egg"], count: 20})
	if left != 0 || slots[0].count != 16 || slots[1].count != 4 {
		t.Fatalf("20 eggs into a hopper: 16 + 4, got %+v left %d", slots[:2], left)
	}
	tools := make([]invStack, 5)
	if left := binInsert(tools, invStack{item: itemByName["iron_pickaxe"], count: 1}); left != 0 || tools[0].count != 1 {
		t.Fatalf("a tool sits alone in its slot: %+v left %d", tools[0], left)
	}
	if left := binInsert(tools, invStack{item: itemByName["iron_pickaxe"], count: 1}); left != 0 || tools[1].count != 1 {
		t.Fatalf("a second tool takes the next slot, never stacking: %+v left %d", tools[:2], left)
	}
}

// Shift-clicking a stonecutter's result moves the results into the
// inventory and repeats while the input lasts; a plain click takes one to
// the cursor.
func TestStonecutterQuickMove(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		stone := int32(itemByName["stone"])
		list := stonecutIndex[stone]
		if len(list) == 0 {
			t.Fatal("stone must have stonecutting recipes")
		}
		h.openStonecutter(tr)
		tr.anvil[0] = invStack{item: stone, count: 3}
		h.stonecutSelect(tr, 0)
		h.takeStonecutResult(h.playersRef, tr, 0)
		if tr.cursor.item != list[0].Out || tr.anvil[0].count != 2 {
			t.Fatalf("a plain click takes one result to the cursor: cursor %+v input %+v", tr.cursor, tr.anvil[0])
		}
		tr.cursor = invStack{}
		h.takeStonecutResult(h.playersRef, tr, 1)
		if tr.anvil[0].item != 0 || tr.cursor.item != 0 {
			t.Fatalf("a shift-click uses the whole input and leaves the cursor empty: input %+v cursor %+v", tr.anvil[0], tr.cursor)
		}
		got := 0
		for _, s := range tr.inv.slots {
			if s.item == list[0].Out {
				got += s.count
			}
		}
		if got != 2*int(list[0].Count) {
			t.Fatalf("two results should be in the inventory, got %d", got)
		}
	})
}
