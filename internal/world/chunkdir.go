package world

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
)

// dirCache stores each generated chunk as one small RLE file in a directory —
// the zero-dependency, zero-config default backend. No temp+rename ceremony:
// a torn write fails decode and just regenerates (cache semantics).
//
// It is BOUNDED. On the cluster the directory lives on a local-path volume,
// i.e. the node's root disk, and nothing else ever removed a file from it:
// with ten cows touring terrain for a day it held 6,368 chunks and would have
// kept growing for as long as anything explored. A sweep every dirSweepEvery
// puts removes the oldest files (by mtime) until the directory is back under
// dirCacheBudget.
type dirCache struct {
	dir      string
	budget   int64
	puts     atomic.Int64
	sweeping atomic.Bool
}

const (
	dirCacheBudget = 512 << 20 // bytes on disk; a chunk file is ~8 KiB, so ~64k chunks
	dirSweepEvery  = 256       // puts between sweeps — a sweep lists the directory
)

// NewDirCache returns a directory-backed chunk cache (creating the directory),
// or nil if it can't be created — the caller runs cache-less.
func NewDirCache(dir string) ChunkCache {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	return &dirCache{dir: dir, budget: dirCacheBudget}
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
	if d.puts.Add(1)%dirSweepEvery == 0 && d.sweeping.CompareAndSwap(false, true) {
		go func() {
			defer d.sweeping.Store(false)
			d.sweep()
		}()
	}
}

// sweep deletes the oldest cache files until the directory is under budget.
// It runs off the hot path and tolerates every error: a file that vanished
// under it was someone else's eviction.
func (d *dirCache) sweep() {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}
	type fileInfo struct {
		path  string
		size  int64
		mtime time.Time
	}
	var files []fileInfo
	var total int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".gc" {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{filepath.Join(d.dir, e.Name()), fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	if total <= d.budget {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
	removed := 0
	for _, f := range files {
		if total <= d.budget {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
		}
	}
	log.Printf("chunk cache: directory %s over budget — evicted %d oldest files", d.dir, removed)
}
