package world

import (
	"encoding/gob"
	"errors"
	"os"
)

// Store persists the world's block edits across restarts. The whole world is
// procedural (a pure function of the seed), so only the diffs — blocks players
// placed or broke — need saving. This interface is deliberately tiny so the
// backing store can later swap from a file to Postgres without touching world
// logic: implement Load/Save against a DB and pass it to NewWithStore.
type Store interface {
	// Load returns all persisted edits (chunk -> local block index -> state).
	// A fresh world (no save yet) returns an empty map and nil error.
	Load() (map[chunkPos]map[int]uint32, error)
	// Save replaces the persisted edits with the given snapshot.
	Save(edits map[chunkPos]map[int]uint32) error
}

// persistedEdit is one flat on-disk record. A flat slice avoids gob's quirks
// with array map keys and is trivial to map to SQL rows later.
type persistedEdit struct {
	CX, CZ int32
	Idx    int32
	State  uint32
}

// FileStore saves edits to a single gob file. Simple and dependency-free; good
// enough until the edit count makes a real database worthwhile.
type FileStore struct{ path string }

// NewFileStore stores edits at path (e.g. "world.gob").
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// Load reads the edit file, treating a missing file as an empty world.
func (fs *FileStore) Load() (map[chunkPos]map[int]uint32, error) {
	f, err := os.Open(fs.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[chunkPos]map[int]uint32{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var recs []persistedEdit
	if err := gob.NewDecoder(f).Decode(&recs); err != nil {
		return nil, err
	}
	out := make(map[chunkPos]map[int]uint32, len(recs))
	for _, r := range recs {
		key := chunkPos{r.CX, r.CZ}
		m := out[key]
		if m == nil {
			m = make(map[int]uint32)
			out[key] = m
		}
		m[int(r.Idx)] = r.State
	}
	return out, nil
}

// Save writes the snapshot atomically (temp file + rename) so a crash mid-write
// can never corrupt the live save.
func (fs *FileStore) Save(edits map[chunkPos]map[int]uint32) error {
	var recs []persistedEdit
	for key, m := range edits {
		for idx, state := range m {
			recs = append(recs, persistedEdit{CX: key[0], CZ: key[1], Idx: int32(idx), State: state})
		}
	}

	tmp := fs.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(recs); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, fs.path)
}
