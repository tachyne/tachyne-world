package world

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// lightDiff names the first cell where the incremental light a differs from
// the full fill b, or returns "" when they are byte-identical.
func lightDiff(a, b *LightData) string {
	if len(a.Sky) != len(b.Sky) || len(a.Block) != len(b.Block) {
		return fmt.Sprintf("section counts differ: sky %d/%d, block %d/%d",
			len(a.Sky), len(b.Sky), len(a.Block), len(b.Block))
	}
	for s := range a.Sky {
		for _, ch := range []struct {
			name string
			x, y *[4096]uint8
		}{{"sky", &a.Sky[s], &b.Sky[s]}, {"block", &a.Block[s], &b.Block[s]}} {
			if *ch.x == *ch.y {
				continue
			}
			for i := range ch.x {
				if ch.x[i] != ch.y[i] {
					return fmt.Sprintf("%s light at local (%d, y=%d, %d): incremental %d, full fill %d",
						ch.name, i%16, worldgen.MinY+s*16+i/256, (i/16)%16, ch.x[i], ch.y[i])
				}
			}
		}
	}
	return ""
}

// checkChunkLight compares one chunk's cached (relit) light with a fresh full
// flood fill of the same blocks.
func checkChunkLight(t *testing.T, w *World, cx, cz int, what string) bool {
	t.Helper()
	if d := lightDiff(w.Light(int32(cx), int32(cz)), w.computeLight(int32(cx), int32(cz))); d != "" {
		t.Errorf("%s: chunk %d,%d: %s", what, cx, cz, d)
		return false
	}
	return true
}

// checkEditLight checks the edited cell's chunk and one other chunk in the
// edit's reach (rotating by step), or with all=true every chunk in reach.
// A full fill costs a few milliseconds, so checking all nine after every one
// of hundreds of edits would make the test crawl; the rotation still covers
// the neighbours, and a wrong value left behind persists into the whole-area
// check at the end.
func checkEditLight(t *testing.T, w *World, x, z, step int, all bool, what string) bool {
	t.Helper()
	cx0, cx1 := floorDiv(x-lightReach, 16), floorDiv(x+lightReach, 16)
	cz0, cz1 := floorDiv(z-lightReach, 16), floorDiv(z+lightReach, 16)
	if !checkChunkLight(t, w, floorDiv(x, 16), floorDiv(z, 16), what) {
		return false
	}
	nx, nz := cx1-cx0+1, cz1-cz0+1
	if !all {
		k := step % (nx * nz)
		if k < 0 {
			k += nx * nz
		}
		return checkChunkLight(t, w, cx0+k%nx, cz0+k/nx, what)
	}
	for cz := cz0; cz <= cz1; cz++ {
		for cx := cx0; cx <= cx1; cx++ {
			if !checkChunkLight(t, w, cx, cz, what) {
				return false
			}
		}
	}
	return true
}

// runLightDifferential applies a random sequence of edits around the origin —
// emitters, opaque and translucent blocks, water, digging, shafts sunk from
// the sky and roofs laid back over them — through SetBlock/RevertEdit, and
// after each compares the relit light with a full recompute.
func runLightDifferential(t *testing.T, w *World, seed int64, steps int) {
	t.Helper()
	// Light every chunk an edit can reach (edits stay within 13 blocks of the
	// origin, reach is 15 more), so each edit relights in place.
	for cz := int32(-3); cz <= 2; cz++ {
		for cx := int32(-3); cx <= 2; cx++ {
			w.Light(cx, cz)
		}
	}
	rng := rand.New(rand.NewSource(seed))
	surf := int(w.SurfaceY(0, 0))
	if surf+16 >= w.Ceiling() {
		surf = w.Ceiling() - 17
	}
	palette := []uint32{
		worldgen.BlockBase("torch"),
		worldgen.BlockBase("glowstone"),
		worldgen.BlockBase("sea_lantern"),
		worldgen.BlockBase("lava"),
		worldgen.Stone,
		worldgen.BlockBase("glass"),
		worldgen.Water,
		worldgen.BlockBase("oak_leaves"),
		worldgen.Air,
		worldgen.Air,
	}
	for step := 0; step < steps; step++ {
		x, z := rng.Intn(25)-12, rng.Intn(25)-12
		all := step%8 == 7
		switch r := rng.Intn(10); {
		case r < 6:
			y := surf - 8 + rng.Intn(20)
			st := palette[rng.Intn(len(palette))]
			w.SetBlock(x, y, z, st)
			if !checkEditLight(t, w, x, z, step, all, fmt.Sprintf("step %d: set (%d,%d,%d) to state %d", step, x, y, z, st)) {
				return
			}
		case r < 7:
			y := surf - 8 + rng.Intn(20)
			w.RevertEdit(x, y, z)
			if !checkEditLight(t, w, x, z, step, all, fmt.Sprintf("step %d: revert (%d,%d,%d)", step, x, y, z)) {
				return
			}
		case r < 9: // sink a shaft from the open sky, a block at a time
			for y := surf + 2; y >= surf-8; y-- {
				w.SetBlock(x, y, z, worldgen.Air)
				if !checkEditLight(t, w, x, z, step+y, all, fmt.Sprintf("step %d: dig shaft at (%d,%d,%d)", step, x, y, z)) {
					return
				}
			}
		default: // roof a strip over it again
			for dx := -1; dx <= 1; dx++ {
				w.SetBlock(x+dx, surf+3, z, worldgen.Stone)
				if !checkEditLight(t, w, x+dx, z, step+dx, all, fmt.Sprintf("step %d: roof at (%d,%d,%d)", step, x+dx, surf+3, z)) {
					return
				}
			}
		}
	}
	relit, dropped := w.lightStats()
	if relit == 0 {
		t.Fatal("no edit was relit in place")
	}
	if dropped != 0 {
		t.Errorf("%d edits fell back to dropping cached light with every chunk in reach lit", dropped)
	}
	for cz := -3; cz <= 2; cz++ {
		for cx := -3; cx <= 2; cx++ {
			checkChunkLight(t, w, cx, cz, "after the sequence")
		}
	}
}

