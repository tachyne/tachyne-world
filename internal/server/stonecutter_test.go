package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// The stonecutter flow: open, insert stone, pick a recipe row, take results
// until the input runs out, selection resets on input change.
func TestStonecutterFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)

	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		stone := int32(itemByName["stone"])
		list := stonecutIndex[stone]
		if len(list) == 0 {
			t.Error("stone must have stonecutting recipes")
			return
		}

		h.openStonecutter(tr)
		if tr.winID == 0 || tr.winKind != winStonecut || tr.stoneSel != -1 {
			t.Errorf("open: id=%d kind=%d sel=%d", tr.winID, tr.winKind, tr.stoneSel)
			return
		}

		// No selection → no result.
		tr.anvil[0] = invStack{item: stone, count: 2}
		if res := h.stonecutResult(tr); res.item != 0 {
			t.Errorf("result before selection: %+v", res)
		}

		// Out-of-range button is refused; a valid one selects.
		h.stonecutSelect(tr, int32(len(list)))
		if tr.stoneSel != -1 {
			t.Errorf("out-of-range selected %d", tr.stoneSel)
		}
		h.stonecutSelect(tr, 0)
		res := h.stonecutResult(tr)
		if tr.stoneSel != 0 || res.item != list[0].Out || res.count != int(list[0].Count) {
			t.Errorf("select 0: sel=%d res=%+v want %+v", tr.stoneSel, res, list[0])
		}

		// Take twice: each consumes one input; the second empties it and
		// resets the selection.
		h.takeStonecutResult(h.playersRef, tr)
		if tr.cursor.item != list[0].Out || tr.anvil[0].count != 1 || tr.stoneSel != 0 {
			t.Errorf("first take: cursor=%+v input=%+v sel=%d", tr.cursor, tr.anvil[0], tr.stoneSel)
		}
		h.takeStonecutResult(h.playersRef, tr)
		if tr.anvil[0].item != 0 || tr.stoneSel != -1 {
			t.Errorf("second take: input=%+v sel=%d", tr.anvil[0], tr.stoneSel)
		}
		if want := 2 * int(list[0].Count); tr.cursor.count != want {
			t.Errorf("cursor count %d, want %d", tr.cursor.count, want)
		}

		// The engine's index preserves the shared table's global order.
		seen := 0
		for _, r := range protocol.StonecuttingRecipes {
			if r.In == stone {
				if list[seen] != r {
					t.Errorf("order diverged at %d: %+v vs %+v", seen, list[seen], r)
					break
				}
				seen++
			}
		}
	})
}
