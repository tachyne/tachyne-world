package world

import "testing"

// EditCount is maintained incrementally now (it used to walk every overlay on
// every autosave). It must count distinct positions, not writes.
func TestEditCountTracksDistinctPositions(t *testing.T) {
	w := New(1)
	if w.EditCount() != 0 {
		t.Fatal("fresh world has edits")
	}
	w.SetBlock(1, 70, 1, 5)
	w.SetBlock(1, 70, 1, 6) // overwrite: same position
	w.SetBlock(2, 70, 1, 5)
	w.SetBlock(200, 70, -300, 5) // another chunk
	if got := w.EditCount(); got != 3 {
		t.Errorf("EditCount = %d, want 3", got)
	}
	// And it matches a full walk.
	w.mu.RLock()
	n := 0
	for _, m := range w.edits {
		n += len(m)
	}
	w.mu.RUnlock()
	if n != w.EditCount() {
		t.Errorf("walk says %d, counter says %d", n, w.EditCount())
	}
}

// A world loaded from a store starts with the right count.
func TestEditCountAfterLoad(t *testing.T) {
	dir := t.TempDir()
	st := NewFileStore(dir + "/w.gob")
	w, err := NewWithStore(1, st)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		w.SetBlock(i, 70, 0, 9)
	}
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	w2, err := NewWithStore(1, NewFileStore(dir+"/w.gob"))
	if err != nil {
		t.Fatal(err)
	}
	if w2.EditCount() != 7 {
		t.Errorf("loaded EditCount = %d, want 7", w2.EditCount())
	}
}
