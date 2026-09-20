package server

import (
	"sync"
	"sync/atomic"

	"github.com/tachyne/tachyne-common/protocol"
)

// boxStore holds the contents a broken shulker box carries on its item, keyed
// by the boxID stamped on the stack. The hub is the only writer, but the
// CONTENTS have to be readable from stackComponents — a free function with no
// hub to ask — so this is guarded and reachable through a global, exactly as
// bundleStore is for bundles and bookStore for written books.
//
// Contents are copied in and out. A stowed box is a snapshot, not a live
// container: the chest it came from is gone by then, and the one it is
// restored into is new.
type boxStore struct {
	mu     sync.Mutex
	items  map[int32]chest
	lastID int32
}

func newBoxStore() *boxStore { return &boxStore{items: map[int32]chest{}} }

func (s *boxStore) get(id int32) (chest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.items[id]
	return c, ok
}

func (s *boxStore) set(id int32, c chest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = c
}

// take is get + delete: a box being placed back down retires its id.
func (s *boxStore) take(id int32) (chest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.items[id]
	delete(s.items, id)
	return c, ok
}

func (s *boxStore) mint() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastID++
	return s.lastID
}

func (s *boxStore) lastMinted() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastID
}

// snapshot is what the persistence layer writes: a copy, so the save can walk
// it without holding the lock.
func (s *boxStore) snapshot() map[int32]*chest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int32]*chest, len(s.items))
	for id, c := range s.items {
		cp := c
		out[id] = &cp
	}
	return out
}

func (s *boxStore) restore(items map[int32]*chest, last int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[int32]chest, len(items))
	for id, c := range items {
		if c != nil {
			s.items[id] = *c
		}
	}
	s.lastID = last
}

// globalBoxes lets stackComponents reach a stack's stowed contents.
var globalBoxes atomic.Pointer[boxStore]

func (h *hub) initBoxes(bs *boxStore) {
	h.boxes = bs
	globalBoxes.Store(bs)
}

// boxComponentBytes is the container payload for a stowed box: a plain list of
// Slots, empties included, because the list is positional
// (ItemContainerContents' stream codec is a list of optional stacks).
func boxComponentBytes(id int32) []byte {
	bs := globalBoxes.Load()
	if bs == nil {
		return nil
	}
	c, ok := bs.get(id)
	if !ok {
		return nil
	}
	b := protocol.AppendVarInt(nil, int32(len(c.slots)))
	for _, st := range c.slots {
		b = appendStack(b, st)
	}
	return b
}
