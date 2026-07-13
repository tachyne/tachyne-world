package server

import "testing"

func TestLecternAndShelf(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		globalBooks.Store(h.books)
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		w := h.world
		bx, bz := int(tr.x)+2, int(tr.z)
		by := int(tr.y)

		// Lectern: place a signed book, page-turn, take it back.
		w.SetBlock(bx, by, bz, lecternMin+1) // facing north, has_book=false
		id := h.books.create(savedBook{Title: "T", Author: "a", Pages: []string{"1", "2", "3"}})
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemWrittenBook, count: 1, bookID: id}
		h.onUseLectern(h.playersRef, evUseLectern{eid: p.eid, x: bx, y: by, z: bz})
		lec := h.lecterns[blockPos{bx, by, bz}]
		if lec == nil || lec.book.bookID != id || tr.inv.slots[tr.p.heldSlot()].item != 0 {
			t.Errorf("place book: %+v", lec)
			return
		}
		if !boolProp(w.At(bx, by, bz), "has_book") {
			t.Error("has_book not set")
		}
		// Open + page buttons clamp to the page count.
		h.onUseLectern(h.playersRef, evUseLectern{eid: p.eid, x: bx, y: by, z: bz})
		if tr.winKind != winLectern {
			t.Errorf("kind %d", tr.winKind)
		}
		h.lecternButton(h.playersRef, tr, lecternButtonNext)
		h.lecternButton(h.playersRef, tr, lecternButtonNext)
		h.lecternButton(h.playersRef, tr, lecternButtonNext) // clamped at 2
		if lec.page != 2 {
			t.Errorf("page %d, want 2", lec.page)
		}
		h.lecternButton(h.playersRef, tr, lecternButtonJumpStart) // jump to 0
		if lec.page != 0 {
			t.Errorf("jump page %d", lec.page)
		}
		h.lecternButton(h.playersRef, tr, lecternButtonTake)
		if h.lecterns[blockPos{bx, by, bz}] != nil || boolProp(w.At(bx, by, bz), "has_book") {
			t.Error("take failed")
		}
		found := false
		for _, s := range tr.inv.slots {
			if s.item == itemWrittenBook && s.bookID == id {
				found = true
			}
		}
		if !found {
			t.Error("book not returned")
		}

		// Chiseled bookshelf: insert by clicked slot, occupancy state, take.
		sx := bx + 2
		w.SetBlock(sx, by, bz, bookshelfMin) // facing north
		state := w.At(sx, by, bz)
		// north face = 2; top-right looking at the face = small cx.
		if slot := shelfHitSlot(state, 2, 0.1, 0.8, 0.5); slot != 2 {
			t.Errorf("hit slot %d, want 2 (top right)", slot)
		}
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: int32(itemByName["book"]), count: 2}
		h.onUseShelf(h.playersRef, evUseShelf{eid: p.eid, x: sx, y: by, z: bz, face: 2, cx: 0.1, cy: 0.8, cz: 0.5})
		shelf := h.bookshelves[blockPos{sx, by, bz}]
		if shelf == nil || shelf[2].item == 0 || tr.inv.slots[tr.p.heldSlot()].count != 1 {
			t.Errorf("insert: %+v", shelf)
			return
		}
		if !boolProp(w.At(sx, by, bz), "slot_2_occupied") {
			t.Error("slot_2_occupied not set")
		}
		// Empty hand takes it back from the same spot.
		tr.inv.slots[tr.p.heldSlot()] = invStack{}
		h.onUseShelf(h.playersRef, evUseShelf{eid: p.eid, x: sx, y: by, z: bz, face: 2, cx: 0.1, cy: 0.8, cz: 0.5})
		if shelf[2].item != 0 || boolProp(w.At(sx, by, bz), "slot_2_occupied") {
			t.Error("take failed")
		}

		// Persistence round trips.
		cs := newContainerStore("")
		cs.recordLecterns(map[blockPos]*lectern{{1, 2, 3}: {book: invStack{item: itemWrittenBook, count: 1, bookID: 9}, page: 4}})
		ll := cs.loadLecterns()
		if l := ll[blockPos{1, 2, 3}]; l == nil || l.book.bookID != 9 || l.page != 4 {
			t.Errorf("lectern round trip: %+v", ll)
		}
		var sh [6]invStack
		sh[5] = invStack{item: int32(itemByName["book"]), count: 1}
		cs.recordShelves(map[blockPos]*[6]invStack{{4, 5, 6}: &sh})
		ss := cs.loadShelves()
		if s := ss[blockPos{4, 5, 6}]; s == nil || s[5].item != int32(itemByName["book"]) {
			t.Errorf("shelf round trip: %+v", ss)
		}
	})
}
