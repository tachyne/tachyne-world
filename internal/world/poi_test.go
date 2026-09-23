package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The POI index answers from loaded chunks only, sees edits over generated
// blocks (a placed bell appears, a broken one goes), and sorts closest first.
func TestPOIIndex(t *testing.T) {
	w := New(1)
	bell, _, _ := worldgen.BlockRangeOK("bell")
	w.SetPOIKinds(func(s uint32) uint8 {
		if lo, hi, ok := worldgen.BlockRangeOK("bell"); ok && s >= lo && s <= hi {
			return 1
		}
		return 0
	})
	x, y, z := 7000, 200, 7000
	w.SetBlock(x, y, z, bell)
	w.SetBlock(x+5, y, z, bell)
	if got := w.POIsNear(x, y, z, 48, nil); len(got) != 0 {
		t.Fatalf("an unloaded chunk answered %d POIs", len(got))
	}
	before := w.CacheLen()
	w.POIsNear(x, y, z, 48, nil)
	if w.CacheLen() != before {
		t.Fatal("a POI query generated chunks")
	}
	w.ForceLoad(x, z, 1)
	got := w.POIsNear(x+4, y, z, 48, nil)
	if len(got) != 2 || got[0].X != x+5 || got[1].X != x {
		t.Fatalf("want the two bells closest first, got %+v", got)
	}
	w.SetBlock(x+5, y, z, worldgen.Air) // broken: gone from the index
	if got := w.POIsNear(x, y, z, 48, nil); len(got) != 1 || got[0].X != x {
		t.Fatalf("after breaking one bell, got %+v", got)
	}
	if got := w.POIsNear(x+60, y, z, 48, nil); len(got) != 0 {
		t.Fatalf("a bell 60 blocks off is outside 48: got %+v", got)
	}
}
