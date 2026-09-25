package world

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The guard sees the edits as they stood when this GenVersion first booted:
// a build made afterwards does not reach it, a reboot under the same version
// keeps the same snapshot, and older versions' snapshots are cleared away.
func TestFreezeGuardSnapshot(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "guard-v1-world.gob")
	os.WriteFile(stale, nil, 0o644)
	planks := worldgen.BlockBase("oak_planks")

	w := New(1)
	w.SetBlock(1, 70, 1, planks) // built before this version
	if err := w.FreezeGuard(dir, "world"); err != nil {
		t.Fatal(err)
	}
	w.SetBlock(5, 70, 5, planks) // built after
	if _, ok := w.guardAt(1, 70, 1); !ok {
		t.Error("the snapshot lost the earlier build")
	}
	if _, ok := w.guardAt(5, 70, 5); ok {
		t.Error("a later build reached the guard")
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("an older version's snapshot was left behind")
	}

	w2 := New(1)
	w2.SetBlock(1, 70, 1, planks)
	w2.SetBlock(5, 70, 5, planks)
	if err := w2.FreezeGuard(dir, "world"); err != nil {
		t.Fatal(err)
	}
	if _, ok := w2.guardAt(5, 70, 5); ok {
		t.Error("a reboot under the same version re-took the snapshot")
	}
	n := 0
	w2.guardIn(0, 0, func(x, y, z int, s uint32) { n++ })
	if n != 1 {
		t.Errorf("guardIn saw %d edits in chunk 0,0, want 1", n)
	}
}
