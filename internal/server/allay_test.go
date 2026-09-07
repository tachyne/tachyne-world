package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Given a cobblestone, an allay collects matching drops and throws them back
// at its player once close; an empty hand takes everything back.
func TestAllayCollectsAndDelivers(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.MobGriefing = true
	a := h.spawnSpecies(players, entityAllay, 0, 0.5, 70, 0.5)
	if a == nil {
		t.Fatal("no allay")
	}
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	a.x, a.y, a.z = 0.5, 70, 0.5
	cobble := int32(itemByName["cobblestone"])
	pl.inv.slots[0] = invStack{item: cobble, count: 2}
	pl.p.setHotbarSlot(0, cobble)
	pl.p.held = 0
	if !h.tryAllay(players, pl, a) || a.held != cobble || a.owner != pl.p.eid || pl.inv.slots[0].count != 1 {
		t.Fatalf("the allay should take one cobblestone and like the giver: held=%d owner=%d left=%d", a.held, a.owner, pl.inv.slots[0].count)
	}
	// A matching drop ten blocks off: the allay heads for it.
	drop := h.spawnItem(players, cobble, 5, 10.5, 70, 0.5)
	if !h.allayStep(players, a) || a.vx <= 0 {
		t.Fatalf("the allay should fly toward the drop: vx=%.3f", a.vx)
	}
	// Beside it (drops settle onto the ground): it gathers the whole stack.
	a.x, a.y = 10, drop.y
	if !h.allayStep(players, a) || a.carry.item != cobble || a.carry.count != 5 || h.items[drop.eid] != nil {
		t.Fatalf("the allay should gather the stack: carry=%+v", a.carry)
	}
	// Back beside the player: it throws the stack down.
	a.x, a.y = 1.5, 70
	before := len(h.items)
	if !h.allayStep(players, a) || a.carry.item != 0 || len(h.items) != before+1 {
		t.Fatalf("the allay should deliver: carry=%+v items=%d→%d", a.carry, before, len(h.items))
	}
	// An empty hand takes the item back and the allay forgets its player.
	pl.inv.slots[0] = invStack{}
	pl.p.setHotbarSlot(0, 0)
	if !h.tryAllay(players, pl, a) || a.held != 0 || a.owner != 0 || pl.inv.slots[0].item != cobble {
		t.Fatalf("the allay should hand the cobblestone back: held=%d owner=%d slot=%+v", a.held, a.owner, pl.inv.slots[0])
	}
}
