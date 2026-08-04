package server

import (
	"encoding/json"
	"os"
	"sync"
)

// spawnStore persists each player's claimed respawn point by name — like
// modeStore, plain JSON an admin can inspect.
//
// The record is [x, y, z, dim]. The dimension is not decoration: a bed only
// works in the overworld and a respawn anchor only in the Nether, so without it
// a Nether anchor's claim would be validated against the overworld block at the
// same coordinates and quietly thrown away. Files written before the dimension
// existed decode with a zero fourth element, which is the overworld — exactly
// where every bed those files recorded actually stood.
type spawnStore struct {
	mu   sync.Mutex
	path string
	m    map[string][4]int
}

func newSpawnStore(path string) *spawnStore {
	s := &spawnStore{path: path, m: map[string][4]int{}}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &s.m)
		}
	}
	return s
}

// get returns name's claimed respawn block and the dimension it stands in.
func (s *spawnStore) get(name string) (blockPos, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[name]
	return blockPos{p[0], p[1], p[2]}, p[3], ok
}

// set records name's respawn block and persists the table atomically.
func (s *spawnStore) set(name string, pos blockPos, dim int) {
	s.mu.Lock()
	s.m[name] = [4]int{pos.x, pos.y, pos.z, dim}
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
