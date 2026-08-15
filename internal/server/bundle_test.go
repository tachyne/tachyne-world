package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Bundles: the weight rule, what fits, and the two clicks that fill and empty
// a pouch.

func bundleHub(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	return h, players
}

// BundleContents.getWeight: one item costs 1/maxStackSize of the pouch, so a
// full stack of anything fills it exactly, whatever that stack's size is.
func TestBundleWeightIsOneOverStackSize(t *testing.T) {
	h, _ := bundleHub(t)
	cases := []struct {
		name string
		item int32
		fits int // how many of it fill an empty bundle
	}{
		{"dirt (stacks to 64)", itemByName["dirt"], 64},
		{"ender pearls (16)", itemByName["ender_pearl"], 16},
		{"a water bucket (1)", itemByName["water_bucket"], 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.item == 0 {
				t.Skip("item not in this build")
			}
			got := h.bundleRoomFor(0, invStack{item: c.item, count: 1})
			if got != c.fits {
				t.Errorf("room for %d, want %d — weight should be 1/stackCap", got, c.fits)
			}
		})
	}
}

// A pouch takes a whole stack and then refuses the next item.
func TestBundleFillsToExactlyOneStack(t *testing.T) {
	h, _ := bundleHub(t)
	dirt := invStack{item: itemByName["dirt"], count: 64}
	n, id := h.bundleInsert(0, dirt)
	if n != 64 {
		t.Fatalf("took %d dirt, want all 64", n)
	}
	if got := h.bundleWeight(id); got != bundleCapacity {
		t.Errorf("weight %d, want a full %d", got, bundleCapacity)
	}
	if more, _ := h.bundleInsert(id, invStack{item: itemByName["stone"], count: 1}); more != 0 {
		t.Errorf("a full pouch took %d more items", more)
	}
}

// Mixed contents share the capacity: half a stack of dirt leaves room for
// exactly half a stack of anything else that stacks to 64.
func TestBundleSharesCapacityBetweenItems(t *testing.T) {
	h, _ := bundleHub(t)
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 32})
	n, _ := h.bundleInsert(id, invStack{item: itemByName["stone"], count: 64})
	if n != 32 {
		t.Errorf("took %d stone into a half-full pouch, want 32", n)
	}
}

// BUNDLE_IN_BUNDLE_WEIGHT: a nested pouch costs 1/16 on top of its contents,
// so pouches cannot be nested without limit.
func TestANestedBundleCostsExtra(t *testing.T) {
	h, _ := bundleHub(t)
	inner := invStack{item: itemByName["bundle"], count: 1}
	if w := h.itemWeight(inner); w != bundleInBundleWeight {
		t.Errorf("an empty nested pouch weighs %d, want %d (1/16)", w, bundleInBundleWeight)
	}
	// …and a nested pouch carrying something weighs that much more.
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 16})
	inner.bundleID = id
	want := bundleInBundleWeight + 16
	if w := h.itemWeight(inner); w != want {
		t.Errorf("a nested pouch with 16 dirt weighs %d, want %d", w, want)
	}
}

// Inserting the same item again merges, and the merged stack moves to the
// front so it is what comes back out first.
func TestInsertingMergesAndMovesToTheFront(t *testing.T) {
	h, _ := bundleHub(t)
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 8})
	h.bundleInsert(id, invStack{item: itemByName["stone"], count: 8})
	h.bundleInsert(id, invStack{item: itemByName["dirt"], count: 8})

	items := h.bundles.get(id)
	if len(items) != 2 {
		t.Fatalf("%d entries, want 2 (the dirt should have merged)", len(items))
	}
	if items[0].item != itemByName["dirt"] || items[0].count != 16 {
		t.Errorf("front entry %+v, want 16 merged dirt", items[0])
	}
}

