package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The block-edit migration rewrites each dimension's edit file in place, so
// it keeps a copy of the file as it was first — and it runs once: a world
// saved before markers existed is 1.21.5-numbered, and afterwards the marker
// says the canonical version.
func TestEditMigrationKeepsTheOriginalAndRunsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.gob")
	w, err := world.NewWithStore(1, world.NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	w.SetBlock(1, 70, 1, worldgen.BlockID("lantern"))
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	s := &Server{WorldFile: path, world: w}
	if err := s.migrateEditsIDSpace(); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(path + ".pre-" + idSpaceVersion)
	if err != nil || !bytes.Equal(kept, before) {
		t.Fatalf("no untouched copy of the edits kept (err %v)", err)
	}
	if got := savedIDSpace(filepath.Join(dir, ".idspace")); got != idSpaceVersion {
		t.Fatalf("marker says %q, want %q", got, idSpaceVersion)
	}
	after, _ := os.ReadFile(path)
	if err := s.migrateEditsIDSpace(); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, after) {
		t.Error("a second run changed the edits again")
	}
}
