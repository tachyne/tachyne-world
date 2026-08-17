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
// the death path handed out its loot and experience for simply waiting.
func TestAWardenDiggingAwayLeavesNothingBehind(t *testing.T) {
	h, players := pushWorld(t)
	m := putMob(t, h, players, entityWarden, 0.5, 70, 0.5)
	m.digClock = wardenDigAwayUpd - 1
	itemsBefore, orbsBefore := len(h.items), len(h.orbs)

	h.wardenTick(players, m) // no players in range: this is the update it leaves on

	if _, still := h.mobs[m.eid]; still {
		t.Fatal("the warden did not dig away")
	}
	if len(h.items) != itemsBefore {
		t.Errorf("%d items dropped, want none — digging away is not a death",
			len(h.items)-itemsBefore)
	}
	if len(h.orbs) != orbsBefore {
		t.Errorf("%d experience orbs dropped, want none", len(h.orbs)-orbsBefore)
	}
}

// It only leaves once the clock is full, not before.
func TestAWardenStaysUntilItsClockRunsOut(t *testing.T) {
	h, players := pushWorld(t)
	m := putMob(t, h, players, entityWarden, 0.5, 70, 0.5)
	for i := 0; i < wardenDigAwayUpd-1; i++ {
		h.wardenTick(players, m)
	}
	if _, still := h.mobs[m.eid]; !still {
		t.Errorf("the warden left after %d updates, before its %d are up",
			wardenDigAwayUpd-1, wardenDigAwayUpd)
	}
}

// A warden that is killed still drops what it should — the change is only to
// the digging-away path.
func TestAKilledWardenStillDrops(t *testing.T) {
	h, players := pushWorld(t)
	m := putMob(t, h, players, entityWarden, 0.5, 70, 0.5)
	before := len(h.items)
	h.despawnMob(players, m)
	if len(h.items) <= before {
		t.Error("a killed warden dropped nothing; only the dig-away should be silent")
	}
}
