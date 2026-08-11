package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Fullness belongs in exactly two places in vanilla: it filters ADOPTION
// (BeeLocateHiveGoal.findNearbyHivesWithSpace) and it is re-tested on ARRIVAL
// (BeeEnterHiveGoal.canBeeUse, which drops the hive if it is full). It is not
// consulted while travelling — BeeGoToHiveGoal.canBeeUse never looks at it.

// fillHive parks n idle occupants in a hive.
func fillHive(h *hub, p blockPos, n int) {
	h.registerHive(p)
	for i := 0; i < n; i++ {
		h.hives[p] = append(h.hives[p], hiveOccupant{SecsLeft: 1 << 20})
	}
}

// A bee whose hive filled up while it was out still flies home. Before this it
// set no goal at all and wandered off with its nectar.
func TestBeeWithNectarStillFliesToAFullHive(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	fillHive(h, hive, beeMaxOccupants)

	h.updateBee(players, m, true, false)

	if m.beeGoalKind != beeGoalKindHive {
		t.Errorf("goal %d, want the hive: a full hive is still the bee's hive", m.beeGoalKind)
	}
	if !m.beeHasHome || m.beeHome != hive {
		t.Error("the bee gave up its hive from a distance just because it was full")
	}
}

// …and it finds out on arrival, dropping the hive rather than queueing.
func TestArrivingAtAFullHiveDropsIt(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	m.x, m.y, m.z = float64(hive.x)+0.5, float64(hive.y), float64(hive.z)+0.5 // right at it
	fillHive(h, hive, beeMaxOccupants)

	h.updateBee(players, m, true, false)

	if m.beeHasHome {
		t.Error("the bee reached a full hive and kept it as home; vanilla nulls hivePos")
	}
	if m.beeHiveBanned(hive) {
		t.Error("a full hive was blacklisted — it is busy, not unreachable")
	}
	if _, still := h.mobs[m.eid]; !still {
		t.Error("the bee got into a full hive")
	}
}

// Room appears while the bee is on its way: it goes in, no re-adoption needed.
func TestABeeEntersOnceTheHiveHasRoomAgain(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	m.x, m.y, m.z = float64(hive.x)+0.5, float64(hive.y), float64(hive.z)+0.5
	fillHive(h, hive, beeMaxOccupants)

	h.updateBee(players, m, true, false)
	if m.beeHasHome {
		t.Fatal("expected the full hive to be dropped first")
	}
	m.beeHome, m.beeHasHome = hive, true // it re-adopts once there is space
	h.hives[hive] = h.hives[hive][:beeMaxOccupants-1]

	h.updateBee(players, m, true, false)
	if _, still := h.mobs[m.eid]; still {
		t.Error("a hive with room did not take the bee in")
	}
}

// A hive that is gone is a different matter from one that is full: the bee
// drops it wherever it happens to be (Bee.isHiveValid).
func TestABrokenHiveStopsBeingHome(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	h.world.SetBlock(hive.x, hive.y, hive.z, worldgen.Air)

	h.updateBee(players, m, true, false)
	if m.beeHasHome && m.beeHome == hive {
		t.Error("a hive that was broken is still the bee's home")
	}
}

// BeeLocateHiveGoal.canBeeUse: only a HOMELESS bee looks, and only once every
// remainingCooldownBeforeLocatingNewHive.
func TestHiveSearchIsRateLimited(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	m.beeHasHome = false // homeless, looking
	fillHive(h, hive, beeMaxOccupants)

	// Nowhere to live: the first pass spends the search, the next few must not.
	h.updateBee(players, m, true, false)
	if m.beeLocateCD != beeLocateCD {
		t.Fatalf("locate cooldown %d after a search, want %d", m.beeLocateCD, beeLocateCD)
	}
	for i := 0; i < beeLocateCD-1; i++ {
		h.updateBee(players, m, true, false)
		if m.beeHasHome {
			t.Fatal("found a home in a hive with no room")
		}
	}
	if m.beeLocateCD != 1 {
		t.Errorf("cooldown %d after %d passes, want it counting down", m.beeLocateCD, beeLocateCD-1)
	}
	// Room appears; the search is allowed again once the cooldown runs out.
	h.hives[hive] = nil
	for i := 0; i < 3 && !m.beeHasHome; i++ {
		h.updateBee(players, m, true, false)
	}
	if !m.beeHasHome {
		t.Error("the bee never resumed looking after the cooldown expired")
	}
}

// BeeLocateHiveGoal.start: when every hive with space is blacklisted the
// blacklist is cleared and the nearest taken anyway, so a bee is never
// permanently homeless because of a blockage that has since gone.
func TestABeeRecoversWhenEveryHiveIsBlacklisted(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	m.beeHasHome = false
	h.beeBanHive(m, hive)
	if !m.beeHiveBanned(hive) {
		t.Fatal("the hive was not blacklisted")
	}

	p, ok := h.findHiveFor(m)
	if !ok || p != hive {
		t.Fatalf("findHiveFor = (%v, %v); the last hive standing should be taken anyway", p, ok)
	}
	if m.beeHiveBanned(hive) {
		t.Error("the blacklist was not cleared when it was the only thing left")
	}
}

// A hive with room and no blacklist entry still wins over a blacklisted one.
func TestAnUnblacklistedHiveIsPreferred(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeHasHome = false

	other := blockPos{6, 78, 2} // nearer than `hive` at x=20
	h.world.SetBlock(other.x, other.y, other.z, beeNestMin)
	h.registerHive(other)
	h.beeBanHive(m, other)

	p, ok := h.findHiveFor(m)
	if !ok || p != hive {
		t.Errorf("findHiveFor = (%v, %v), want the un-blacklisted hive %v", p, ok, hive)
	}
	if !m.beeHiveBanned(other) {
		t.Error("the blacklist was cleared even though a clean hive was available")
	}
}

// The trip deadline is MAX_TRAVELLING_TICKS in SECONDS, because the counter
// ticks once per second with the rest of updateBee's clocks. Measuring it in
// mob-updates made it twenty minutes instead of two.
func TestTripDeadlineIsTwoMinutes(t *testing.T) {
	if want := 2400 / 20; beeTravelGiveUp != want {
		t.Errorf("beeTravelGiveUp = %d passes (%d s), want %d (120 s)",
			beeTravelGiveUp, beeTravelGiveUp, want)
	}
}
