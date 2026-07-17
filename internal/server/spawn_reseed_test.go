package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestSeedSkipsPersistedChunks guards the herd-doubling bug: a chunk whose
// mobs are parked in the persistence store (a prior session populated it) must
// NOT get a fresh generation herd — it is marked seeded and its persisted herd
// reloads via reconcileMobChunks instead. Without the guard, seededChunks
// (in-memory, reset on restart) let seedChunkGeneration re-lay the one-time
// herd on top of the reloaded one, doubling animals every restart.
func TestSeedSkipsPersistedChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	h := newHub(world.New(1))
	h.mobstore = newMobStore(path)

	// Park a cow herd in chunk (0,0)'s store bucket, as a boot-time restore
	// would have (mobs.json → chunk buckets).
	h.mobstore.stash(0, 0, []savedMob{
		{Etype: entityCow, X: 5, Y: 70, Z: 5, Health: 10},
		{Etype: entityCow, X: 6, Y: 70, Z: 6, Health: 10},
	})

	chunkSet := map[[2]int32]bool{{0, 0}: true}
	var counts [catCount]int
	before := len(h.mobs)
	h.seedChunkGeneration(nil, chunkSet, &counts)

	if len(h.mobs) != before {
		t.Fatalf("a store-backed chunk must not get a generation herd: %d new mobs", len(h.mobs)-before)
	}
	if !h.seededChunks[[2]int32{0, 0}] {
		t.Fatal("a store-backed chunk must still be marked seeded (never re-seed it)")
	}
	// The persisted herd is untouched — it reloads via reconcile.
	if !h.mobstore.has(0, 0) {
		t.Fatal("seeding must not consume the parked persisted mobs")
	}
}
