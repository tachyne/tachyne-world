package world

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestCorruptWorldFileRefusesToLoad: a damaged edit file must stop the world
// from starting — never load as an empty world that the next autosave would
// write over the only copy of the player's builds — and must be left
// byte-for-byte as it was, for recovery.
func TestCorruptWorldFileRefusesToLoad(t *testing.T) {
	for _, damage := range []struct {
		name string
		data []byte
	}{
		{"garbage", []byte("this is not a gob file at all")},
		{"truncated", nil}, // filled below: a real save cut short
	} {
		path := filepath.Join(t.TempDir(), "world.gob")
		if damage.data == nil {
			w, err := NewWithStore(1, NewFileStore(path))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 200; i++ {
				w.SetBlock(i, 70, i, 1)
			}
			if err := w.Save(); err != nil {
				t.Fatal(err)
			}
			full, _ := os.ReadFile(path)
			damage.data = full[:len(full)/2]
		}
		if err := os.WriteFile(path, damage.data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := NewWithStore(1, NewFileStore(path)); err == nil {
			t.Fatalf("%s: a corrupt world file loaded without error", damage.name)
		}
		if _, err := NewNether(1, NewFileStore(path)); err == nil {
			t.Fatalf("%s: a corrupt Nether file loaded without error", damage.name)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, damage.data) {
			t.Fatalf("%s: the corrupt file was altered by the failed load", damage.name)
		}
	}
}
