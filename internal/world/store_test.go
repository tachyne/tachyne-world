package world

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPersistenceRoundTrip: edits saved by one world are visible to a new world
// loaded from the same store — i.e. they survive a "restart".
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.gob")
	store := NewFileStore(path)

	w1, err := NewWithStore(7, store)
	if err != nil {
		t.Fatal(err)
	}
	// Place a couple of blocks, including at a negative coord.
	w1.SetBlock(10, 70, 10, 1) // stone
	w1.SetBlock(-5, 64, -5, worldgen.OakLog)
	if err := w1.Save(); err != nil {
		t.Fatal(err)
	}

	// "Restart": a fresh world from the same file must see the edits.
	w2, err := NewWithStore(7, store)
	if err != nil {
		t.Fatal(err)
	}
	if got := w2.Block(10, 70, 10); got != 1 {
		t.Errorf("persisted block (10,70,10) = %d, want 1", got)
	}
	if got := w2.Block(-5, 64, -5); got != worldgen.OakLog {
		t.Errorf("persisted block (-5,64,-5) = %d, want %d", got, worldgen.OakLog)
	}
}

// TestSaveNoopWhenClean: Save without a store or without changes does nothing.
func TestSaveNoopWhenClean(t *testing.T) {
	if err := New(1).Save(); err != nil { // no store
		t.Fatalf("Save on store-less world should be nil, got %v", err)
	}
	store := NewFileStore(filepath.Join(t.TempDir(), "w.gob"))
	w, _ := NewWithStore(1, store)
	if err := w.Save(); err != nil { // store but no edits
		t.Fatalf("Save with no edits should be nil, got %v", err)
	}
}
