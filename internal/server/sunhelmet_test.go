package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestHelmetTakesTheSunAndPhantomFearsCats: a zombie in a leather cap does
// not ignite — the cap wears and finally breaks; a phantom drops its swoop
// when a cat is within sixteen.
func TestHelmetTakesTheSunAndPhantomFearsCats(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	cap := int32(itemByName["leather_helmet"])
	z.gear[0] = invStack{item: cap, count: 1}
	max := itemMaxDurability[cap]
	broke := false
	for i := 0; i < 200 && !broke; i++ {
		if !h.sunHelmetTakesIt(players, z) {
			t.Fatal("with a cap on, the helmet takes the sun")
		}
		broke = z.gear[0].item == 0
	}
	if !broke || max == 0 {
		t.Fatalf("the cap should wear through: max %d left %+v", max, z.gear[0])
	}
	if h.sunHelmetTakesIt(players, z) {
		t.Fatal("bare-headed, the sun finds skin")
	}
	ph := h.spawnMob(players, entityPhantom, 0.5, 190, 0.5)
	h.tick.Store(100)
	if h.phantomFearsCats(players, ph) {
		t.Fatal("no cat, no fear")
	}
	h.spawnMob(players, entityCat, 5.5, 180, 0.5)
	h.gridDirty()
	h.tick.Store(200)
	if !h.phantomFearsCats(players, ph) {
		t.Fatal("a cat within sixteen calls the swoop off")
	}
}
