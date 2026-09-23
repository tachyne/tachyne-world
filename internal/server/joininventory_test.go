package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A creative player rejoins to the inventory the server kept for them, as
// every player does in vanilla (bug #24: the hotbar came back empty).
func TestCreativeRejoinGetsTheSavedInventory(t *testing.T) {
	for _, mode := range []int{gmCreative, gmSurvival, gmAdventure} {
		h := newHub(world.New(1))
		h.invs = newInvStore("")
		players := map[int32]*tracked{}
		p := newPlayer(h.allocEID(), "builder", [16]byte{9})
		saved := &tracked{p: p}
		initSurvival(saved)
		saved.inv.slots[0] = invStack{item: itemByName["observer"], count: 12}
		h.invs.record("builder", saved)

		h.onJoin(players, evJoin{p: p, x: 0.5, y: 80, z: 0.5, gamemode: mode})
		var got *attachproto.WindowItems
	drain:
		for {
			select {
			case o := <-p.out:
				if w, ok := o.ev.(attachproto.WindowItems); ok && w.ID == 0 {
					got = &w
				}
			default:
				break drain
			}
		}
		if got == nil {
			t.Fatalf("mode %d: no inventory was sent on join", mode)
		}
		if hot := got.Slots[36]; hot.ID != int32(itemByName["observer"]) || hot.Count != 12 {
			t.Fatalf("mode %d: hotbar slot 0 came back as %+v, want 12 observers", mode, hot)
		}
	}
}
