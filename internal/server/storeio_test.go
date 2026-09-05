package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The property every store depends on: a file that fails to decode is never
// mistaken for an empty one. It is moved aside with its bytes intact and the
// store starts empty — so the 30-second flush that follows writes a fresh
// file, not an empty one over the only good copy.
func TestLoadStoreQuarantinesCorruptFileInsteadOfLoadingItEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventories.json")
	corrupt := []byte(`{"EdgeZA": [1, 2, 3` /* truncated mid-write */)
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	var v map[string]any
	if err := loadStore(path, &v); err != nil {
		t.Fatalf("loadStore returned %v; a corrupt file should quarantine, not fail", err)
	}
	if v != nil {
		t.Errorf("store loaded as %v, want untouched (zero) after a decode failure", v)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupt file still at %s — it must be moved aside so nothing overwrites it", path)
	}
	entries, _ := os.ReadDir(dir)
	var quarantined string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "inventories.json.corrupt-") {
			quarantined = filepath.Join(dir, e.Name())
		}
	}
	if quarantined == "" {
		t.Fatalf("no quarantine file created; dir holds %v", entries)
	}
	got, _ := os.ReadFile(quarantined)
	if string(got) != string(corrupt) {
		t.Errorf("quarantined bytes differ from the original: %q vs %q", got, corrupt)
	}
}

func TestLoadStoreMissingFileIsEmpty(t *testing.T) {
	var v map[string]int
	if err := loadStore(filepath.Join(t.TempDir(), "absent.json"), &v); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if v != nil {
		t.Errorf("got %v, want nil", v)
	}
}

func TestLoadStoreDecodesAGoodFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, []byte(`{"a":1,"b":2}`), 0o644)
	var v map[string]int
	if err := loadStore(path, &v); err != nil {
		t.Fatal(err)
	}
	if v["a"] != 1 || v["b"] != 2 {
		t.Errorf("decoded %v", v)
	}
}

// writeAtomic leaves either the old file or the new one, never a partial, and
// never a stray .tmp on the success path.
func TestWriteAtomicReplacesWholeFileAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := writeAtomic(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("second — longer than the first")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "second — longer than the first" {
		t.Errorf("read back %q", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file left behind after a successful write")
	}
}

// End to end on the most precious store: a corrupt inventories.json must not
// come back as "everyone has nothing".
func TestInvStoreSurvivesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventories.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	s := newInvStore(path)
	if s == nil {
		t.Fatal("no store")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("corrupt inventories.json was left in place to be overwritten by the next flush")
	}
	s.flush() // a fresh, valid file — not an empty one written over the original
	if _, err := os.Stat(path); err != nil {
		t.Errorf("flush did not write a new file: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 { // the new file + the quarantined original
		t.Errorf("dir holds %d entries, want the fresh store plus the quarantined original: %v", len(entries), entries)
	}
}
