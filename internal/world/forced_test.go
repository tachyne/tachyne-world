package world

import "testing"

// A forced chunk is passed over when the LRU evicts, even as its oldest entry,
// and is evictable again once released.
func TestForcedChunkSurvivesEviction(t *testing.T) {
	w := New(1)
	forced := chunkPos{50, 50}
	if !w.SetForced(50, 50, true) || w.SetForced(50, 50, true) {
		t.Fatal("SetForced should report only a change")
	}
	// Fill the cache to its cap with the forced chunk as the least recently
	// used entry. The stand-ins carry no chunk: eviction never reads one.
	w.genMu.Lock()
	w.cache[forced] = cacheEntry{elem: w.lru.PushFront(forced)}
	for i := int32(0); len(w.cache) < w.cacheCap(); i++ {
		k := chunkPos{1000 + i, 0}
		w.cache[k] = cacheEntry{elem: w.lru.PushFront(k)}
	}
	w.genMu.Unlock()

	w.generated(0, 0) // one more: something must go
	if !w.Loaded(50, 50) {
		t.Fatal("the forced chunk was evicted")
	}
	if w.Loaded(1000, 0) {
		t.Error("the oldest unforced chunk should have gone instead")
	}
	if n := w.CacheLen(); n > w.cacheCap() {
		t.Errorf("cache holds %d, over its cap %d", n, w.cacheCap())
	}

	w.SetForced(50, 50, false)
	w.generated(1, 0)
	if w.Loaded(50, 50) {
		t.Error("a released chunk should be evictable again")
	}
	if got := w.ForcedChunks(); len(got) != 0 {
		t.Errorf("forced chunks left: %v", got)
	}
}

// ForcedChunks sorts by vanilla's packed chunk long: z first, then x as an
// unsigned low word (so a negative x sorts after the positive ones).
func TestForcedChunksOrder(t *testing.T) {
	w := New(1)
	for _, c := range [][2]int32{{-1, 0}, {3, 0}, {0, 1}, {0, -1}} {
		w.SetForced(c[0], c[1], true)
	}
	want := [][2]int32{{0, -1}, {3, 0}, {-1, 0}, {0, 1}}
	got := w.ForcedChunks()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}
