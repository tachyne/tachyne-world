package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// Particles go to the dimension they happened in. spawnParticles used to pass
// dimension 0 whatever the caller was doing, and toNearbyEv filters viewers by
// dimension — so a crit landed in the Nether reached nobody who could see it,
// and an overworld player standing at the same coordinates got a burst out of
// nowhere. A wind burst even carried an `if dim == 0` guard to suppress the
// wrong-dimension particle rather than send the right one.
func TestParticlesGoToTheirOwnDimension(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	over := testTracked()
	over.p.name, over.p.eid = "Over", 500
	over.dim, over.x, over.y, over.z = dimOverworld, 8, 70, 8
	over.p.out = make(chan outPkt, 32)

	under := testTracked()
	under.p.name, under.p.eid = "Under", 501
	under.dim, under.x, under.y, under.z = dimNether, 8, 70, 8
	under.p.out = make(chan outPkt, 32)

	players[over.p.eid], players[under.p.eid] = over, under

	h.spawnParticles(players, dimNether, particleCrit, 8, 71, 8, 0.4, 0.2, 8)

	close(over.p.out)
	close(under.p.out)
	count := func(ch chan outPkt) int {
		n := 0
		for pk := range ch {
			if _, ok := pk.ev.(attachproto.Particles); ok {
				n++
			}
		}
		return n
	}
	if got := count(under.p.out); got != 1 {
		t.Errorf("the player in the Nether should see it, got %d particle events", got)
	}
	if got := count(over.p.out); got != 0 {
		t.Errorf("the player in the overworld should see nothing, got %d particle events", got)
	}
}
