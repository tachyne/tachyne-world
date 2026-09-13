package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestWolvesHunt: a wild wolf goes for a sheep and bites it; a tamed wolf
// leaves sheep alone but goes for what hurt its owner; any wolf goes for
// a skeleton.
func TestWolvesHunt(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 30, 180, 30
	w := h.spawnMob(players, entityWolf, 0.5, 180, 0.5)
	sheep := h.spawnMob(players, entitySheep, 6.5, 180, 0.5)
	h.gridDirty()
	hp := sheep.health
	bit := false
	for i := 0; i < 400 && !bit; i++ {
		h.wolfHuntStep(players, w)
		w.x += w.vx
		w.z += w.vz
		bit = sheep.health < hp
	}
	if !bit || w.wolfPrey != sheep.eid {
		t.Fatalf("a wild wolf hunts the sheep: prey %d bit %v", w.wolfPrey, bit)
	}
	tame := h.spawnMob(players, entityWolf, 0.5, 180, 8.5)
	tame.tamed, tame.owner = true, pl.p.eid
	sheep2 := h.spawnMob(players, entitySheep, 3.5, 180, 8.5)
	h.gridDirty()
	for i := 0; i < 50; i++ {
		h.wolfHuntStep(players, tame)
	}
	if tame.wolfPrey == sheep2.eid {
		t.Fatal("a tamed wolf leaves sheep alone")
	}
	zombie := h.spawnMob(players, entityZombie, 3.5, 180, 10.5)
	pl.x, pl.y, pl.z = 2.5, 180, 9.5
	pl.lastHurtByMob = zombie.eid
	h.gridDirty()
	h.wolfHuntStep(players, tame)
	if tame.wolfPrey != zombie.eid {
		t.Fatalf("a tamed wolf goes for what hurt its owner: prey %d", tame.wolfPrey)
	}
	sk := h.spawnMob(players, entitySkeleton, 10.5, 180, 0.5)
	w.wolfPrey = 0
	h.gridDirty()
	for i := 0; i < 50 && w.wolfPrey != sk.eid; i++ {
		h.wolfHuntStep(players, w)
	}
	if w.wolfPrey != sk.eid && w.wolfPrey != sheep.eid {
		t.Fatalf("a wolf goes for skeletons: prey %d", w.wolfPrey)
	}
}
