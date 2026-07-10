package server

import (
	"encoding/json"
	"os"
	"sync"
)

// spawnStore persists each player's claimed respawn point (their bed) by name
// — like modeStore, plain JSON an admin can inspect.
type spawnStore struct {
	mu   sync.Mutex
	path string
	m    map[string][3]int
}

func newSpawnStore(path string) *spawnStore {
	s := &spawnStore{path: path, m: map[string][3]int{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// get returns name's claimed respawn block, if any.
func (s *spawnStore) get(name string) (blockPos, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[name]
	return blockPos{p[0], p[1], p[2]}, ok
}

// set records name's respawn block and persists the table atomically.
func (s *spawnStore) set(name string, pos blockPos) {
	s.mu.Lock()
	s.m[name] = [3]int{pos.x, pos.y, pos.z}
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path != "" {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			os.Rename(tmp, path)
		}
	}
}
