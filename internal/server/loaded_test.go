package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The hub never generates a chunk: random ticks, natural spawning and held
// maps skip one that is not loaded, and a scheduled update in one waits until
// it loads — as vanilla ticks only loaded chunks. Generating on the hub
// stalled whole ticks (and most of the test suite's time).
func TestHubLeavesUnloadedChunksAlone(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	pl := testTracked()
	pl.x, pl.y, pl.z = 5000.5, 120, 5000.5 // nothing about here is loaded
	players := map[int32]*tracked{1: pl}

	before := w.CacheLen()
	h.runRandomTicks(players)
	if got := w.CacheLen(); got != before {
		t.Fatalf("random ticks generated %d chunks on the hub", got-before)
	}

	// A chunk that is loaded but not ticking (nothing loaded around it) is
	// left alone too: its lighting would read, and generate, its neighbours.
	w.ForceLoad(5000, 5000, 0)
	before = w.CacheLen()
	for i := 0; i < 20; i++ {
		h.runRandomTicks(players)
	}
	if got := w.CacheLen(); got != before {
		t.Fatalf("random ticks at the edge of what is loaded generated %d chunks", got-before)
	}

	// Sand hanging over air in an unloaded chunk: its fall waits.
	x, y, z := 5100, 150, 5100
	w.SetBlock(x, y, z, worldgen.Sand)
	w.SetBlock(x, y-1, z, worldgen.Air)
	h.scheduleIn(0, blockPos{x, y, z}, 1)
	runTicks(h, players, 1, 30)
	if got := w.CacheLen(); got != before {
		t.Fatalf("a scheduled update generated %d chunks on the hub", got-before)
	}

	// Once its chunks load (a player's view), the waiting update runs.
	w.ForceLoad(x, z, 1)
	runTicks(h, players, 31, 80)
	if w.Block(x, y, z) == worldgen.Sand {
		t.Fatal("the sand's update never ran after its chunk loaded")
	}
}

// A beehive in a chunk nobody has loaded is left alone, as vanilla's block
// entity is: its bees do not age inside it, and reading it does not generate
// the chunk. 103 hives across the world once made the first tick after a
// boot take a second and a half.
func TestHivesTickOnlyInLoadedChunks(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	far := blockPos{8000, 90, 8000}
	h.hives = map[simPos][]hiveOccupant{{blockPos: far}: {{SecsLeft: 50}}}
	before := w.CacheLen()
	for i := 0; i < 5; i++ {
		h.updateBees(players)
	}
	if got := w.CacheLen(); got != before {
		t.Fatalf("updating a far hive generated %d chunks on the hub", got-before)
	}
	if occ := h.hives[simPos{blockPos: far}]; len(occ) != 1 || occ[0].SecsLeft != 50 {
		t.Fatalf("a bee in an unloaded hive aged: %+v", occ)
	}
}
