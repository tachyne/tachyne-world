package server

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/tachyne/tachyne-common/protocol"
)

// Books, on the map model: the stack carries only a book id; pages and the
// signing metadata live in the hub's bookStore (books.json) and are composed
// into the content component at send time. edit_book updates the store;
// signing turns the writable book into a written one (author = the player,
// generation 0); using a written book opens the client's reader.

var (
	itemWritableBook = int32(itemByName["writable_book"])
	itemWrittenBook  = int32(itemByName["written_book"])
)

const (
	componentWritableBook = 45 // canonical ids; the chain renumbers per version
	componentWrittenBook  = 46

	bookMaxPages   = 100
	bookMaxPageLen = 1024
	bookMaxTitle   = 32
)

// savedBook is one book's contents. Title == "" means still writable.
type savedBook struct {
	Title  string   `json:"title,omitempty"`
	Author string   `json:"author,omitempty"`
	Gen    int      `json:"gen,omitempty"`
	Pages  []string `json:"pages,omitempty"`
}

type bookStore struct {
	mu     sync.Mutex
	path   string
	LastID int32                `json:"last_id"`
	Books  map[string]savedBook `json:"books"`
	dirty  bool
}

func newBookStore(path string) *bookStore {
	s := &bookStore{path: path, Books: map[string]savedBook{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, s)
		}
	}
	if s.Books == nil {
		s.Books = map[string]savedBook{}
	}
	return s
}

func bookKey(id int32) string { return strconv.FormatInt(int64(id), 10) }

// globalBooks lets the free stackComponents compose book contents without
// threading the hub through fifty call sites. One live hub per process; the
// last-constructed hub's store wins (tests run hubs in parallel but only
// book tests read book contents, on their own hub).
var globalBooks atomic.Pointer[bookStore]

func (h *hub) initBooks(bs *bookStore) {
	h.books = bs
	globalBooks.Store(bs)
}

func (s *bookStore) get(id int32) (savedBook, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.Books[bookKey(id)]
	return b, ok
}

func (s *bookStore) put(id int32, b savedBook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Books[bookKey(id)] = b
	s.dirty = true
}

// create mints the next id (ids start at 1 so 0 means "no book").
func (s *bookStore) create(b savedBook) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastID++
	s.Books[bookKey(s.LastID)] = b
	s.dirty = true
	return s.LastID
}

func (s *bookStore) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil && os.Rename(tmp, s.path) == nil {
		s.dirty = false
	}
}

type evEditBook struct {
	eid      int32
	slot     int32
	pages    []string
	title    string
	hasTitle bool
}

func (evEditBook) isHubEvent() {}

// onEditBook applies a page save or a signing to the held writable book.
func (h *hub) onEditBook(t *tracked, e evEditBook) {
	if t.inv == nil || len(e.pages) > bookMaxPages {
		return
	}
	var ptr *invStack
	switch {
	case e.slot >= 0 && e.slot <= 8:
		ptr = &t.inv.slots[e.slot]
	case e.slot == 40:
		ptr = &t.offhand
	default:
		return
	}
	if ptr.item != itemWritableBook || ptr.count <= 0 {
		return
	}
	pages := make([]string, 0, len(e.pages))
	for _, p := range e.pages {
		if len(p) > bookMaxPageLen {
			p = p[:bookMaxPageLen]
		}
		pages = append(pages, p)
	}
	b := savedBook{Pages: pages}
	if e.hasTitle { // signing: the writable book becomes a one-of-a-kind written one
		title := e.title
		if len(title) > bookMaxTitle {
			title = title[:bookMaxTitle]
		}
		b.Title, b.Author, b.Gen = title, t.p.name, 0
		ptr.item = itemWrittenBook // the client shows the component's title
	}
	if ptr.bookID == 0 {
		ptr.bookID = h.books.create(b)
	} else {
		h.books.put(ptr.bookID, b)
	}
	if e.slot == 40 {
		h.sendInventory(t)
	} else {
		h.sendSlot(t, int(e.slot))
	}
}

func bookComponentBytes(item int32, b savedBook) []byte {
	var out []byte
	if item == itemWrittenBook {
		out = protocol.AppendVarInt(out, componentWrittenBook)
		out = protocol.AppendString(out, b.Title)
		out = append(out, 0) // no filtered title
		out = protocol.AppendString(out, b.Author)
		out = protocol.AppendVarInt(out, int32(b.Gen))
		out = protocol.AppendVarInt(out, int32(len(b.Pages)))
		for _, p := range b.Pages {
			out = append(out, chatNBT(p)...)
			out = append(out, 0) // no filtered page
		}
		out = append(out, 1) // resolved
		return out
	}
	out = protocol.AppendVarInt(out, componentWritableBook)
	out = protocol.AppendVarInt(out, int32(len(b.Pages)))
	for _, p := range b.Pages {
		out = protocol.AppendString(out, p)
		out = append(out, 0) // no filtered page
	}
	return out
}
