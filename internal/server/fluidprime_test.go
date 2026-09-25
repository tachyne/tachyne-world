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
	w.ForceLoad(spring[0], spring[2], 1) // the player's view around it
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

// A restart drops every scheduled tick, and what was moving froze: bug #20
// caught flowing lava hanging where a burned tree's leaves had been, beside
// fire with nothing left to burn. Priming the chunk wakes its saved fluid
// and fire edits, and they settle as vanilla's would have.
func TestPrimingWakesFrozenLavaAndFire(t *testing.T) {
	w := world.New(1)
	x, y, z := 3000, 170, 3000
	w.ForceLoad(x, z, 1)
	for dx := -3; dx <= 3; dx++ { // open air all round, well above the terrain
		for dz := -3; dz <= 3; dz++ {
			for dy := -4; dy <= 3; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	flowing := worldgen.LavaBase + 2 // flowing lava, no source, nothing under it
	w.SetBlock(x, y, z, flowing)
	w.SetBlock(x+2, y, z, fireDefault) // fire with nothing flammable about
	h := newHub(w)
	tr := testTracked()
	tr.x, tr.y, tr.z = float64(x)+0.5, float64(y), float64(z)+0.5
	players := map[int32]*tracked{1: tr}
	h.primeFluids(players)
	runTicks(h, players, 1, 200)
	if got := w.Block(x, y, z); got == flowing {
		t.Error("the flowing lava is still hanging where it froze")
	}
	if isFire(w.Block(x+2, y, z)) {
		t.Error("the fire is still burning with nothing to burn")
	}
}

// TestPrimingRearmsFrogspawn: frogspawn hatches on a scheduled tick, which
// vanilla saves with the chunk; the engine keeps it in memory, so a clutch
// from before a restart must be armed again when its chunk is primed — at
// a hatch time inside vanilla's three-to-ten-minute window.
func TestPrimingRearmsFrogspawn(t *testing.T) {
	w := world.New(1)
	x, y, z := 3000, 170, 3000
	w.ForceLoad(x, z, 1)
	w.SetBlock(x, y-1, z, worldgen.WaterBase)
	w.SetBlock(x, y, z, frogspawnBlock)
	h := newHub(w)
	tr := testTracked()
	tr.x, tr.y, tr.z = float64(x), float64(y), float64(z)
	players := map[int32]*tracked{1: tr}
	for i := 0; i < 20; i++ {
		h.primeFluids(players)
	}
	now := h.tick.Load()
	for due, list := range h.pending {
		for _, sp := range list {
			if sp.blockPos == (blockPos{x, y, z}) && due >= now+frogspawnMinHatch && due < now+frogspawnMaxHatch {
				return
			}
		}
	}
	t.Fatal("the frogspawn left from before a restart was never armed to hatch")
}
