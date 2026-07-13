package server

import "testing"

// The cartography table's three operations end-to-end on the hub: zoom out
// mints a blank map one scale up, lock mints a frozen pixel-copy, clone
// shares the source id — and each take consumes one of each input.
func TestCartographyTable(t *testing.T) {
	_, h, p := breakPlaceServer(t)

	onHub(t, h, func() {
		h.maps = newMapStore("")
		tr := h.playersRef[p.eid]
		h.openCartography(tr)
		if tr.winID == 0 || tr.winKind != winCarto {
			t.Errorf("window not open: id=%d kind=%d", tr.winID, tr.winKind)
			return
		}

		src := h.maps.create(100, 200, 0, 0)
		src.Colors[5] = 42

		// Zoom out: map + paper → a fresh BLANK map at scale 1.
		tr.anvil[0] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
		tr.anvil[1] = invStack{item: itemPaper, count: 2}
		h.takeTwoSlotResult(h.playersRef, tr)
		if tr.cursor.item != itemFilledMap || tr.cursor.count != 1 {
			t.Errorf("zoom cursor: %+v", tr.cursor)
		}
		zoomed := h.maps.get(tr.cursor.mapID)
		if zoomed == nil || zoomed.ID == src.ID || zoomed.Scale != 1 || zoomed.Locked || zoomed.Colors[5] != 0 {
			t.Errorf("zoomed map: %+v", zoomed)
		}
		if tr.anvil[0].item != 0 || tr.anvil[1].count != 1 {
			t.Errorf("zoom consumption: %+v %+v", tr.anvil[0], tr.anvil[1])
		}

		// Lock: map + glass pane → a frozen copy under a new id, colors kept.
		tr.cursor = invStack{}
		tr.anvil[0] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
		tr.anvil[1] = invStack{item: itemGlassPane, count: 1}
		h.takeTwoSlotResult(h.playersRef, tr)
		locked := h.maps.get(tr.cursor.mapID)
		if locked == nil || locked.ID == src.ID || !locked.Locked ||
			locked.Scale != src.Scale || locked.Colors[5] != 42 {
			t.Errorf("locked map: %+v", locked)
		}
		if h.maps.get(src.ID).Locked {
			t.Error("source must stay unlocked")
		}

		// A locked map refuses paper and pane…
		tr.cursor = invStack{}
		tr.anvil[0] = invStack{item: itemFilledMap, count: 1, mapID: locked.ID}
		tr.anvil[1] = invStack{item: itemPaper, count: 1}
		if res, _ := h.twoSlotResult(tr); res.item != 0 {
			t.Error("locked map must not zoom")
		}
		tr.anvil[1] = invStack{item: itemGlassPane, count: 1}
		if res, _ := h.twoSlotResult(tr); res.item != 0 {
			t.Error("locked map must not re-lock")
		}

		// …but still clones (both copies share the locked map's id).
		tr.anvil[1] = invStack{item: itemEmptyMap, count: 1}
		h.takeTwoSlotResult(h.playersRef, tr)
		if tr.cursor.count != 2 || tr.cursor.mapID != locked.ID {
			t.Errorf("clone cursor: %+v", tr.cursor)
		}
		if tr.anvil[0].item != 0 || tr.anvil[1].item != 0 {
			t.Errorf("clone consumption: %+v %+v", tr.anvil[0], tr.anvil[1])
		}

		// The scale cap: a scale-4 map + paper computes no result.
		capped := h.maps.create(0, 0, mapMaxScale, 0)
		tr.anvil[0] = invStack{item: itemFilledMap, count: 1, mapID: capped.ID}
		tr.anvil[1] = invStack{item: itemPaper, count: 1}
		if res, _ := h.twoSlotResult(tr); res.item != 0 {
			t.Error("scale-4 map must not zoom")
		}

		// Junk inputs never produce a result.
		tr.anvil[0] = invStack{item: int32(itemByName["stone"]), count: 1}
		tr.anvil[1] = invStack{item: itemPaper, count: 1}
		if res, _ := h.twoSlotResult(tr); res.item != 0 {
			t.Error("stone is not a map")
		}
		tr.anvil[0] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
		tr.anvil[1] = invStack{item: int32(itemByName["stone"]), count: 1}
		if res, _ := h.twoSlotResult(tr); res.item != 0 {
			t.Error("stone is not a modifier")
		}
	})
}
