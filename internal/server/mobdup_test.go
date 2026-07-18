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
	inRange := map[[2]int32]bool{{5, 5}: true}
	h.reconcileMobChunks(players, inRange) // activate (5,5)

	h.mobstore.bucketLive(h.mobs, h.persistMob, h.activeChunks) // the autosave

	empty := map[[2]int32]bool{}
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