// removeOne takes the front stack out and empties the store when the last one
// goes, so an emptied pouch leaves nothing behind.
func TestRemovingTheLastStackEmptiesTheStore(t *testing.T) {
	h, _ := bundleHub(t)
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 8})
	out, ok := h.bundleRemoveOne(id, -1)
	if !ok || out.count != 8 {
		t.Fatalf("removeOne = %+v, %v; want the 8 dirt back", out, ok)
	}
	if got := h.bundles.get(id); len(got) != 0 {
		t.Errorf("%d entries left after emptying", len(got))
	}
}

// Left-clicking a stack with a pouch on the cursor puts it in; the slot
// empties and the pouch gains an id.
func TestLeftClickPutsAStackInTheBundle(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.winID, tr.winKind = 0, winPlayer
	tr.inv.slots[9] = invStack{item: itemByName["dirt"], count: 32}
	tr.cursor = invStack{item: itemByName["bundle"], count: 1}

	if !h.tryBundleClick(players, tr, evClick{eid: tr.p.eid, slot: 9, mode: 0, button: 0}) {
		t.Fatal("a left click with a pouch on the cursor did nothing")
	}
	if tr.inv.slots[9].count != 0 {
		t.Errorf("slot still holds %d dirt", tr.inv.slots[9].count)
	}
	if tr.cursor.bundleID == 0 {
		t.Fatal("the pouch was not given an id")
	}
	items := h.bundles.get(tr.cursor.bundleID)
	if len(items) != 1 || items[0].count != 32 {
		t.Errorf("pouch holds %+v, want 32 dirt", items)
	}
}

// Right-clicking an empty slot takes one stack back out.
func TestRightClickTakesAStackOut(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.winID, tr.winKind = 0, winPlayer
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 12})
	tr.cursor = invStack{item: itemByName["bundle"], count: 1, bundleID: id}

	if !h.tryBundleClick(players, tr, evClick{eid: tr.p.eid, slot: 9, mode: 0, button: 1}) {
		t.Fatal("a right click on an empty slot took nothing out")
	}
	if got := tr.inv.slots[9]; got.item != itemByName["dirt"] || got.count != 12 {
		t.Errorf("slot got %+v, want the 12 dirt", got)
	}
}

// A pouch sitting in a slot takes what is on the cursor.
func TestClickingACursorStackOntoABundleInASlot(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.winID, tr.winKind = 0, winPlayer
	tr.inv.slots[9] = invStack{item: itemByName["bundle"], count: 1}
	tr.cursor = invStack{item: itemByName["dirt"], count: 20}

	if !h.tryBundleClick(players, tr, evClick{eid: tr.p.eid, slot: 9, mode: 0, button: 0}) {
		t.Fatal("clicking a cursor stack onto a pouch did nothing")
	}
	if tr.cursor.count != 0 {
		t.Errorf("cursor still holds %d", tr.cursor.count)
	}
	if items := h.bundles.get(tr.inv.slots[9].bundleID); len(items) != 1 || items[0].count != 20 {
		t.Errorf("pouch holds %+v, want 20 dirt", items)
	}
}

// Clicks that are not bundle actions must fall through to the ordinary
// reconciliation — this is the guard that keeps every other click working.
func TestNonBundleClicksFallThrough(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.winID, tr.winKind = 0, winPlayer
	tr.inv.slots[9] = invStack{item: itemByName["dirt"], count: 32}

	cases := []struct {
		name string
		set  func()
		ev   evClick
	}{
		{"no pouch anywhere", func() { tr.cursor = invStack{} }, evClick{slot: 9, mode: 0, button: 0}},
		{"a shift-click", func() { tr.cursor = invStack{item: itemByName["bundle"], count: 1} },
			evClick{slot: 9, mode: 1, button: 0}},
		{"a click outside the window", func() {}, evClick{slot: -999, mode: 0, button: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.set()
			c.ev.eid = tr.p.eid
			if h.tryBundleClick(players, tr, c.ev) {
				t.Error("a non-bundle click was swallowed by the bundle path")
			}
		})
	}
}

