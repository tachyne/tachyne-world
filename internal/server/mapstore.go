package server

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

// mapStore persists filled maps (vanilla's maps/<n>.dat + last_id) — one
// plain-JSON file an admin can inspect; the 128×128 color buffer marshals
// as base64. Owned by the hub goroutine at runtime (only load/flush touch
// the mutex — mirroring the other feature stores' shape).
type mapStore struct {
	mu     sync.Mutex
	path   string
	lastID int32
	maps   map[int32]*mapData
	dirty  bool
}

// savedMap is one map's JSON form.
type savedMap struct {
	CenterX int32  `json:"center_x"`
	CenterZ int32  `json:"center_z"`
	Scale   int8   `json:"scale"`
	Dim     int    `json:"dim"`
	Locked  bool   `json:"locked,omitempty"`
	Colors  []byte `json:"colors"` // base64 in JSON
}

type savedMaps struct {
	LastID int32               `json:"last_id"`
	Maps   map[string]savedMap `json:"maps"`
}

func newMapStore(path string) *mapStore {
	ms := &mapStore{path: path, maps: map[int32]*mapData{}}
	if path == "" {
		return ms
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ms
	}
	var sv savedMaps
	if json.Unmarshal(raw, &sv) != nil {
		return ms
	}
	ms.lastID = sv.LastID
	for key, m := range sv.Maps {
		id64, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			continue
		}
		md := &mapData{
			ID: int32(id64), CenterX: m.CenterX, CenterZ: m.CenterZ,
			Scale: m.Scale, Dim: m.Dim, Locked: m.Locked,
			holders: map[int32]*mapHolder{},
		}
		copy(md.Colors[:], m.Colors)
		ms.maps[md.ID] = md
	}
	return ms
}

// get returns a map by id (nil if unknown).
func (ms *mapStore) get(id int32) *mapData { return ms.maps[id] }

// create allocates the next map id, centred on (x, z) with vanilla grid
// snapping. Ids start at 1 so 0 can mean "no map" on stacks.
func (ms *mapStore) create(x, z int, scale int8, dim int) *mapData {
	ms.lastID++
	md := &mapData{
		ID: ms.lastID, CenterX: snapCenter(x, scale), CenterZ: snapCenter(z, scale),
		Scale: scale, Dim: dim, holders: map[int32]*mapHolder{},
	}
	ms.maps[md.ID] = md
	ms.dirty = true
	return md
}

// clone registers a locked/scaled derivative (cartography + zoom recipes).
func (ms *mapStore) derive(src *mapData, scale int8, locked bool) *mapData {
	ms.lastID++
	md := &mapData{
		ID: ms.lastID, CenterX: snapCenter(int(src.CenterX), scale), CenterZ: snapCenter(int(src.CenterZ), scale),
		Scale: scale, Dim: src.Dim, Locked: locked,
		holders: map[int32]*mapHolder{},
	}
	if locked {
		md.Colors = src.Colors
		md.CenterX, md.CenterZ = src.CenterX, src.CenterZ
		md.Scale = src.Scale
	}
	ms.maps[md.ID] = md
	ms.dirty = true
	return md
}

// markDirty records color/state changes for the next flush.
func (ms *mapStore) markDirty() { ms.dirty = true }

// flushIfDirty writes the store atomically when anything changed.
func (ms *mapStore) flushIfDirty() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if !ms.dirty || ms.path == "" {
		return
	}
	sv := savedMaps{LastID: ms.lastID, Maps: map[string]savedMap{}}
	for id, md := range ms.maps {
		sv.Maps[strconv.FormatInt(int64(id), 10)] = savedMap{
			CenterX: md.CenterX, CenterZ: md.CenterZ, Scale: md.Scale,
			Dim: md.Dim, Locked: md.Locked, Colors: md.Colors[:],
		}
	}
	raw, err := json.Marshal(&sv)
	if err != nil {
		return
	}
	tmp := ms.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil && os.Rename(tmp, ms.path) == nil {
		ms.dirty = false
	}
}
