package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestSkeletonDrawsBow: a skeleton's hand goes active in the twenty ticks
// before its shot and idles after; a pillager's while its crossbow loads.
func TestSkeletonDrawsBow(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 8.5, 180, 0.5
	s := h.spawnMob(players, entitySkeleton, 0.5, 180, 0.5)
	s.attackCD = 19
	h.bowDrawTick(players, s)
	if s.handActive {
		t.Fatal("thirty-eight ticks out, the bow hangs")
	}
	s.attackCD = 10
	h.bowDrawTick(players, s)
	if !s.handActive {
		t.Fatal("twenty ticks out, it draws")
	}
	pl.x = 40
	h.bowDrawTick(players, s)
	if s.handActive {
		t.Fatal("no target, no draw")
	}
	pl.x = 6.5
	p := h.spawnMob(players, entityPillager, 0.5, 180, 4.5)
	h.pillagerTick(players, p)
	if p.cbState != cbCharging || !p.handActive {
		t.Fatalf("a loading crossbow is a busy hand: state %d active %v", p.cbState, p.handActive)
	}
	for i := 0; i < 13 && p.cbState == cbCharging; i++ {
		h.pillagerTick(players, p)
	}
	if p.handActive {
		t.Fatal("loaded, the hand is free")
	}
}
