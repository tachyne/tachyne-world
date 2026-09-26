package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A block set back to what generation made there leaves no edit behind: the
// overlay remembers changes, not round trips.
func TestSettingTheGeneratedBlockLeavesNoEdit(t *testing.T) {
	w := New(1)
	x, z := 10, 10
	y := w.GroundY(x, z) // the top block: dirt, grass or sand, generated
	gen := w.At(x, y, z) // …and reading it brings the chunk into memory
	w.SetBlock(x, y, z, worldgen.Air)
	if _, ok := w.EditAt(x, y, z); !ok || w.EditCount() != 1 {
		t.Fatalf("digging the ground is an edit: count %d", w.EditCount())
	}
	w.SetBlock(x, y, z, gen)
	if _, ok := w.EditAt(x, y, z); ok {
		t.Fatal("putting the generated block back should drop the edit")
	}
	if w.EditCount() != 0 {
		t.Fatalf("EditCount %d after the edit went, want 0", w.EditCount())
	}
	if w.At(x, y, z) != gen {
		t.Fatal("the block still reads as generated")
	}
	// Writing the generated block where there was never an edit adds none.
	w.SetBlock(x, y-1, z, w.At(x, y-1, z))
	if w.EditCount() != 0 {
		t.Fatalf("a no-op write became an edit: count %d", w.EditCount())
	}
}

// A chunk that is not in memory is never generated to answer the question:
// the write is kept as an edit, as before.
func TestSetBlockNeverGenerates(t *testing.T) {
	w := New(1)
	w.SetBlock(5000, 70, 5000, worldgen.Stone)
	if w.CacheLen() != 0 {
		t.Fatalf("SetBlock generated %d chunk(s)", w.CacheLen())
	}
	if _, ok := w.EditAt(5000, 70, 5000); !ok {
		t.Fatal("with nothing to compare against, the write is an edit")
	}
}

// RevertEdit keeps the edit count honest.
func TestRevertEditCounts(t *testing.T) {
	w := New(1)
	w.SetBlock(5000, 70, 5000, worldgen.Stone)
	w.RevertEdit(5000, 70, 5000)
	w.RevertEdit(5000, 70, 5000) // nothing there any more
	if w.EditCount() != 0 {
		t.Fatalf("EditCount %d after revert, want 0", w.EditCount())
	}
}
