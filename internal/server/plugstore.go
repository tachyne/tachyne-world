package server

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// plugStore is a plugin's persistent KV store: a flat JSON object at
// plugins/<name>/data.json, flushed with the hub's other persistence (the
// 600-tick block + evSaveState) whenever dirty. Mutex-guarded like the other
// stores, though plugins only touch it from the hub goroutine.
type plugStore struct {
	path  string
	mu    sync.Mutex
	data  map[string]json.RawMessage
	dirty bool
}

func newPlugStore(path string) *plugStore {
	st := &plugStore{path: path, data: map[string]json.RawMessage{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &st.data); err != nil {
			log.Printf("plugin store %s unreadable (%v) — starting empty", path, err)
			st.data = map[string]json.RawMessage{}
		}
	}
	return st
}

func (st *plugStore) Get(key string, v any) bool {
	st.mu.Lock()
	raw, ok := st.data[key]
	st.mu.Unlock()
	if !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func (st *plugStore) Set(key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		log.Printf("plugin store %s: unmarshalable value for %q: %v", st.path, key, err)
		return
	}
	st.mu.Lock()
	st.data[key] = raw
	st.dirty = true
	st.mu.Unlock()
}

func (st *plugStore) Delete(key string) {
	st.mu.Lock()
	if _, ok := st.data[key]; ok {
		delete(st.data, key)
		st.dirty = true
	}
	st.mu.Unlock()
}

func (st *plugStore) flushIfDirty() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.dirty {
		return
	}
	raw, err := json.MarshalIndent(st.data, "", "  ")
	if err != nil {
		log.Printf("plugin store %s: marshal: %v", st.path, err)
		return
	}
	if err := os.WriteFile(st.path, raw, 0o644); err != nil {
		log.Printf("plugin store %s: write: %v", st.path, err)
		return
	}
	st.dirty = false
}
