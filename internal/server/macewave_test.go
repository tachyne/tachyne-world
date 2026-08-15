package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// MaceItem.knockback — the shockwave a smash sends through everything near the
// entity it struck. It used to reach mobs only, and measured from the ATTACKER
// rather than the target.

func maceHub(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	return pushWorld(t)
}

// The wave is centred on what was HIT, not on who swung. The two only coincide
// when the attacker lands squarely on its target.
func TestTheShockwaveIsCentredOnTheTarget(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	// Struck entity ten blocks away; a bystander stands right beside IT.
	bystander := putMob(t, h, players, entityCow, 11, 70, 0)
	bystander.vx, bystander.vz = 0, 0

	h.smashAround(players, attacker, 10, 70, 0, 9999, 1)

	if bystander.vx == 0 && bystander.vz == 0 {
		t.Error("a mob beside the struck entity was not shoved; the wave is " +
			"centred on the attacker rather than the target")
	}
	if bystander.vx <= 0 {
		t.Errorf("shoved by vx=%.3f, want it pushed AWAY from the impact", bystander.vx)
	}
}

// Nothing outside the radius moves.
func TestTheShockwaveStopsAtItsRadius(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	far := putMob(t, h, players, entityCow, maceKnockRadius+2, 70, 0)

	h.smashAround(players, attacker, 0, 70, 0, 9999, 1)
	if far.vx != 0 || far.vz != 0 {
		t.Errorf("a mob %.1f blocks out was shoved", maceKnockRadius+2)
	}
}

// The struck entity itself is not shoved by its own shockwave — it already
// took the hit's knockback.
func TestTheStruckEntityIsSparedTheWave(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	target := putMob(t, h, players, entityCow, 1, 70, 0)

	h.smashAround(players, attacker, 1, 70, 0, target.eid, 1)
	if target.vx != 0 || target.vz != 0 {
		t.Error("the struck mob was shoved by its own shockwave")
	}
}

// getKnockbackPower doubles past a five-block fall.
func TestAHeavySmashHitsTwiceAsHard(t *testing.T) {
	shove := func(fall float64) float64 {
		h, players := maceHub(t)
		attacker := leashPlayer(t, h, players, 0, 70, 0)
		m := putMob(t, h, players, entityCow, 2, 70, 0)
		m.vx = 0
		h.smashAround(players, attacker, 0, 70, 0, 9999, fall)
		return math.Abs(m.vx)
	}
	light, heavy := shove(2), shove(maceHeavyThreshold+1)
	if heavy <= light {
		t.Errorf("heavy smash shoved %.4f, light %.4f — a fall past %.0f should double it",
			heavy, light, maceHeavyThreshold)
	}
}

// The whole point of #148: the wave catches PLAYERS. It used to walk h.mobs
// alone, so a mace smash in PvP moved nobody.
func TestTheShockwaveCatchesPlayers(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	bystander := leashPlayer(t, h, players, 2, 70, 0)
	drainEvents(bystander)

	h.smashAround(players, attacker, 0, 70, 0, 9999, 1)

	vel := lastVelocity(t, bystander)
	if vel.VX <= 0 {
		t.Errorf("bystander pushed vx=%.3f, want it away from the impact", vel.VX)
	}
	// A flat 0.7 up, not scaled by distance — the wave pops people into the air.
	if vel.VY != maceKnockPower {
		t.Errorf("vertical %.3f, want the flat %.3f", vel.VY, maceKnockPower)
	}
}

// The attacker is not thrown by their own smash.
func TestTheAttackerIsSparedTheirOwnWave(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	drainEvents(attacker)
	h.smashAround(players, attacker, 0, 70, 0, 9999, 1)
	if hasVelocity(attacker) {
		t.Error("the attacker was shoved by their own shockwave")
	}
}

// Spectators and creative fliers are not shoved (vanilla's predicate).
func TestSpectatorsAndCreativeAreSpared(t *testing.T) {
	for _, mode := range []int{gmSpectator, gmCreative} {
		h, players := maceHub(t)
		attacker := leashPlayer(t, h, players, 0, 70, 0)
		other := leashPlayer(t, h, players, 2, 70, 0)
		other.gamemode = mode
		drainEvents(other)

		h.smashAround(players, attacker, 0, 70, 0, 9999, 1)
		if hasVelocity(other) {
			t.Errorf("a player in mode %d was shoved by the shockwave", mode)
		}
	}
}

// Your own tamed pet rides it out.
func TestYourOwnPetIsSparedTheWave(t *testing.T) {
	h, players := maceHub(t)
	attacker := leashPlayer(t, h, players, 0, 70, 0)
	pet := putMob(t, h, players, entityWolf, 2, 70, 0)
	pet.tamed, pet.owner = true, attacker.p.eid
	pet.vx = 0

	h.smashAround(players, attacker, 0, 70, 0, 9999, 1)
	if pet.vx != 0 || pet.vz != 0 {
		t.Error("the attacker's own tamed wolf was thrown by their smash")
	}
}

// drainEvents empties a player's outgoing queue so a later read sees only what
// the code under test produced.
func drainEvents(t *tracked) {
	for {
		select {
		case <-t.p.out:
		default:
			return
		}
	}
}

// lastVelocity returns the most recent Velocity event queued for a player.
func lastVelocity(t *testing.T, tr *tracked) attachproto.Velocity {
	t.Helper()
	var got attachproto.Velocity
	found := false
	for {
		select {
		case pkt := <-tr.p.out:
			if v, ok := pkt.ev.(attachproto.Velocity); ok {
				got, found = v, true
			}
		default:
			if !found {
				t.Fatal("no Velocity event was sent")
			}
			return got
		}
	}
}

// hasVelocity reports whether any Velocity event was queued.
func hasVelocity(tr *tracked) bool {
	for {
		select {
		case pkt := <-tr.p.out:
			if _, ok := pkt.ev.(attachproto.Velocity); ok {
				return true
			}
		default:
			return false
		}
	}
}

// Player.awardKillScore: killing a player counts on BOTH scoreboard criteria.
// playerKillCount was bumped nowhere at all, so an objective tracking it read
// zero however the fight went; totalKillCount was kept for mobs and arrows but
// not for a player kill.
func TestAPlayerKillCountsOnBothCriteria(t *testing.T) {
	h, players := maceHub(t)
	killer := leashPlayer(t, h, players, 0, 70, 0)
	victim := leashPlayer(t, h, players, 1, 70, 0)
	h.sb.Objectives["pk"] = &sbObjective{Criteria: "playerKillCount", Title: "pk"}
	h.sb.Objectives["tk"] = &sbObjective{Criteria: "totalKillCount", Title: "tk"}
	h.rules.PvP = true

	// Put the victim one hit from death and swing until they drop.
	victim.health = 1
	for i := 0; i < 40 && !victim.dead; i++ {
		killer.lastAttack = 0 // clear the attack-cooldown scaling
		h.attackPlayer(players, killer.p.eid, victim.p.eid)
	}
	if !victim.dead {
		t.Fatal("the victim never died, so there is no kill to count")
	}
	if got := h.sb.Scores[killer.p.name]["pk"]; got != 1 {
		t.Errorf("playerKillCount = %d, want 1", got)
	}
	if got := h.sb.Scores[killer.p.name]["tk"]; got != 1 {
		t.Errorf("totalKillCount = %d, want 1 — a player kill counts there too", got)
	}
}
