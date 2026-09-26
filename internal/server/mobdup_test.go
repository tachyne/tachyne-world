package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestNoDoublingOnAutosaveThenUnload reproduces the herd-doubling bug: the
// 600-tick autosave writes active chunks' live mobs into the store, and if the
// chunk then unloads and MERGES that snapshot with its live mobs, the herd
// doubles every autosave-then-unload cycle.
func TestNoDoublingOnAutosaveThenUnload(t *testing.T) {
	h := newHub(world.New(1))
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	players := map[int32]*tracked{}
	h.tick.Store(1000)

	for i := 0; i < 3; i++ { // 3 cows in chunk (5,5)
		h.spawnMob(players, entityCow, 85.5+float64(i)*0.1, 70, 85.5)
	}
	inRange := map[[3]int32]bool{{0, 5, 5}: true}
	h.reconcileMobChunks(players, inRange) // activate (5,5)

	h.mobstore.bucketLive(h.mobs, h.persistMob, h.activeChunks) // the autosave

	empty := map[[3]int32]bool{}
	h.reconcileMobChunks(players, empty) // start unload clock
	h.tick.Store(1000 + mobUnloadGrace)
	h.reconcileMobChunks(players, empty) // evict (5,5)

	h.reconcileMobChunks(players, inRange) // return → reload
	cows := 0
	for _, m := range h.mobs {
		if m.etype == entityCow {
			cows++
		}
	}
	if cows != 3 {
		t.Fatalf("autosave-then-unload doubled the herd: reloaded %d cows, want 3", cows)
	}
}

// Bug #45: a mob that walked or teleported out of the loaded area (an
// enderman's daylight teleport reaches 32 blocks) was filed by the autosave
// into a chunk nothing had loaded — over that chunk's saved mobs — and that
// chunk's next load brought back a copy beside the original. One holding a
// block never despawns, so every copy stayed. Now the stray's chunk becomes
// active (its saved mobs load, nothing is overwritten) and saves with it.
func TestStrayMobIsNotCopiedOnItsChunksLoad(t *testing.T) {
	h := newHub(world.New(1))
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	players := map[int32]*tracked{}
	h.tick.Store(1000)
	// Chunk (9,9) already holds a saved zombie from an earlier visit.
	h.mobstore.stash(0, 9, 9, []savedMob{{Etype: entityZombie, X: 9*16 + 4, Y: 70, Z: 9*16 + 4, Health: 20}})

	e := h.spawnMob(players, entityEnderman, 9*16+8.5, 70, 9*16+8.5) // outside the loaded (5,5)
	if e == nil {
		t.Fatal("no enderman")
	}
	e.carriedBlock = 1 // holding a block: never despawns
	inRange := map[[3]int32]bool{{0, 5, 5}: true}
	h.reconcileMobChunks(players, inRange)
	h.mobstore.bucketLive(h.mobs, h.persistMob, h.activeChunks) // the autosave

	near := map[[3]int32]bool{{0, 9, 9}: true} // a player comes to (9,9)
	h.reconcileMobChunks(players, near)
	h.reconcileMobChunks(players, near)
	count := map[int]int{}
	for _, m := range h.mobs {
		count[m.etype]++
	}
	if count[entityEnderman] != 1 {
		t.Fatalf("%d endermen after the stray's chunk loaded, want 1", count[entityEnderman])
	}
	if count[entityZombie] != 1 {
		t.Fatalf("%d zombies: the chunk's saved mob must load once, not be lost or doubled", count[entityZombie])
	}
}

// A Nether or End mob was filed in the OVERWORLD bucket at its x/z, and an
// overworld player loading that chunk brought a copy back into its own
// dimension beside the original. Each dimension now keeps its own buckets.
func TestOtherDimensionMobIsNotCopiedByAnOverworldLoad(t *testing.T) {
	h := newHub(world.New(1))
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	players := map[int32]*tracked{}
	h.tick.Store(1000)
	e := h.spawnMob(players, entityEnderman, 5*16+8.5, 70, 5*16+8.5)
	if e == nil {
		t.Fatal("no enderman")
	}
	e.dim = dimEnd
	h.reconcileMobChunks(players, map[[3]int32]bool{{dimEnd, 5, 5}: true})
	h.mobstore.bucketLive(h.mobs, h.persistMob, h.activeChunks)
	h.reconcileMobChunks(players, map[[3]int32]bool{{dimEnd, 5, 5}: true, {0, 5, 5}: true})
	n := 0
	for _, m := range h.mobs {
		if m.etype == entityEnderman {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d endermen after an overworld load at the End mob's x/z, want 1", n)
	}
}

// Buckets written under the old x/z-only key are re-filed by each mob's own
// dimension when the store loads.
func TestMobStoreRefilesBucketsByDimension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	s := newMobStore(path)
	s.m.Chunks["5,5"] = []savedMob{
		{Etype: entityEnderman, Dim: dimEnd, X: 88, Y: 60, Z: 88},
		{Etype: entityCow, X: 88, Y: 70, Z: 88},
	}
	s.writeNow()
	s2 := newMobStore(path)
	if got := s2.take(dimEnd, 5, 5); len(got) != 1 || got[0].Etype != entityEnderman {
		t.Fatalf("End bucket after reload: %+v, want the enderman", got)
	}
	if got := s2.take(0, 5, 5); len(got) != 1 || got[0].Etype != entityCow {
		t.Fatalf("overworld bucket after reload: %+v, want the cow", got)
	}
}
