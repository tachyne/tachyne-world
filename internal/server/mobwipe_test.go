package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWipeWildKeepsVillageTiedAndTamed verifies the wild-wipe removes all
// naturally-spawned mobs (passives + hostiles) while preserving the village-tied
// and tamed set, and marks every populated chunk permanently seeded.
func TestWipeWildKeepsVillageTiedAndTamed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mobs.json")
	st := newMobStore(path)

	// Chunk 0,0: two cows (wild), a villager, a tamed wolf.
	st.stash(0, 0, []savedMob{
		{Etype: entityCow, X: 1, Z: 1, Health: 10},
		{Etype: entityCow, X: 2, Z: 2, Health: 10},
		{Etype: entityVillager, X: 3, Z: 3, Health: 20, Profession: 1},
		{Etype: entityWolf, X: 4, Z: 4, Health: 20, Tamed: true},
	})
	// Chunk 5,-3: a lone hostile — wiped, but the chunk still gets seeded.
	st.stash(5, -3, []savedMob{{Etype: entityZombie, X: 85, Z: -40, Health: 20}})

	before, after := st.wipeWild()
	if before != 5 {
		t.Fatalf("before = %d, want 5", before)
	}
	if after != 2 {
		t.Fatalf("after = %d, want 2 (villager + tamed wolf)", after)
	}

	kept := st.take(0, 0)
	if len(kept) != 2 {
		t.Fatalf("chunk 0,0 kept %d mobs, want 2", len(kept))
	}
	for _, m := range kept {
		if m.Etype == entityCow {
			t.Fatalf("a wild cow survived the wipe")
		}
	}
	if got := st.take(5, -3); len(got) != 0 {
		t.Fatalf("hostile chunk kept %d mobs, want 0", len(got))
	}

	// Both populated chunks are now permanently seeded.
	seeded := st.seededSet()
	if !seeded[[2]int32{0, 0}] || !seeded[[2]int32{5, -3}] {
		t.Fatalf("populated chunks not marked seeded: %v", seeded)
	}
}

// TestSeededSetPersistsAcrossReload verifies the seeded-chunk set round-trips
// through flush + reload, so chunk-generation herds fire once per chunk EVER.
func TestSeededSetPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mobs.json")

	st := newMobStore(path)
	st.recordSeeded(map[[2]int32]bool{{1, 2}: true, {-3, 4}: true})
	st.flush()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mobs.json not written: %v", err)
	}

	st2 := newMobStore(path)
	seeded := st2.seededSet()
	if !seeded[[2]int32{1, 2}] || !seeded[[2]int32{-3, 4}] {
		t.Fatalf("seeded set did not persist: %v", seeded)
	}
	if seeded[[2]int32{9, 9}] {
		t.Fatalf("unexpected chunk in seeded set")
	}
}
