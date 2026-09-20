package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// Putting something in a pot puffs seven dust motes off the rim
// (DecoratedPotBlock.useItemOn), in the pot's own dimension.
func TestPotInsertPuffsDust(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pos := blockPos{4, 70, 4}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["emerald"], count: 3}

	pl.p.out = make(chan outPkt, 64)
	if !h.usePot(players, pl, pos) {
		t.Fatal("the pot should have taken the emerald")
	}
	close(pl.p.out)
	seen, others := 0, 0
	for pk := range pl.p.out {
		e, ok := pk.ev.(attachproto.Particles)
		if !ok {
			continue
		}
		if e.PID != particleDustPlume {
			others++
			continue
		}
		seen++
		if e.Count != potPlumeCount {
			t.Errorf("seven motes, got %d", e.Count)
		}
		if e.Y != float64(pos.y)+1.2 {
			t.Errorf("they come off the rim (y+1.2), got %v", e.Y)
		}
	}
	if seen != 1 {
		t.Errorf("one dust plume per insert, got %d (and %d other particle events)", seen, others)
	}
}
