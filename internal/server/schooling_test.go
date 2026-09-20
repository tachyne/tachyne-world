package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Cod form a shoal: one becomes the leader and the rest follow it, breaking
// off if they fall too far behind.
func TestFishSchool(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	a := h.spawnMob(players, entityCod, 0.5, 62, 0.5)
	b := h.spawnMob(players, entityCod, 2.5, 62, 0.5)
	for _, f := range []*mob{a, b} {
		f.setMoveSpeed(0.2)
		f.schoolNext = 0
	}
	if !h.schoolStep(players, b) {
		t.Fatal("a lone cod beside another should join it")
	}
	if b.schoolLeader != a.eid || a.schoolFollowers != 1 {
		t.Fatalf("b should follow a: leader=%d followers=%d", b.schoolLeader, a.schoolFollowers)
	}
	// Following steers it towards the leader.
	b.vx, b.vz = 0, 0
	a.x = 6.5
	h.schoolStep(players, b)
	if b.vx <= 0 {
		t.Errorf("a follower swims after its leader, vx=%v", b.vx)
	}
	// Out of range: the school breaks.
	a.x = 100
	if h.schoolStep(players, b) {
		t.Error("a fish eleven blocks behind stops following")
	}
	if b.schoolLeader != 0 {
		t.Error("…and forgets its leader")
	}
	// A leader does not follow anyone.
	a.x, a.schoolFollowers, a.schoolNext = 0.5, 1, 0
	if h.schoolStep(players, a) {
		t.Error("the leader of a school follows nobody")
	}
	// Removing a follower frees its place.
	b.schoolLeader, a.schoolFollowers = a.eid, 1
	h.removeMob(players, b)
	if a.schoolFollowers != 0 {
		t.Errorf("the school should shrink when a fish goes, got %d", a.schoolFollowers)
	}
}
