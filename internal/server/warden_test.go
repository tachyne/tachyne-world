package server

import "testing"

// The warden's two timers, and what happens when it gives up and leaves.

// WardenAi.DIGGING_COOLDOWN is 1200 ticks; SonicBoom runs for 60 and then sets
// a 40-tick cooldown, so booms land 100 apart. Both are counted here in mob
// updates, which is what made them easy to get wrong: an update is two ticks.
func TestWardenTimersMatchVanilla(t *testing.T) {
	if got := wardenDigAwayUpd * mobMoveInterval; got != 1200 {
		t.Errorf("digs away after %d ticks, want vanilla's 1200", got)
	}
	if got := wardenSonicCD * mobMoveInterval; got != 100 {
		t.Errorf("booms every %d ticks, want vanilla's 60 duration + 40 cooldown", got)
	}
}

// Digging.stop removes the warden as DISCARDED — not a death. Going through
// the death path handed out its loot and experience for simply waiting. It
// does not vanish on the spot either: the dig clock running out starts the
// DIGGING animation, and it leaves at the end of it (WardenAi's DIG activity,
// DIGGING_DURATION 100 ticks).
func TestAWardenDiggingAwayLeavesNothingBehind(t *testing.T) {
	h, players := pushWorld(t)
	m := putMob(t, h, players, entityWarden, 0.5, 70, 0.5)
	m.digClock = wardenDigAwayUpd - 1
	itemsBefore, orbsBefore := len(h.items), len(h.orbs)

	h.wardenTick(players, m) // no players in range: this is the update it gives up on

	if _, still := h.mobs[m.eid]; !still {
		t.Fatal("it should burrow first, not vanish on the spot")
	}
	if m.wardenPose != poseDigging {
		t.Fatalf("it should be digging, got pose %d", m.wardenPose)
	}
	for i := 0; i < wardenDigUpd; i++ {
		h.wardenTick(players, m)
	}
	if _, still := h.mobs[m.eid]; still {
		t.Fatalf("the warden did not dig away after %d updates", wardenDigUpd)
	}
	if len(h.items) != itemsBefore {
		t.Errorf("%d items dropped, want none — digging away is not a death",
			len(h.items)-itemsBefore)
	}
	if len(h.orbs) != orbsBefore {
		t.Errorf("%d experience orbs dropped, want none", len(h.orbs)-orbsBefore)
	}
}
