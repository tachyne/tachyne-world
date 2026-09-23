package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A generated spring runs once its chunk is loaded, as vanilla's does the
// moment it is placed — it no longer hangs in its cave wall.
func TestGeneratedSpringsRun(t *testing.T) {
	w := world.New(1)
	var spring [3]int
	found := false
	for cx := int32(0); cx < 20 && !found; cx++ {
		for cz := int32(0); cz < 20 && !found; cz++ {
			if u := w.UnstableFluids(cx, cz); len(u) > 0 {
				spring, found = u[0], true
			}
		}
	}
	if !found {
		t.Skip("no generated spring in reach on this seed")
	}
	fluid := w.Block(spring[0], spring[1], spring[2])
	open := func() int {
		n := 0
		for _, d := range [5][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
			if w.Block(spring[0]+d[0], spring[1]+d[1], spring[2]+d[2]) == worldgen.Air {
				n++
			}
		}
		return n
	}
	before := open()
	if before == 0 {
		t.Fatalf("spring at %v has no opening", spring)
	}
	h := newHub(w)
	tr := testTracked()
	tr.x, tr.y, tr.z = float64(spring[0]), float64(spring[1]), float64(spring[2])
	players := map[int32]*tracked{1: tr}
	for i := 0; i < 20; i++ { // prime the view window
		h.primeFluids(players)
	}
	runTicks(h, players, 0, 400)
	if after := open(); after >= before {
		t.Errorf("the spring at %v (%d) did not run: %d openings before, %d after", spring, fluid, before, after)
	}
}
