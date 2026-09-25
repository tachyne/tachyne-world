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

// Bat.customServerAiStep: a flying bat in the open steers at its own target
// cell — within six blocks sideways, two below to three above — and moves
// through the mob update toward it rather than drifting at a hover height.
func TestBatFliesToItsTarget(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			for y := 170; y <= 200; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	players := map[int32]*tracked{}
	b := h.spawnMob(players, entityBat, 0.5, 185, 0.5)
	h.batStep(players, b)
	if !b.batHasT || abs(b.batT.x) > 6 || abs(b.batT.z) > 6 || b.batT.y < 183 || b.batT.y > 188 {
		t.Fatalf("target cell %v out of the bat's range", b.batT)
	}
	moved := false
	for i := 0; i < 40; i++ {
		x0, z0 := b.x, b.z
		h.updateMobs(players)
		if b.x != x0 || b.z != z0 {
			moved = true
		}
	}
	if !moved {
		t.Fatal("the bat never moved")
	}
}
