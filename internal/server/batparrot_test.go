package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestBatHangsAndParrotMimics: a bat under a stone ceiling settles and
// hangs, a player coming within four wakes it; a parrot knows a zombie's
// call but not a cow's.
func TestBatHangsAndParrotMimics(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	w.SetBlock(0, 181, 0, worldgen.Stone)
	pl.x, pl.y, pl.z = 30, 180, 0.5
	b := h.spawnMob(players, entityBat, 0.5, 180, 0.5)
	rested := false
	for i := 0; i < 2000 && !rested; i++ {
		h.batStep(players, b)
		rested = b.batResting
	}
	if !rested || !h.batStep(players, b) {
		t.Fatal("under a ceiling the bat settles and hangs")
	}
	pl.x = 2.5
	if h.batStep(players, b) || b.batResting {
		t.Fatal("a player within four wakes it")
	}
	if _, ok := parrotImitates[entityZombie]; !ok {
		t.Fatal("parrots know the zombie")
	}
	if _, ok := parrotImitates[entityCow]; ok {
		t.Fatal("but not the cow")
	}
	p := h.spawnMob(players, entityParrot, 0.5, 180, 0.5)
	h.spawnMob(players, entityZombie, 5.5, 180, 0.5)
	h.gridDirty()
	squawked := false
	for i := 0; i < 20000 && !squawked; i++ {
		squawked = h.parrotImitateTick(players, p)
	}
	if !squawked {
		t.Fatal("with a zombie about, the parrot mimics it eventually")
	}
}
