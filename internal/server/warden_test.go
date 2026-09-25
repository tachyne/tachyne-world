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
	if got := (wardenSonicRunUpd + wardenSonicCoolUpd) * mobMoveInterval; got != 100 {
		t.Errorf("booms every %d ticks, want vanilla's 60 duration + 40 cooldown", got)
	}
	if got := (wardenMeleeCD + 1) * mobMoveInterval; got != 18 {
		t.Errorf("melee every %d ticks, want MeleeAttack.create(18)", got)
	}
}

// wardenFightFixture is a warden on a stone floor at y=180 already furious
// with one survival player.
func wardenFightFixture(t *testing.T, px, py, pz float64) (*hub, map[int32]*tracked, *mob, *tracked) {
	t.Helper()
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = px, py, pz
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityWarden, 0.5, 180, 0.5)
	m.wardenAnger = map[int32]int{pl.p.eid: wardenAngerMax}
	return h, players, m, pl
}

// Nobody it is angry at, nobody it attacks: a player it merely senses is
// something to sniff for, not a target. It used to fall back on the nearest
// player whenever the sniff was on cooldown, roar at them and give chase.
func TestWardenLeavesAPlayerItIsNotAngryAtAlone(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 11.5, 180, 0.5 // sensed, but outside the sniff's 6 blocks
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityWarden, 0.5, 180, 0.5)
	m.rest = 1 << 20 // no strolling up to them
	for i := 0; i < 150; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if m.wardenTarget != 0 || m.wardenPose == poseRoaring || m.hasTarget {
			t.Fatalf("update %d: it went for a player it was never angry at (target %d, pose %d, hunting %v)",
				i, m.wardenTarget, m.wardenPose, m.hasTarget)
		}
	}
	if pl.health < 20 {
		t.Errorf("the player took %v damage", 20-pl.health)
	}
}

// FIGHT: in reach it hits every 18 ticks (MeleeAttack.create(18)) and every
// blow puts the sonic boom back 40 ticks, so a target who stands and fights
// is never boomed. It never meleed at all before.
func TestWardenMeleesEveryEighteenTicks(t *testing.T) {
	h, players, m, pl := wardenFightFixture(t, 1.7, 180, 0.5)
	pl.health = 1000
	var hits []int
	for i := 0; i < 160; i++ {
		pl.x, pl.y, pl.z = m.x+1.2, 180, m.z // keep them in reach
		before := pl.health
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if pl.health < before {
			hits = append(hits, i)
		}
		pl.health = 1000
		if m.sonicRun > 0 {
			t.Fatalf("update %d: it charged a sonic boom at a player it can hit", i)
		}
	}
	if len(hits) < 5 {
		t.Fatalf("only %d blows landed: %v", len(hits), hits)
	}
	for k := 1; k < len(hits); k++ {
		if gap := (hits[k] - hits[k-1]) * mobMoveInterval; gap != 18 {
			t.Errorf("blows %d ticks apart (%v), want 18", gap, hits)
			break
		}
	}
}

// A target it cannot reach gets the boom — but only 200 ticks after the roar
// (setAttackTarget), a 34-tick charge before it lands, and 100 ticks between
// charges. It used to boom the instant it could, with no charge at all.
func TestWardenSonicBoomChargesAndWaits(t *testing.T) {
	h, players, m, pl := wardenFightFixture(t, 1.5, 186, 0.5) // up a pillar: out of melee reach
	pl.health = 1000
	roarEnd, lastPose := -1, false
	var starts, blasts []int
	for i := 0; i < 400; i++ {
		pl.x, pl.y, pl.z = m.x+1, 186, m.z
		before := pl.health
		run := m.sonicRun
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		roaring := m.wardenPose == poseRoaring
		if lastPose && !roaring && roarEnd < 0 {
			roarEnd = i
		}
		lastPose = roaring
		if run == 0 && m.sonicRun == 1 {
			starts = append(starts, i)
		}
		if pl.health < before {
			blasts = append(blasts, i)
		}
		pl.health = 1000
	}
	if roarEnd < 0 || len(starts) < 2 || len(blasts) < 2 {
		t.Fatalf("roar ended %d, charges %v, blasts %v", roarEnd, starts, blasts)
	}
	if wait := (starts[0] - roarEnd) * mobMoveInterval; wait != 200 {
		t.Errorf("first charge %d ticks after the roar, want 200", wait)
	}
	if lag := (blasts[0] - starts[0]) * mobMoveInterval; lag != 34 {
		t.Errorf("the blast landed %d ticks into the charge, want 34", lag)
	}
	if gap := (starts[1] - starts[0]) * mobMoveInterval; gap != 100 {
		t.Errorf("charges %d ticks apart, want 100", gap)
	}
}

// Warden.hurtServer: a direct blow from someone, with no attack target yet,
// makes them the target at once — no roar first.
func TestWardenStruckGoesStraightForTheAttacker(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 2.5, 180, 0.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityWarden, 0.5, 180, 0.5)
	h.attackMob(players, pl.p.eid, m.eid)
	if m.wardenTarget != pl.p.eid {
		t.Fatalf("the attacker should be its target, got %d", m.wardenTarget)
	}
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if m.wardenPose == poseRoaring {
		t.Error("struck directly, it should not stop to roar")
	}
	if !m.hasTarget {
		t.Error("it should be coming for the attacker")
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
