package world

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The directory cache evicts oldest-first once over budget, and only then.
func TestDirCacheSweepEvictsOldestUntilUnderBudget(t *testing.T) {
	dir := t.TempDir()
	d := NewDirCache(dir).(*dirCache)
	d.budget = 5 * 1000 // room for five 1000-byte files
	payload := make([]byte, 1000)
	old := time.Now().Add(-time.Hour)
	for i := 0; i < 8; i++ {
		name := string(rune('a' + i))
		d.Put(name, payload)
		// Distinct, ordered mtimes so "oldest" is well defined.
		os.Chtimes(d.path(name), old.Add(time.Duration(i)*time.Minute), old.Add(time.Duration(i)*time.Minute))
	}
	d.sweep()
	left, _ := os.ReadDir(dir)
	if len(left) != 5 {
		t.Fatalf("%d files after sweep, want 5", len(left))
	}
	for _, name := range []string{"a", "b", "c"} { // the three oldest
		if _, err := os.Stat(d.path(name)); !os.IsNotExist(err) {
			t.Errorf("oldest file %s survived the sweep", name)
		}
	}
	for _, name := range []string{"d", "e", "f", "g", "h"} {
		if _, err := os.Stat(d.path(name)); err != nil {
			t.Errorf("recent file %s was evicted", name)
		}
	}
	d.sweep() // under budget: a no-op
	if left, _ = os.ReadDir(dir); len(left) != 5 {
		t.Errorf("sweep under budget removed files: %d left", len(left))
	}
	_ = filepath.Join
}

// Put triggers a sweep on the cadence, in the background, without ever
// blocking the caller.
func TestDirCachePutSchedulesSweep(t *testing.T) {
	dir := t.TempDir()
	d := NewDirCache(dir).(*dirCache)
	d.budget = 10
	for i := 0; i < dirSweepEvery; i++ {
		d.Put("k"+string(rune('0'+i%10))+string(rune('a'+i/10%26)), []byte("0123456789ABCDEF"))
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if left, _ := os.ReadDir(dir); len(left) <= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	left, _ := os.ReadDir(dir)
	t.Fatalf("background sweep never ran: %d files over a 10-byte budget", len(left))
}
