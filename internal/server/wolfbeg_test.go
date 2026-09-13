package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestWolfBegs: a wolf near a player holding a bone tilts its head and
// watches them for forty to eighty ticks; putting the bone away ends it.
func TestWolfBegs(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 3, 180, 0
	w := h.spawnMob(players, entityWolf, 0.5, 180, 0.5)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBone, count: 1}
	for i := 0; i < 5 && !w.begging; i++ {
		h.begStep(players, w)
	}
	if !w.begging || w.begTicks < begLookMin || w.begTicks > begLookMin+begLookRandom {
		t.Fatalf("the wolf should beg: %v %d", w.begging, w.begTicks)
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{}
	h.begStep(players, w)
	if w.begging || w.begTicks != 0 {
		t.Fatal("the bone put away ends the beg")
	}
	// Any wolf food works; a stone does not.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["cooked_beef"], count: 1}
	if !wolfWantsToBeg(pl) {
		t.Fatal("cooked beef is wolf food")
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["stone"], count: 1}
	if wolfWantsToBeg(pl) {
		t.Fatal("stone is not")
	}
}
