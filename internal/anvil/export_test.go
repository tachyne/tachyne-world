package anvil

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// headerStamp reads the region-header timestamp for a local chunk.
func headerStamp(t *testing.T, path string, lx, lz int) uint32 {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return binary.BigEndian.Uint32(raw[sectorSize+4*(lz*32+lx):])
}

func TestExportIncremental(t *testing.T) {
	dir := t.TempDir()
	w := world.New(7)

	st := NewExportState()
	opt := Options{
		Dir: dir, Radius: 1, Timestamp: 1000, NoLight: true, State: st,
	}
	changed, err := Export(w, opt)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 9 {
		t.Fatalf("first export changed %d, want 9", changed)
	}
	region := filepath.Join(dir, "region", "r.0.0.mca")
	if headerStamp(t, region, 0, 0) != 1000 {
		t.Fatal("first export stamp")
	}
	negRegion := filepath.Join(dir, "region", "r.-1.-1.mca")
	if headerStamp(t, negRegion, 31, 31) != 1000 { // chunk (-1,-1)
		t.Fatal("negative-coord chunk missing")
	}

	// Second export, nothing changed: no chunk rewrites, stamps preserved.
	info1, _ := os.Stat(region)
	opt.Timestamp = 2000
	changed, err = Export(w, opt)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("no-op export changed %d", changed)
	}
	info2, _ := os.Stat(region)
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatal("unchanged region file was rewritten")
	}

	// Edit one block: only that chunk's stamp moves.
	w.SetBlock(5, 80, 5, 1) // chunk (0,0)
	opt.Timestamp = 3000
	changed, err = Export(w, opt)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("edit export changed %d, want 1", changed)
	}
	if got := headerStamp(t, region, 0, 0); got != 3000 {
		t.Fatalf("edited chunk stamp %d", got)
	}
	if got := headerStamp(t, region, 1, 0); got != 1000 {
		t.Fatalf("untouched chunk stamp %d", got)
	}
	if got := headerStamp(t, negRegion, 31, 31); got != 1000 {
		t.Fatalf("untouched region stamp %d", got)
	}

	// State survives a round trip.
	sp := filepath.Join(dir, "state.json")
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadExportState(sp)
	if err != nil {
		t.Fatal(err)
	}
	opt.State = st2
	opt.Timestamp = 4000
	if changed, _ = Export(w, opt); changed != 0 {
		t.Fatalf("reloaded-state export changed %d", changed)
	}
}
