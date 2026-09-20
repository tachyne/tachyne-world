package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestGuardianBeamAndElderAura(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.x, pl.y, pl.z = 5, 63, 5
	players := map[int32]*tracked{1: pl}

	// Elder guardian 7 blocks away: in beam range (> 3 blocks).
	g := h.spawnMob(players, entityElderGuardian, 12, 63, 5)
	if g == nil {
		t.Fatal("elder guardian spawn returned nil")
	}
	// The beam locks on first, then charges for the elder's 60-tick attack
	// duration before it lands — no instant hit.
	h.guardianTick(players, g)
	if g.beamTarget != pl.p.eid || pl.health < maxHealth {
		t.Fatalf("lock-on should hurt nobody yet: target %d health %v", g.beamTarget, pl.health)
	}
	for i := 0; i < 60 && pl.health >= maxHealth; i++ {
		h.guardianTick(players, g)
	}
	if pl.health >= maxHealth {
		t.Fatalf("elder guardian beam should damage the player, health=%v", pl.health)
	}
	if g.beamTarget != 0 {
		t.Fatal("firing the beam should let go of the target")
	}

	// Elder aura fires on the interval and lays Mining Fatigue.
	g.digClock = elderAuraUpd - 1
	h.guardianTick(players, g)
	if pl.hasEffect(effMiningFatigue) == 0 {
		t.Fatal("elder guardian should curse a nearby player with Mining Fatigue")
	}
}

// Guardian.hurtServer: a guardian whose spikes are out spits two points back
// at whoever melees it, and it does so before it takes the blow itself.
func TestGuardianSpikesBiteBackAtMelee(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 1, 63, 0
	players := map[int32]*tracked{pl.p.eid: pl}

	g := h.spawnMob(players, entityGuardian, 0, 63, 0)
	if g == nil {
		t.Fatal("guardian spawn returned nil")
	}
	// Holding station on the player it has already reached: nowhere left to
	// swim, so the spikes stand out.
	g.hasTarget, g.tx, g.tz = true, pl.x, pl.z
	if !guardianSpikesOut(g) {
		t.Fatal("a guardian on top of its target should have its spikes out")
	}

	before := pl.health
	h.attackMob(players, pl.p.eid, g.eid)
	if pl.health != before-guardianThornsDamage {
		t.Errorf("health %v after punching a guardian, want %v", pl.health, before-guardianThornsDamage)
	}
}

// …but one still swimming at you has them folded back, and punching it costs
// nothing.
func TestASwimmingGuardianHasNoSpikes(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 5, 63, 0
	players := map[int32]*tracked{pl.p.eid: pl}

	g := h.spawnMob(players, entityGuardian, 0, 63, 0)
	if g == nil {
		t.Fatal("guardian spawn returned nil")
	}
	g.hasTarget, g.tx, g.tz = true, pl.x, pl.z // five blocks still to cover
	if guardianSpikesOut(g) {
		t.Fatal("a guardian closing on its target should have its spikes folded back")
	}

	before := pl.health
	h.attackMob(players, pl.p.eid, g.eid)
	if pl.health != before {
		t.Errorf("health %v after punching a moving guardian, want it untouched at %v", pl.health, before)
	}
}

// The elder's spikes are worth the same two points, and an arrow never sets
// them off: vanilla reflects onto the DIRECT entity of the blow, which for a
// shot is the arrow and not the archer, so only the melee seam fires them.
func TestElderGuardianSpikesAndTheArcherItSpares(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 1, 63, 0
	players := map[int32]*tracked{pl.p.eid: pl}

	g := h.spawnMob(players, entityElderGuardian, 0, 63, 0)
	if g == nil {
		t.Fatal("elder guardian spawn returned nil")
	}
	g.rest = 1 // idling between strolls, no target: spikes out

	before := pl.health
	h.hurtMobOf(players, g, 3, dtArrow) // shot from across the room
	if pl.health != before {
		t.Errorf("health %v after SHOOTING an elder guardian, want it untouched at %v", pl.health, before)
	}
	h.attackMob(players, pl.p.eid, g.eid)
	if pl.health != before-guardianThornsDamage {
		t.Errorf("health %v after punching an elder guardian, want %v", pl.health, before-guardianThornsDamage)
	}
}
