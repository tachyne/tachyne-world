package world

import "testing"

// A ChunkReader must see exactly what World.At sees — generated terrain and
// the edit overlay alike — or the random ticker would grow crops in a world
// the players are not standing in.
func TestReaderMatchesAt(t *testing.T) {
	w := New(7)
	w.SetBlock(35, 64, -3, 1234)  // chunk (2,-1)
	w.SetBlock(32, 70, -16, 4321) // same chunk, corner
	rd := w.Reader(2, -1)
	for _, p := range [][3]int{{35, 64, -3}, {32, 70, -16}, {40, 60, -8}, {47, 90, -1}, {33, -60, -15}, {36, 400, -5}} {
		if got, want := rd.At(p[0], p[1], p[2]), w.At(p[0], p[1], p[2]); got != want {
			t.Errorf("Reader.At%v = %d, At = %d", p, got, want)
		}
	}
}

// The LRU promotes a chunk at most once per epoch. Without Tick a re-read is
// free (no list write); after Tick it promotes again — so recency is still
// tracked at tick granularity and eviction order stays meaningful.
func TestCachePromotesOncePerEpoch(t *testing.T) {
	w := New(1)
	w.generated(0, 0) // inserted at epoch 0, front
	w.generated(1, 0) // now front
	front := func() chunkPos { return w.lru.Front().Value.(chunkPos) }
	if front() != (chunkPos{1, 0}) {
		t.Fatalf("front = %v after two inserts", front())
	}
	w.generated(0, 0) // same epoch as its insert: no promotion
	if front() != (chunkPos{1, 0}) {
		t.Errorf("re-read in the same epoch promoted %v; want no LRU write", front())
	}
	w.Tick()
	w.generated(0, 0) // new epoch: promotes
	if front() != (chunkPos{0, 0}) {
		t.Errorf("read in a new epoch did not promote; front = %v", front())
	}
	w.generated(0, 0)
	w.generated(0, 0) // and again only once
	w.genMu.Lock()
	e := w.cache[chunkPos{0, 0}]
	w.genMu.Unlock()
	if e.touched != w.epoch.Load() {
		t.Errorf("touched = %d, epoch = %d", e.touched, w.epoch.Load())
	}
}
