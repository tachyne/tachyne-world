package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestCrafterInsertTargeting — a hopper filling a crafter routes each item to
// the emptiest enabled slot (vanilla canPlaceItem/smallerStackExist) and never
// into a disabled slot.
func TestCrafterInsertTargeting(t *testing.T) {
	item := int32(itemByName["oak_planks"])
	c := &bin{slots: make([]invStack, 9)}
	// An empty enabled slot always wins over partially-filled ones: slots 0,1
	// hold items, slot 2 is empty → slot 2.
	c.slots[0] = invStack{item: item, count: 2}
	c.slots[1] = invStack{item: item, count: 1}
	if got := crafterInsertTarget(c, item); got != 2 {
		t.Fatalf("empty enabled slot should win: target %d, want 2", got)
	}
	// With EVERY empty slot disabled, the fewest-filled enabled slot (1) wins,
	// ties breaking to the lower index.
	for i := 2; i < 9; i++ {
		c.disabled[i] = true
	}
	if got := crafterInsertTarget(c, item); got != 1 {
		t.Fatalf("all empties disabled → fewest-filled slot 1 should win: got %d", got)
	}
	// A full different-item slot is skipped; a wrong-item slot too.
	c2 := &bin{slots: make([]invStack, 9)}
	c2.disabled[0] = true
	c2.slots[1] = invStack{item: int32(itemByName["stone"]), count: 1} // wrong item
	if got := crafterInsertTarget(c2, item); got != 2 {
		t.Fatalf("skip disabled(0) + wrong-item(1) → slot 2: got %d", got)
	}
	// crafterInsert places into the winning slots and reports overflow.
	c3 := &bin{slots: make([]invStack, 9)}
	for i := 0; i < 9; i++ {
		c3.disabled[i] = true
	}
	c3.disabled[4] = false // only slot 4 is enabled
	if left := crafterInsert(c3, invStack{item: item, count: 3}); left != 0 || c3.slots[4].count != 3 {
		t.Fatalf("crafterInsert into the one enabled slot: left=%d slot4=%d", left, c3.slots[4].count)
	}
	// Fill the single enabled slot to the cap; the rest overflows.
	c3.slots[4].count = stackMax
	if left := crafterInsert(c3, invStack{item: item, count: 5}); left != 5 {
		t.Fatalf("no room in the one enabled slot: left=%d, want 5", left)
	}
}

// TestCrafterComparator — the comparator reads the number of slots that are
// filled OR disabled (0-9), not the fullness average.
func TestCrafterComparator(t *testing.T) {
	c := &bin{slots: make([]invStack, 9)}
	if got := crafterComparator(c); got != 0 {
		t.Fatalf("empty crafter signal %d, want 0", got)
	}
	c.slots[0] = invStack{item: 1, count: 1}
	c.slots[1] = invStack{item: 1, count: 64}
	c.disabled[2] = true
	c.disabled[3] = true
	if got := crafterComparator(c); got != 4 { // 2 filled + 2 disabled
		t.Fatalf("signal %d, want 4 (2 filled + 2 disabled)", got)
	}
}

// TestCrafterSlotToggle — onSlotState disables an EMPTY grid slot but refuses a
// filled one (vanilla only toggles empty slots).
func TestCrafterSlotToggle(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{2, 70, 2}
	c := &bin{slots: make([]invStack, 9)}
	c.slots[5] = invStack{item: int32(itemByName["stone"]), count: 1}
	h.bins[pos] = c

	pl := testTracked()
	pl.winKind, pl.winPos, pl.winID = winCrafter, pos, 3
	players := map[int32]*tracked{pl.p.eid: pl}

	// Disable an empty slot (state=false = disable).
	h.onSlotState(players, evSlotState{eid: pl.p.eid, slot: 0, enable: false})
	if !c.disabled[0] {
		t.Fatal("empty slot 0 should have been disabled")
	}
	// Re-enable it.
	h.onSlotState(players, evSlotState{eid: pl.p.eid, slot: 0, enable: true})
	if c.disabled[0] {
		t.Fatal("slot 0 should have been re-enabled")
	}
	// A filled slot cannot be disabled.
	h.onSlotState(players, evSlotState{eid: pl.p.eid, slot: 5, enable: false})
	if c.disabled[5] {
		t.Fatal("a filled slot must not be toggleable")
	}
	// Out-of-range slot is ignored.
	h.onSlotState(players, evSlotState{eid: pl.p.eid, slot: 9, enable: false})
}

// TestCrafterResultPreview — the preview slot reflects the grid's recipe.
func TestCrafterResultPreview(t *testing.T) {
	c := &bin{slots: make([]invStack, 9)}
	if crafterResult(c).item != 0 {
		t.Fatal("empty grid must have no result")
	}
	c.slots[0] = invStack{item: int32(itemByName["oak_log"]), count: 1}
	if crafterResult(c).item == 0 {
		t.Fatal("a log in the grid should preview planks")
	}
}

// TestCrafterDisabledPersists — the disabled mask survives a store round-trip.
func TestCrafterDisabledPersists(t *testing.T) {
	pos := blockPos{7, 64, -3}
	c := &bin{slots: make([]invStack, 9)}
	c.slots[0] = invStack{item: 5, count: 3}
	c.disabled[1] = true
	c.disabled[8] = true

	st := newContainerStore(t.TempDir() + "/containers.json")
	st.recordBins(map[blockPos]*bin{pos: c})
	got := st.loadBins()[pos]
	if got == nil {
		t.Fatal("bin not persisted")
	}
	if !got.disabled[1] || !got.disabled[8] || got.disabled[0] || got.disabled[2] {
		t.Fatalf("disabled mask not preserved: %v", got.disabled)
	}
	if got.slots[0].item != 5 || got.slots[0].count != 3 {
		t.Fatalf("slot contents not preserved: %+v", got.slots[0])
	}
}
