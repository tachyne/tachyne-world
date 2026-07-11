package server

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// signStore persists sign text keyed by dimension + block position — plain
// JSON an admin can inspect, written atomically, mirroring containerStore.
// Unlike the hub-owned container maps, the store itself is the live owner
// (mutex-guarded): the attach layer's chunk-build workers read sign NBT for
// the chunk packet's block-entity section concurrently with hub writes.
type signStore struct {
	mu    sync.Mutex
	path  string
	m     map[string]signData
	dirty bool
}

// signSide mirrors vanilla SignText: four plain-text lines, dye color name
// ("" = black) and the glow-ink flag.
type signSide struct {
	Lines [4]string `json:"lines"`
	Color string    `json:"color,omitempty"`
	Glow  bool      `json:"glow,omitempty"`
}

// signData mirrors vanilla SignBlockEntity (minus the transient edit lock,
// which lives in hub.signMayEdit). Hanging picks the block-entity type the
// gateways render.
type signData struct {
	Front   signSide `json:"front"`
	Back    signSide `json:"back"`
	Waxed   bool     `json:"waxed,omitempty"`
	Hanging bool     `json:"hanging,omitempty"`
}

func (s signSide) hasMessage() bool {
	for _, l := range s.Lines {
		if l != "" {
			return true
		}
	}
	return false
}

func signKey(dim, x, y, z int) string { return fmt.Sprintf("%d:%d,%d,%d", dim, x, y, z) }

func newSignStore(path string) *signStore {
	s := &signStore{path: path, m: map[string]signData{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

func (s *signStore) get(dim, x, y, z int) (signData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[signKey(dim, x, y, z)]
	return d, ok
}

func (s *signStore) set(dim, x, y, z int, d signData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[signKey(dim, x, y, z)] = d
	s.dirty = true
}

// remove drops a sign's entry (block broken or overwritten); a no-op that
// doesn't dirty the file when there is nothing stored there.
func (s *signStore) remove(dim, x, y, z int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := signKey(dim, x, y, z)
	if _, ok := s.m[k]; ok {
		delete(s.m, k)
		s.dirty = true
	}
}

// flushIfDirty writes the store atomically when anything changed since the
// last flush. Called from the hub's 30s persistence cadence and on graceful
// shutdown.
func (s *signStore) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s.m, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if os.Rename(tmp, s.path) == nil {
		s.dirty = false
	}
}
