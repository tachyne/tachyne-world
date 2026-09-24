package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Mob.checkDespawn: noActionTime keeps counting while an enderman carries a
// block (it is only persistent, not reset), so one far from every player
// can go the moment it sets its block down. Resetting the clock while it
// carried meant it needed thirty fresh seconds after a put-down, picked the
// next block up long before that, and never went (bug #31).
func TestEndermanDespawnsSoonAfterPuttingItsBlockDown(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 60.5, 100, 0.5 // 60 blocks off: past 32, inside 128
	m := h.spawnHostileYIn(players, entityEnderman, dimOverworld, 0.5, 100, 0.5)
	m.carriedBlock = worldgen.GrassBlock
	for i := 0; i < 40; i++ {
		h.despawnSweep(players)
	}
	if h.mobs[m.eid] == nil {
		t.Fatal("an enderman carrying a block despawned")
	}
	if m.idleSecs <= 30 {
		t.Fatalf("idle clock %d after 40 far-off seconds carrying, want past 30", m.idleSecs)
	}
	m.carriedBlock = 0 // it sets its block down
	for i := 0; i < 400 && h.mobs[m.eid] != nil; i++ {
		h.despawnSweep(players)
	}
	if h.mobs[m.eid] != nil {
		t.Error("an idle enderman with its hands empty never despawned")
	}
}
