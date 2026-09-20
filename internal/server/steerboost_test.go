package server

import (
	"math"
	"testing"
)

// boostedPig puts a rider on a saddled pig with a carrot on a stick in hand.
func boostedPig(t *testing.T, etype int, item int32) (*hub, *tracked, map[int32]*tracked, *mob) {
	t.Helper()
	h, pl, players, m := ridingSetup(t, etype)
	m.saddled = true
	h.mountMob(players, pl, m)
	give(pl, item)
	return h, pl, players, m
}

// TestCarrotOnAStickBoostsAPig: using the stick rolls a sprint of 140-980
// ticks (ItemBasedSteering.boost) and charges the stick seven of its
// twenty-five durability points (FoodOnAStickItem's consumeItemDamage).
func TestCarrotOnAStickBoostsAPig(t *testing.T) {
	h, pl, players, m := boostedPig(t, entityPig, itemCarrotOnStick)
	h.steerBoost(players, pl, 0)
	if !m.boosting {
		t.Fatal("using a carrot on a stick should start the pig sprinting")
	}
	if m.boostTotal < boostTimeMin || m.boostTotal > boostTimeMin+boostTimeSpan-1 {
		t.Errorf("boost roll %d outside 140..980", m.boostTotal)
	}
	if s := pl.inv.slots[0]; s.item != itemCarrotOnStick || s.dmg != 7 {
		t.Errorf("the stick should take seven points of wear, got %+v", s)
	}
}

// TestSteerBoostNeedsTheRightMount: the stick only boosts the species it was
// made for, and only from the saddle — a carrot waved on foot, or from a
// strider's back, does nothing and costs nothing.
func TestSteerBoostNeedsTheRightMount(t *testing.T) {
	h, pl, players, m := boostedPig(t, entityStrider, itemCarrotOnStick)
	h.steerBoost(players, pl, 0)
	if m.boosting || pl.inv.slots[0].dmg != 0 {
		t.Errorf("a carrot must not boost a strider: boosting=%v stick=%+v", m.boosting, pl.inv.slots[0])
	}
	give(pl, itemWarpedFungusStick)
	h.steerBoost(players, pl, 0)
	if !m.boosting || pl.inv.slots[0].dmg != 1 {
		t.Errorf("a warped fungus boosts a strider for one point: boosting=%v stick=%+v", m.boosting, pl.inv.slots[0])
	}

	h2, pl2, players2, _ := ridingSetup(t, entityPig)
	give(pl2, itemCarrotOnStick)
	h2.steerBoost(players2, pl2, 0)
	if pl2.inv.slots[0].dmg != 0 {
		t.Error("a stick used on foot must not wear")
	}
}

// TestBoostRefusedWhileBoosting: ItemBasedSteering.boost returns false while a
// sprint is running, so the second use is free — and once the rolled time runs
// out the stick works again. The clock only turns while the mount is ridden.
func TestBoostRefusedWhileBoosting(t *testing.T) {
	h, pl, players, m := boostedPig(t, entityPig, itemCarrotOnStick)
	h.steerBoost(players, pl, 0)
	total := m.boostTotal
	h.steerBoost(players, pl, 0)
	if pl.inv.slots[0].dmg != 7 {
		t.Fatalf("a second boost mid-sprint must not wear the stick: %+v", pl.inv.slots[0])
	}
	for i := 0; i <= total; i++ {
		h.tickBoosts(players)
	}
	if m.boosting {
		t.Fatalf("the sprint should be over after %d ticks", total)
	}
	h.steerBoost(players, pl, 0)
	if !m.boosting || pl.inv.slots[0].dmg != 14 {
		t.Errorf("a spent sprint should take another boost: boosting=%v stick=%+v", m.boosting, pl.inv.slots[0])
	}
}

// TestBoostClockOnlyTurnsWhileRidden: vanilla ticks the steering from
// tickRidden, so a mount whose rider got off holds its sprint where it was.
func TestBoostClockOnlyTurnsWhileRidden(t *testing.T) {
	h, pl, players, m := boostedPig(t, entityPig, itemCarrotOnStick)
	h.steerBoost(players, pl, 0)
	h.tickBoosts(players)
	h.dismountMob(players, pl)
	at := m.boostTick
	for i := 0; i < 50; i++ {
		h.tickBoosts(players)
	}
	if m.boostTick != at || !m.boosting {
		t.Errorf("a dismounted mount's sprint should hold at %d, got %d (boosting=%v)", at, m.boostTick, m.boosting)
	}
}

// TestBoostFactorCurve: the sprint is a half-sine over the rolled time — 1× at
// both ends, 2.15× at the middle — and nothing at all when no sprint is on.
func TestBoostFactorCurve(t *testing.T) {
	m := &mob{}
	if got := m.boostFactor(); got != 1 {
		t.Errorf("an idle mount rides at %v, want 1", got)
	}
	m.boosting, m.boostTotal = true, 200
	for _, c := range []struct {
		at   int
		want float64
	}{{0, 1}, {100, 2.15}, {200, 1}} {
		m.boostTick = c.at
		if got := m.boostFactor(); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("boostFactor at %d/200 = %v, want %v", c.at, got, c.want)
		}
	}
}

// TestSpentStickBecomesAFishingRod: hurtAndConvertOnBreak does not destroy the
// stick — the carrot comes off and an undamaged fishing rod is left in hand.
func TestSpentStickBecomesAFishingRod(t *testing.T) {
	h, pl, players, m := boostedPig(t, entityPig, itemCarrotOnStick)
	pl.inv.slots[0].dmg = itemMaxDurability[itemCarrotOnStick] - 7 // one boost left
	h.steerBoost(players, pl, 0)
	if s := pl.inv.slots[0]; s.item != itemFishingRod || s.count != 1 || s.dmg != 0 {
		t.Fatalf("a spent carrot on a stick should leave a fresh fishing rod, got %+v", s)
	}
	if !m.boosting {
		t.Error("the boost that spent the stick should still have happened")
	}
}
