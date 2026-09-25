package world

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The build guard's frozen view of the edits.
//
// Generation keeps features out of player builds (worldgen/buildguard.go,
// treeguard.go). Asked of the live edits, that answer drifts: a bridge built
// over a ravine after the ravine was there would, at the next regeneration of
// any of its chunks, drop the whole ravine — or half of it, where some chunks
// were still cached. The builds a feature has to respect are the ones that
// existed before the feature did.
//
// So at boot under a new GenVersion the world copies its edits once, into a
// snapshot saved beside the world file and named for that version, and the
// generator's guard reads the snapshot from then on. A feature introduced by
// version V steps around what was built before V and never changes its mind
// afterwards; the next version takes a fresh snapshot. The rest of the game
// (and chunk contents) still read the live edits.

// FreezeGuard points the generator's build guard at the edit snapshot for the
// current GenVersion, taking it (and saving it to dir) if there is none yet.
// name tells the dimensions apart ("world", "nether", "end").
func (w *World) FreezeGuard(dir, name string) error {
	path := filepath.Join(dir, fmt.Sprintf("guard-v%d-%s.gob", worldgen.GenVersion, name))
	fs := NewFileStore(path)
	snap, err := fs.Load()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		w.mu.RLock()
		snap = make(map[chunkPos]map[int]uint32, len(w.edits))
		for k, m := range w.edits {
			c := make(map[int]uint32, len(m))
			for i, s := range m {
				c[i] = s
			}
			snap[k] = c
		}
		w.mu.RUnlock()
		if err := fs.Save(snap); err != nil {
			return err
		}
		// The previous versions' snapshots are done with.
		old, _ := filepath.Glob(filepath.Join(dir, "guard-v*-"+name+".gob"))
		for _, o := range old {
			if o != path {
				os.Remove(o)
			}
		}
	}
	w.guardSnap = snap
	w.gen.SetEditLookup(w.guardAt)
	w.gen.SetEditRegion(w.guardIn)
	return nil
}

// guardAt and guardIn are EditAt and ForEachEditIn over the snapshot. It is
// never written after FreezeGuard, so they read it without the lock.
func (w *World) guardAt(x, y, z int) (uint32, bool) {
	cx, cz, lx, lz := chunkOf(x, z)
	s, ok := w.guardSnap[chunkPos{int32(cx), int32(cz)}][localIndex(lx, y, lz)]
	return s, ok
}

func (w *World) guardIn(cx, cz int32, fn func(x, y, z int, state uint32)) {
	for idx, state := range w.guardSnap[chunkPos{cx, cz}] {
		lx, y, lz := splitIndex(idx)
		fn(int(cx)*16+lx, y, int(cz)*16+lz, state)
	}
}
