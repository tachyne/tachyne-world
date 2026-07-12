package anvil

import (
	"encoding/json"
	"os"
)

// ExportState remembers, per chunk, a content hash and the timestamp it was
// last written with, so repeated exports keep old region-header timestamps
// for unchanged chunks — renderers watching the save (BlueMap -u) then only
// re-render what actually changed. Keys are "<subdir>:<cx>,<cz>".
type ExportState struct {
	Hash  map[string]uint64 `json:"hash"`
	Stamp map[string]uint32 `json:"stamp"`
}

func NewExportState() *ExportState {
	return &ExportState{Hash: map[string]uint64{}, Stamp: map[string]uint32{}}
}

// LoadExportState reads a state file; a missing file is an empty state.
func LoadExportState(path string) (*ExportState, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewExportState(), nil
	}
	if err != nil {
		return nil, err
	}
	st := NewExportState()
	if err := json.Unmarshal(raw, st); err != nil {
		return NewExportState(), nil // corrupt state = full re-export, not a failure
	}
	return st, nil
}

// Save writes the state atomically.
func (st *ExportState) Save(path string) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// fnv1a hashes chunk NBT for change detection.
func fnv1a(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}
