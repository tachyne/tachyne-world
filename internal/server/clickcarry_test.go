package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// clickRig is a survival player with the player window open (window 0: slots
// 9-35 are the main inventory at the same index).
func clickRig(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	return h, players, pl
}

// click sends one declared click through the hub, as remote.go builds it from
// a WindowClick: slots and cursor as an item and a count only.
func click(h *hub, players map[int32]*tracked, pl *tracked, slot int16, mode int32, cursor invStack, changed ...slotChange) {
	for i := range changed {
		changed[i].st = invStack{item: changed[i].st.item, count: changed[i].st.count}
	}
	h.handleClick(players, evClick{eid: pl.p.eid, windowID: 0, slot: slot, mode: mode,
		cursor: invStack{item: cursor.item, count: cursor.count}, changed: changed})
}

var potionItem = itemByName["potion"]

// Picking a potion up and putting it down elsewhere keeps its type: the click
// declares only "potion ×1".
func TestClickCarriesPotionType(t *testing.T) {
	h, players, pl := clickRig(t)
	pl.inv.slots[9] = invStack{item: potionItem, count: 1, potion: 7}
	click(h, players, pl, 9, 0, invStack{item: potionItem, count: 1}, slotChange{slot: 9})
	click(h, players, pl, 20, 0, invStack{}, slotChange{slot: 20, st: invStack{item: potionItem, count: 1}})
	if got := pl.inv.slots[20]; got.item != potionItem || got.potion != 7 {
		t.Errorf("moved potion %+v, want type 7", got)
	}
}

// Swapping two different potions crosses their data, as the stacks crossed.
func TestClickSwapKeepsEachPotion(t *testing.T) {
	h, players, pl := clickRig(t)
	pl.inv.slots[9] = invStack{item: potionItem, count: 1, potion: 7}
	pl.inv.slots[10] = invStack{item: potionItem, count: 1, potion: 12}
	click(h, players, pl, 9, 0, invStack{item: potionItem, count: 1}, slotChange{slot: 9})
	click(h, players, pl, 10, 0, invStack{item: potionItem, count: 1}, slotChange{slot: 10, st: invStack{item: potionItem, count: 1}})
	if pl.inv.slots[10].potion != 7 || pl.cursor.potion != 12 {
		t.Errorf("after the swap slot 10 holds %d and the cursor %d, want 7 and 12", pl.inv.slots[10].potion, pl.cursor.potion)
	}
}

// A shift-clicked shulker box keeps its contents.
func TestClickShiftMoveKeepsShulkerContents(t *testing.T) {
	h, players, pl := clickRig(t)
	box := itemByName["shulker_box"]
	pl.inv.slots[9] = invStack{item: box, count: 1, boxID: 41}
	click(h, players, pl, 9, 1, invStack{}, slotChange{slot: 9}, slotChange{slot: 30, st: invStack{item: box, count: 1}})
	if got := pl.inv.slots[30]; got.item != box || got.boxID != 41 {
		t.Errorf("shift-moved shulker box %+v, want its contents (box 41)", got)
	}
}

// Splitting a stack of dyed items keeps the colour on both halves, and a
// merge back does not duplicate anything.
func TestClickSplitAndMergeKeepData(t *testing.T) {
	h, players, pl := clickRig(t)
	boots := itemByName["leather_boots"]
	pl.inv.slots[9] = invStack{item: boots, count: 1, color: 0x336699}
	stone := itemByName["stone"]
	pl.inv.slots[11] = invStack{item: stone, count: 4}
	click(h, players, pl, 11, 0, invStack{item: stone, count: 2}, slotChange{slot: 11, st: invStack{item: stone, count: 2}})
	click(h, players, pl, 11, 0, invStack{}, slotChange{slot: 11, st: invStack{item: stone, count: 4}})
	if pl.inv.slots[11].count != 4 || pl.cursor.item != 0 || len(h.items) != 0 {
		t.Errorf("split and merge: slot %+v cursor %+v, %d items dropped", pl.inv.slots[11], pl.cursor, len(h.items))
	}
	click(h, players, pl, 9, 0, invStack{item: boots, count: 1}, slotChange{slot: 9})
	click(h, players, pl, 12, 0, invStack{}, slotChange{slot: 12, st: invStack{item: boots, count: 1}})
	if pl.inv.slots[12].color != 0x336699 {
		t.Errorf("moved dyed boots lost their colour: %+v", pl.inv.slots[12])
	}
}

// A potion thrown out of the window (Q over the slot) drops as itself.
func TestClickThrowKeepsPotion(t *testing.T) {
	h, players, pl := clickRig(t)
	h.world.ForceLoad(0, 0, 1)
	pl.inv.slots[9] = invStack{item: potionItem, count: 1, potion: 7}
	click(h, players, pl, 9, 4, invStack{}, slotChange{slot: 9})
	var thrown *itemEntity
	for _, it := range h.items {
		thrown = it
	}
	if thrown == nil || thrown.item != potionItem || thrown.potion != 7 {
		t.Errorf("thrown potion %+v, want the type-7 potion", thrown)
	}
}

// A death scatters whole stacks: a potion stays a potion, a full shulker box
// keeps its contents (vanilla drops the ItemStacks themselves).
func TestDeathDropKeepsItemData(t *testing.T) {
	h, players, pl := clickRig(t)
	h.world.ForceLoad(0, 0, 1)
	box := itemByName["shulker_box"]
	pl.inv.slots[3] = invStack{item: potionItem, count: 1, potion: 7}
	pl.inv.slots[4] = invStack{item: box, count: 1, boxID: 41}
	h.dropInventory(players, pl)
	var potion, full bool
	for _, it := range h.items {
		potion = potion || (it.item == potionItem && it.potion == 7)
		full = full || (it.item == box && it.boxID == 41)
	}
	if !potion || !full {
		t.Errorf("death drops: potion kept %v, shulker contents kept %v", potion, full)
	}
}

// Q with no window open throws the held stack whole.
func TestTossHeldKeepsItemData(t *testing.T) {
	h, players, pl := clickRig(t)
	h.world.ForceLoad(0, 0, 1)
	pl.inv.slots[0] = invStack{item: potionItem, count: 1, potion: 9}
	h.tossHeld(players, pl, 0, false)
	var got *itemEntity
	for _, it := range h.items {
		got = it
	}
	if got == nil || got.potion != 9 {
		t.Errorf("Q-thrown potion %+v, want type 9", got)
	}
}