// The seventeen dyed bundles are all recognised, and nothing else is.
func TestEveryBundleVariantIsKnown(t *testing.T) {
	if len(bundleItems) != 17 {
		t.Errorf("%d bundles known, want 17", len(bundleItems))
	}
	for _, n := range []string{"bundle", "red_bundle", "light_gray_bundle"} {
		if id := itemByName[n]; id == 0 || !isBundle(id) {
			t.Errorf("%s is not recognised as a bundle", n)
		}
	}
	if isBundle(itemByName["chest"]) {
		t.Error("a chest is not a bundle")
	}
}

// Contents survive a save/load round trip: the pouch is an item that can be
// dropped or chested, so its contents cannot live only in memory.
func TestBundleContentsSurviveAReload(t *testing.T) {
	h, _ := bundleHub(t)
	dir := t.TempDir()
	h.containers = newContainerStore(dir + "/containers.json")
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 30})
	h.containers.recordBundles(h.bundles)

	back := h.containers.loadBundles()
	items := back.get(id)
	if len(items) != 1 || items[0].count != 30 || items[0].item != itemByName["dirt"] {
		t.Errorf("after reload the pouch holds %+v, want 30 dirt", items)
	}
	if back.lastID != h.bundles.lastID {
		t.Errorf("id counter %d, want %d — reusing an id would merge two pouches",
			back.lastID, h.bundles.lastID)
	}
}

// Scrolling a pouch picks which stack comes out next.
func TestSelectingAStackChangesWhatComesOut(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.winID, tr.winKind = 0, winPlayer

	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 4})
	h.bundleInsert(id, invStack{item: itemByName["stone"], count: 4})
	// stone went in last, so it sits at the front and is what comes out by default.
	tr.inv.slots[9] = invStack{item: itemByName["bundle"], count: 1, bundleID: id}

	h.selectBundleItem(tr, 9, 1) // pick the dirt behind it
	if got := h.bundles.sel(id); got != 1 {
		t.Fatalf("selection %d, want 1", got)
	}
	// Go through the real click, not bundleRemoveOne directly — the point is
	// that the CLICK honours the stored selection.
	tr.cursor = invStack{item: itemByName["bundle"], count: 1, bundleID: id}
	tr.inv.slots[9] = invStack{}
	if !h.tryBundleClick(players, tr, evClick{eid: tr.p.eid, slot: 9, mode: 0, button: 1}) {
		t.Fatal("right-clicking an empty slot took nothing out")
	}
	if got := tr.inv.slots[9]; got.item != itemByName["dirt"] {
		t.Errorf("took out %+v, want the SELECTED dirt rather than the front stack", got)
	}
	if got := h.bundles.sel(id); got != -1 {
		t.Errorf("selection %d after removing, want it cleared", got)
	}
}

// toggleSelectedItem: choosing what is already chosen clears the selection,
// and an out-of-range index is refused rather than remembered.
func TestSelectingTogglesAndRejectsOutOfRange(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	_, id := h.bundleInsert(0, invStack{item: itemByName["dirt"], count: 4})
	tr.inv.slots[9] = invStack{item: itemByName["bundle"], count: 1, bundleID: id}

	h.selectBundleItem(tr, 9, 0)
	if h.bundles.sel(id) != 0 {
		t.Fatal("index 0 was not selected")
	}
	h.selectBundleItem(tr, 9, 0) // same again
	if got := h.bundles.sel(id); got != -1 {
		t.Errorf("selection %d, want re-picking the same index to clear it", got)
	}
	h.selectBundleItem(tr, 9, 7) // nothing at 7
	if got := h.bundles.sel(id); got != -1 {
		t.Errorf("selection %d, want an out-of-range index refused", got)
	}
}

// Selecting on a slot that is not a bundle must do nothing at all.
func TestSelectingOnANonBundleSlotIsIgnored(t *testing.T) {
	h, players := bundleHub(t)
	tr := testTracked()
	players[tr.p.eid] = tr
	tr.inv.slots[9] = invStack{item: itemByName["dirt"], count: 1}
	h.selectBundleItem(tr, 9, 0) // must not panic or record anything
	h.selectBundleItem(tr, 999, 0)
	h.selectBundleItem(tr, -1, 0)
}
