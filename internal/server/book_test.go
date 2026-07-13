package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

func TestBookEditSignAndComponent(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		globalBooks.Store(h.books) // parallel test hubs race on the global; pin ours
		tr := h.playersRef[p.eid]
		tr.inv.slots[0] = invStack{item: itemWritableBook, count: 1}

		// Page save creates a book id and stores the pages.
		h.onEditBook(tr, evEditBook{eid: p.eid, slot: 0, pages: []string{"hello", "world"}})
		st := tr.inv.slots[0]
		if st.bookID == 0 || st.item != itemWritableBook {
			t.Errorf("after edit: %+v", st)
			return
		}
		b, ok := h.books.get(st.bookID)
		if !ok || len(b.Pages) != 2 || b.Pages[0] != "hello" || b.Title != "" {
			t.Errorf("stored: %+v %v", b, ok)
		}

		// The component composes as writable content (id 45 canonical).
		comp := stackComponents(st)
		br := bytes.NewReader(comp)
		if n, _ := protocol.ReadVarInt(br); n != 1 {
			t.Errorf("component count %d", n)
			return
		}
		protocol.ReadVarInt(br) // removed = 0
		if cid, _ := protocol.ReadVarInt(br); cid != componentWritableBook {
			t.Errorf("component id %d", cid)
			return
		}
		if n, _ := protocol.ReadVarInt(br); n != 2 {
			t.Errorf("page count %d", n)
			return
		}

		// Signing converts to a written book, same id, author = player.
		h.onEditBook(tr, evEditBook{eid: p.eid, slot: 0, pages: []string{"hello", "world"}, title: "My Tale", hasTitle: true})
		st = tr.inv.slots[0]
		if st.item != itemWrittenBook {
			t.Errorf("after sign: %+v", st)
		}
		b, _ = h.books.get(st.bookID)
		if b.Title != "My Tale" || b.Author != tr.p.name || b.Gen != 0 {
			t.Errorf("signed: %+v", b)
		}
		// Written component id 46, resolved bit set at the tail.
		comp = stackComponents(st)
		if len(comp) == 0 || comp[len(comp)-1] != 1 {
			t.Errorf("written component tail: % x", comp)
		}

		// A signed book refuses further edits.
		h.onEditBook(tr, evEditBook{eid: p.eid, slot: 0, pages: []string{"tamper"}})
		b, _ = h.books.get(st.bookID)
		if b.Pages[0] != "hello" {
			t.Error("written book must not re-edit")
		}

		// Pack round trip carries the book id.
		if got := unpackStack(packStack(st)); got != st {
			t.Errorf("pack round trip: %+v vs %+v", got, st)
		}
	})
}
