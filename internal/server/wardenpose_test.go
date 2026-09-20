package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func wardenSetup(t *testing.T) (*hub, map[int32]*tracked, *mob, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	pl.p.name, pl.p.eid = "Prey", 500 // well clear of the mob eids
	pl.x, pl.y, pl.z = 10, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnMob(players, entityWarden, 0.5, 70, 0.5)
	return h, players, m, pl
}

// A warden a shrieker calls rises out of the ground first: it holds the
// emerging pose, stands still, and only then behaves like a warden.
func TestWardenEmergesBeforeItHunts(t *testing.T) {
	h, players, m, _ := wardenSetup(t)
	h.wardenEmerge(players, m)
	if m.wardenPose != poseEmerging || m.wardenPoseLeft != wardenEmergeUpd {
		t.Fatalf("it should be emerging for %d updates, got pose %d left %d",
			wardenEmergeUpd, m.wardenPose, m.wardenPoseLeft)
	}
	m.vx, m.vz = 1, 1
	if !h.wardenStep(players, m) || m.vx != 0 || m.vz != 0 {
		t.Error("it should be rooted to the spot while it emerges")
	}
	for i := 0; i < wardenEmergeUpd-1; i++ {
		if !h.wardenPoseTick(players, m) {
			t.Fatalf("the animation ended early, at %d of %d", i, wardenEmergeUpd)
		}
	}
	h.wardenPoseTick(players, m) // the last one
	if m.wardenPose != 0 || m.wardenPoseLeft != 0 {
		t.Fatalf("it should be standing again, got pose %d left %d", m.wardenPose, m.wardenPoseLeft)
	}
	if h.wardenStep(players, m) {
		t.Error("and moving again")
	}
}

// It sniffs the air before it commits to anyone, then roars at whoever it
// settles on — the roar target only becomes the attack target when the roar
// is over, so no sonic boom lands during it.
func TestWardenSniffsThenRoars(t *testing.T) {
	h, players, m, pl := wardenSetup(t)
	h.wardenTick(players, m)
	if m.wardenPose != poseSniffing {
		t.Fatalf("with nobody to be angry at it should sniff first, got pose %d", m.wardenPose)
	}
	if m.wardenSniffCD < wardenSniffCDMin {
		t.Errorf("sniffing should start its cooldown, got %d", m.wardenSniffCD)
	}
	for m.wardenPoseLeft > 0 {
		h.wardenTick(players, m)
	}
	if m.wardenPose != 0 {
		t.Fatalf("the sniff should have ended, got pose %d", m.wardenPose)
	}
	// Now it fixes on the player, and roars before it comes for them.
	h.wardenTick(players, m)
	if m.wardenPose != poseRoaring || m.wardenPoseLeft != wardenRoarUpd {
		t.Fatalf("it should roar on fixing a target, got pose %d left %d", m.wardenPose, m.wardenPoseLeft)
	}
	if m.wardenTarget != pl.p.eid {
		t.Errorf("it should have fixed on the player, got %d", m.wardenTarget)
	}
	if m.sonicCD != 0 {
		t.Error("no sonic boom lands during the roar")
	}
	// It does not roar again at the same quarry.
	for m.wardenPoseLeft > 0 {
		h.wardenTick(players, m)
	}
	h.wardenTick(players, m)
	if m.wardenPose == poseRoaring {
		t.Error("it should not roar twice at the same target")
	}
}

// Left alone long enough it burrows away — a hundred ticks of digging, then
// it is discarded, which is not a death and so drops nothing.
func TestWardenBurrowsAwayRatherThanVanishing(t *testing.T) {
	h, players, m, pl := wardenSetup(t)
	delete(players, pl.p.eid) // nobody about at all
	m.digClock = wardenDigAwayUpd - 1
	h.wardenTick(players, m)
	if m.wardenPose != poseDigging || m.wardenPoseLeft != wardenDigUpd {
		t.Fatalf("it should dig for %d updates, got pose %d left %d",
			wardenDigUpd, m.wardenPose, m.wardenPoseLeft)
	}
	if h.mobs[m.eid] == nil {
		t.Fatal("it should still be there while it digs")
	}
	for i := 0; i < wardenDigUpd; i++ {
		h.wardenPoseTick(players, m)
	}
	if h.mobs[m.eid] != nil {
		t.Error("once it has dug itself in it should be gone")
	}
	if len(h.items) != 0 {
		t.Errorf("burrowing away is not dying: it should drop nothing, got %d items", len(h.items))
	}
}
