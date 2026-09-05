package server

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// Custom item names — the anvil rename — live in a side table, interned by
// id, for the same reason books, shulker boxes, hives and bundles do: the
// persisted stack is a fixed row of int32 (stackRow), and a string cannot ride
// it. The stack keeps its name in memory for the wire; only the SAVE goes
// through the id.
//
// Names are immutable strings, so interning needs no refcount: an id maps to
// one string forever, and the same string always yields the same id. The table
// grows with the number of DISTINCT names ever used, which is small.
type nameStore struct {
	mu     sync.Mutex
	byID   map[int32]string
	byName map[string]int32
	lastID int32
}

func newNameStore() *nameStore {
	return &nameStore{byID: map[int32]string{}, byName: map[string]int32{}}
}

// intern returns the id for name, minting one on first sight. "" is 0.
func (n *nameStore) intern(name string) int32 {
	if name == "" {
		return 0
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if id, ok := n.byName[name]; ok {
		return id
	}
	n.lastID++
	n.byID[n.lastID] = name
	n.byName[name] = n.lastID
	return n.lastID
}

// get returns the name for id, or "" for 0 or an unknown id.
func (n *nameStore) get(id int32) string {
	if id == 0 {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.byID[id]
}

// globalNames lets packStack/unpackStack — free functions used by every
// store — reach the table, exactly as globalBooks and globalBundles do.
// Set once at hub construction and replaced by the loaded table at boot,
// BEFORE any store decodes a stack.
var globalNames atomic.Pointer[nameStore]

func init() { globalNames.Store(newNameStore()) }

// recordNames snapshots the table into the save file.
func (s *containerStore) recordNames(n *nameStore) {
	if n == nil {
		return
	}
	n.mu.Lock()
	snap := make(map[string]string, len(n.byID))
	for id, name := range n.byID {
		snap[strconv.Itoa(int(id))] = name
	}
	last := n.lastID
	n.mu.Unlock()
	s.mu.Lock()
	s.m.Names, s.m.NextNameID = snap, last
	s.mu.Unlock()
}

// loadNames rebuilds the table from the save file.
func (s *containerStore) loadNames() *nameStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := newNameStore()
	for k, name := range s.m.Names {
		id, err := strconv.Atoi(k)
		if err != nil || name == "" {
			continue
		}
		out.byID[int32(id)] = name
		out.byName[name] = int32(id)
	}
	out.lastID = s.m.NextNameID
	return out
}
