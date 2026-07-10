package world

import (
	"os"
	"path/filepath"
)

// dirCache stores each generated chunk as one small RLE file in a directory —
// the zero-dependency, zero-config default backend. No temp+rename ceremony:
// a torn write fails decode and just regenerates (cache semantics).
type dirCache struct{ dir string }

// NewDirCache returns a directory-backed chunk cache (creating the directory),
// or nil if it can't be created — the caller runs cache-less.
func NewDirCache(dir string) ChunkCache {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	return &dirCache{dir: dir}
}

func (d *dirCache) path(key string) string { return filepath.Join(d.dir, key+".gc") }

func (d *dirCache) Get(key string) ([]byte, bool) {
	data, err := os.ReadFile(d.path(key))
	if err != nil {
		return nil, false
	}
	return data, true
}

func (d *dirCache) Put(key string, val []byte) {
	os.WriteFile(d.path(key), val, 0o644)
}
