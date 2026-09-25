package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// /tp moves a mob into the destination's dimension: a pig in the overworld
// sent to a Nether player lands in the Nether beside them, its seat left
// behind; and a player sent to a mob in another dimension goes there.
func TestTeleportMobAcrossDimensions(t *testing.T) {
	h := newHub(world.New(1))
	me, you := survPlayer(h), survPlayer(h)
	you.p.eid = me.p.eid + 1
	you.p.name = "you"
	players := map[int32]*tracked{me.p.eid: me, you.p.eid: you}
	h.playersRef = players
	h.allocEID()
	h.allocEID()
	pig := h.spawnMob(players, entityPig, 3.5, 80, 3.5)
	you.dim, you.x, you.y, you.z = dimNether, 40.5, 64, 40.5
	h.onTeleportTargets(players, evTeleportTargets{by: me.p.eid, targets: "@e[type=pig]", dest: "you"})
	if pig.dim != dimNether || pig.x != 40.5 || pig.z != 40.5 {
		t.Fatalf("the pig is in dim %d at %.1f,%.1f; want the Nether beside you", pig.dim, pig.x, pig.z)
	}
	h.onTeleportTo(players, evTeleportTo{eid: me.p.eid, target: "@e[type=pig]"})
	if me.p.pendingDim.Load() != int32(dimNether) || !me.p.pendingDestOK {
		t.Fatalf("the caller was not sent to the pig's dimension: pending %d", me.p.pendingDim.Load())
	}
}
