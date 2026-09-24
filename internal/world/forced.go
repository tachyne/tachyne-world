package world

import "sort"

// Force-loaded chunks (vanilla's ForcedChunksSavedData / the forced chunk
// ticket): once a forced chunk is generated the LRU never evicts it, so it
// stays Loaded — and the hub, which simulates loaded chunks only, keeps
// ticking it with nobody near. Which chunks are forced is the caller's to
// persist; the world only honours the set.

// SetForced forces a chunk (on) or releases it (off). It does not generate
// the chunk — a caller on the hub goroutine must not stall on up to 256
// generations; warm it with ForceLoad from another goroutine. Reports whether
// anything changed (ServerLevel.setChunkForced).
func (w *World) SetForced(cx, cz int32, on bool) bool {
	key := chunkPos{cx, cz}
	w.genMu.Lock()
	was := w.forced[key]
	if was != on {
		if on {
			if w.forced == nil {
				w.forced = map[chunkPos]bool{}
			}
			w.forced[key] = true
		} else {
			delete(w.forced, key)
		}
	}
	w.genMu.Unlock()
	return was != on
}

// Forced reports whether a chunk is force-loaded.
func (w *World) Forced(cx, cz int32) bool {
	w.genMu.Lock()
	defer w.genMu.Unlock()
	return w.forced[chunkPos{cx, cz}]
}

// ForcedCount is how many chunks are forced — the hub's per-tick fast path.
func (w *World) ForcedCount() int {
	w.genMu.Lock()
	defer w.genMu.Unlock()
	return len(w.forced)
}

// ForcedChunks lists the forced chunks in vanilla's order: sorted by the
// packed chunk long (z in the high word, x's low 32 bits unsigned below it).
func (w *World) ForcedChunks() [][2]int32 {
	w.genMu.Lock()
	out := make([][2]int32, 0, len(w.forced))
	for k := range w.forced {
		out = append(out, k)
	}
	w.genMu.Unlock()
	pack := func(c [2]int32) int64 { return int64(uint32(c[0])) | int64(c[1])<<32 }
	sort.Slice(out, func(i, j int) bool { return pack(out[i]) < pack(out[j]) })
	return out
}
