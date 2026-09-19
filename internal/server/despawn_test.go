package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Vanilla's per-species despawn rules: a cow never goes, a wild cat only
// after two minutes alive, a jockey's chicken with its rider, a raider in
// its raid never, a block-carrying enderman never, a leashed or riding
// mob never; a plain zombie beyond 128 blocks at once.
func TestDespawnRules(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{1: pl}
	h.tick.Store(100)
	far := 200.5
	cow := h.spawnMob(players, entityCow, far, 64, 0.5)
	cat := h.spawnMob(players, entityCat, far, 64, 0.5)
	chick := h.spawnMob(players, entityChicken, far, 64, 0.5)
	chick.jockey = true
	zom := h.spawnHostileY(players, entityZombie, far, 64, 0.5)
	raider := h.spawnHostileY(players, entityPillager, far, 64, 0.5)
	raider.raidCenter = blockPos{10, 64, 10}
	ender := h.spawnHostileY(players, entityEnderman, far, 64, 0.5)
	ender.carriedBlock = 5
	leashed := h.spawnHostileY(players, entitySkeleton, far, 64, 0.5)
	leashed.leash = 7
	h.despawnSweep(players)
	for _, tc := range []struct {
		m    *mob
		stay bool
		why  string
	}{{cow, true, "an animal never despawns"}, {cat, true, "a cat younger than two minutes stays"},
		{chick, false, "a jockey's chicken goes"}, {zom, false, "a zombie beyond 128 goes at once"},
		{raider, true, "a raider in its raid stays"}, {ender, true, "an enderman holding a block stays"},
		{leashed, true, "a leashed mob stays"}} {
		if _, ok := h.mobs[tc.m.eid]; ok != tc.stay {
			t.Errorf("%s (present %v)", tc.why, ok)
		}
	}
	h.tick.Store(100 + 2401)
	h.despawnSweep(players)
	if _, ok := h.mobs[cat.eid]; ok {
		t.Error("a wild cat older than two minutes goes")
	}
	tame := h.spawnMob(players, entityCat, far, 64, 0.5)
	tame.tamed = true
	h.tick.Store(100 + 6000)
	h.despawnSweep(players)
	if _, ok := h.mobs[tame.eid]; !ok {
		t.Error("a tamed cat stays")
	}
}