// TestIncrementalLightMatchesFullFill: per-block relighting of sky AND block
// light after every edit is byte-for-byte what the full flood fill gives for
// the same blocks.
func TestIncrementalLightMatchesFullFill(t *testing.T) {
	steps := 30
	if testing.Short() {
		steps = 8
	}
	for _, seed := range []int64{1, 7} {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			runLightDifferential(t, New(seed), seed*31+5, steps)
		})
	}
}

// TestIncrementalLightNether: the same with no sky (block light only).
func TestIncrementalLightNether(t *testing.T) {
	w, err := NewNether(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	steps := 10
	if testing.Short() {
		steps = 4
	}
	runLightDifferential(t, w, 99, steps)
}

// TestIncrementalLightTorchInPocket: a torch placed in a carved pocket lights
// its surroundings through the relight path, and taking it away returns the
// pocket to darkness — with the chunks in reach relit, not dropped.
func TestIncrementalLightTorchInPocket(t *testing.T) {
	w := New(1)
	for cz := int32(-1); cz <= 1; cz++ {
		for cx := int32(-1); cx <= 1; cx++ {
			w.Light(cx, cz)
		}
	}
	const x, z = 8, 8
	y := int(w.SurfaceY(x, z)) - 8 // TestBlockLightFromEmitter's sealed pocket
	for dx := 0; dx <= 2; dx++ {
		w.SetBlock(x+dx, y, z, worldgen.Air)
	}
	w.SetBlock(x, y, z, worldgen.BlockBase("torch"))
	if got := w.BlockLightAt(x+1, y, z); got != 13 {
		t.Errorf("block light beside a torch = %d, want 13", got)
	}
	if got := w.BlockLightAt(x+2, y, z); got != 12 {
		t.Errorf("block light two from a torch = %d, want 12", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)
	if got := w.BlockLightAt(x+1, y, z); got != 0 {
		t.Errorf("block light after the torch is gone = %d, want 0", got)
	}
	checkChunkLight(t, w, 0, 0, "pocket")
	if relit, dropped := w.lightStats(); relit == 0 || dropped != 0 {
		t.Errorf("relit %d, dropped %d: want every edit relit in place", relit, dropped)
	}
}

// TestIncrementalLightPublishedCopiesStayPut: a LightData handed out before an
// edit keeps its values (readers encode it without locks); the edit publishes
// a new copy.
func TestIncrementalLightPublishedCopiesStayPut(t *testing.T) {
	w := New(1)
	for cz := int32(-1); cz <= 1; cz++ {
		for cx := int32(-1); cx <= 1; cx++ {
			w.Light(cx, cz)
		}
	}
	const x, y, z = 8, 250, 8 // open sky, far over any terrain
	before := w.Light(0, 0)
	keep := &LightData{Sky: append([][4096]uint8(nil), before.Sky...), Block: append([][4096]uint8(nil), before.Block...)}
	w.SetBlock(x, y, z, worldgen.BlockBase("glowstone"))
	if d := lightDiff(before, keep); d != "" {
		t.Fatalf("an edit wrote into a published LightData: %s", d)
	}
	after := w.Light(0, 0)
	if after == before {
		t.Fatal("the edit did not publish a new LightData")
	}
	if got := after.Block[(y+1-worldgen.MinY)/16][((y+1-worldgen.MinY)%16*16+z)*16+x]; got != 14 {
		t.Errorf("block light over the glowstone = %d, want 14", got)
	}
}

// TestIncrementalLightConcurrentReaders: readers on other goroutines keep
// reading (and computing) chunk light while edits relight it; run under -race
// this pins the copy-on-write and locking. The result must still match the
// full fill.
func TestIncrementalLightConcurrentReaders(t *testing.T) {
	w := New(7)
	for cz := int32(-2); cz <= 1; cz++ {
		for cx := int32(-2); cx <= 1; cx++ {
			w.Light(cx, cz)
		}
	}
	surf := int(w.SurfaceY(0, 0))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				cx, cz := int32(i%6-3), int32((i/6+g)%6-3)
				ld := w.Light(cx, cz)
				_ = ld.Sky[len(ld.Sky)-1][i%4096] + ld.Block[0][i%4096]
			}
		}(g)
	}
	rng := rand.New(rand.NewSource(11))
	states := []uint32{worldgen.BlockBase("torch"), worldgen.Stone, worldgen.Air, worldgen.BlockBase("glowstone")}
	for i := 0; i < 20; i++ {
		x, z := rng.Intn(9)-4, rng.Intn(9)-4
		w.SetBlock(x, surf-4+rng.Intn(8), z, states[rng.Intn(len(states))])
	}
	close(stop)
	wg.Wait()
	for cz := -1; cz <= 0; cz++ {
		for cx := -1; cx <= 0; cx++ {
			checkChunkLight(t, w, cx, cz, "after concurrent edits")
		}
	}
}
