package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// chestState builds a chest state with the given facing + type.
func chestState(t *testing.T, facing, ctype string) uint32 {
	t.Helper()
	base := worldgen.BlockBase("chest")
	info, ok := worldgen.InfoForState(base)
	if !ok {
		t.Fatal("chest has no block info")
	}
	if !info.HasProperty("type") || !info.HasProperty("facing") {
		t.Fatalf("chest missing type/facing props: %+v", info.Props)
	}
	s := worldgen.SetProperty(info, base, "facing", facing)
	return worldgen.SetProperty(info, s, "type", ctype)
}

func chestType(state uint32) string {
	_, ct := chestFacingType(state)
	return ct
}

func TestDoubleChestPairing(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := h.world

	onHub(t, h, func() {
		// An existing single chest facing north sits at (1,0,64); a new chest is
		// placed at (0,0,64) facing north. North's clockwise side is east, so the
		// existing chest is on the new one's clockwise side → new = LEFT, old = RIGHT.
		existing := chestState(t, "north", "single")
		w.SetBlock(1, 64, 0, existing)

		newState := chestState(t, "north", "single")
		placed := s.pairChestOnPlace(p, 0, 64, 0, newState)
		if chestType(placed) != "left" {
			t.Fatalf("placed chest type %q, want left", chestType(placed))
		}
		if got := chestType(w.At(1, 64, 0)); got != "right" {
			t.Fatalf("partner chest type %q, want right", got)
		}
		w.SetBlock(0, 64, 0, placed)

		// chestPairPositions resolves the ordered halves from either block.
		l, r, paired := h.chestPairPositions(0, 64, 0, placed)
		if !paired || l != (blockPos{0, 64, 0}) || r != (blockPos{1, 64, 0}) {
			t.Fatalf("pair from left: %v %v paired=%v", l, r, paired)
		}
		l, r, paired = h.chestPairPositions(1, 64, 0, w.At(1, 64, 0))
		if !paired || l != (blockPos{0, 64, 0}) || r != (blockPos{1, 64, 0}) {
			t.Fatalf("pair from right: %v %v paired=%v", l, r, paired)
		}

		// Breaking the right half reverts the left half to a single chest.
		h.setBlock(h.playersRef, blockPos{1, 64, 0}, worldgen.Air)
		h.spillContainer(h.playersRef, 1, 64, 0, worldgen.Air)
		if got := chestType(w.At(0, 64, 0)); got != "single" {
			t.Fatalf("survivor chest type %q, want single after partner broke", got)
		}
	})
}

func TestDoubleChestNoPairWrongSide(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		// A chest facing north placed IN FRONT of (north of) another north chest
		// is on the facing axis, not a connect side — no pairing.
		w.SetBlock(0, 64, -1, chestState(t, "north", "single"))
		placed := s.pairChestOnPlace(p, 0, 64, 0, chestState(t, "north", "single"))
		if chestType(placed) != "single" {
			t.Fatalf("front-to-back chests wrongly paired: %q", chestType(placed))
		}
		// A chest with a DIFFERENT facing does not pair either.
		w.SetBlock(3, 64, 0, chestState(t, "south", "single"))
		placed = s.pairChestOnPlace(p, 2, 64, 0, chestState(t, "north", "single"))
		if chestType(placed) != "single" {
			t.Fatalf("mismatched-facing chests wrongly paired: %q", chestType(placed))
		}
	})
}

// TestDoubleChestContentsAndBreak covers Wesley's two questions: existing
// contents survive pairing, and breaking one half drops only that half's items
// while the survivor keeps its items and reverts to a single chest.
func TestDoubleChestContentsAndBreak(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		left, right := blockPos{0, 64, 0}, blockPos{1, 64, 0}
		// Two SINGLE chests already placed with items (the pre-pairing world).
		w.SetBlock(left.x, left.y, left.z, chestState(t, "north", "single"))
		w.SetBlock(right.x, right.y, right.z, chestState(t, "north", "single"))
		lc, rc := &chest{}, &chest{}
		lc.slots[3] = invStack{item: 10, count: 7}  // left has items
		rc.slots[5] = invStack{item: 20, count: 12} // right has items
		h.chests[left], h.chests[right] = lc, rc

		// Opening self-heals them into a pair WITHOUT touching contents.
		l, r, ok := h.formChestPair(left.x, left.y, left.z, w.At(left.x, left.y, left.z))
		if !ok || l != left || r != right {
			t.Fatalf("formChestPair: %v %v ok=%v", l, r, ok)
		}
		if h.chests[left].slots[3].count != 7 || h.chests[right].slots[5].count != 12 {
			t.Fatal("pairing altered chest contents")
		}
		if chestType(w.At(left.x, left.y, left.z)) != "left" || chestType(w.At(right.x, right.y, right.z)) != "right" {
			t.Fatal("pairing did not set left/right types")
		}

		// Break the RIGHT half: its items spill (storage deleted), the LEFT half
		// keeps its items and reverts to a single chest.
		h.setBlock(h.playersRef, right, worldgen.Air)
		h.spillContainer(h.playersRef, right.x, right.y, right.z, worldgen.Air)
		if h.chests[right] != nil {
			t.Error("broken half's storage not cleared")
		}
		if h.chests[left] == nil || h.chests[left].slots[3].count != 7 {
			t.Error("survivor lost its contents")
		}
		if chestType(w.At(left.x, left.y, left.z)) != "single" {
			t.Error("survivor did not revert to single")
		}
	})
}

func TestDoubleChestWindowSlots(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		left, right := blockPos{0, 64, 0}, blockPos{1, 64, 0}
		lc, rc := &chest{}, &chest{}
		lc.slots[0] = invStack{item: 1, count: 5}  // top-left corner
		rc.slots[26] = invStack{item: 2, count: 9} // bottom-right corner
		h.chests[left], h.chests[right] = lc, rc
		tr.winID, tr.winPos, tr.winPos2, tr.winKind = 7, left, right, winDoubleChest

		// Slot 0 → left[0]; slot 53 → right[26]; slot 81 → hotbar 0.
		if ptr, _ := h.winSlotPtr(tr, 0); ptr == nil || ptr.item != 1 {
			t.Errorf("slot 0 not left[0]: %+v", ptr)
		}
		if ptr, _ := h.winSlotPtr(tr, 53); ptr == nil || ptr.item != 2 {
			t.Errorf("slot 53 not right[26]: %+v", ptr)
		}
		if ptr, hot := h.winSlotPtr(tr, 81); ptr != &tr.inv.slots[0] || hot != 0 {
			t.Errorf("slot 81 not hotbar 0 (hot=%d)", hot)
		}
		if ptr, _ := h.winSlotPtr(tr, 54); ptr != &tr.inv.slots[9] {
			t.Error("slot 54 not main inv start")
		}
	})
}
