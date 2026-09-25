package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The creaking is the only mob whose life is a property of a block. Every test
// here is about that link rather than about the mob.

// paleTrunk builds a scrap of pale oak with a heart in it and registers the
// heart, returning the hub, the heart position and its link.
func paleTrunk(t *testing.T) (*hub, blockPos, *heartLink) {
	t.Helper()
	h := newHub(world.New(3))
	pos := blockPos{100, 80, 100}
	for dy := -3; dy <= 3; dy++ {
		h.world.SetBlock(pos.x, pos.y+dy, pos.z, worldgen.PaleOakLog)
	}
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.CreakingHeartDormant)
	h.hearts = map[simPos]*heartLink{{blockPos: pos}: {pos: pos}}
	return h, pos, h.hearts[simPos{blockPos: pos}]
}

// nightHub sets the clock after dusk, which is the only time a heart is awake.
func nightHub(h *hub) {
	h.dayTime.Store(15000)
	h.rules.DoMobSpawning = true
	h.rules.Difficulty = diffNormal
}

func TestHeartWakesAtNightAndSleepsByDay(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	nightHub(h)

	link.nextAt = 0
	h.updateHearts(players)
	if got := h.world.At(pos.x, pos.y, pos.z); got != worldgen.CreakingHeartAwake {
		t.Errorf("a heart should be awake at night, state %d", got)
	}

	h.dayTime.Store(1000) // morning
	link.nextAt = 0
	h.updateHearts(players)
	if got := h.world.At(pos.x, pos.y, pos.z); got != worldgen.CreakingHeartDormant {
		t.Errorf("a heart should sleep by day, state %d", got)
	}
}

// Cut the logs out from under a heart and it uproots — which is also what stops
// it spawning anything.
func TestHeartUprootsWithoutItsLogs(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	nightHub(h)
	h.world.SetBlock(pos.x, pos.y+1, pos.z, worldgen.Air)

	link.nextAt = 0
	h.updateHearts(players)
	if got := h.world.At(pos.x, pos.y, pos.z); got != worldgen.CreakingHeartUproot {
		t.Errorf("a heart with no logs should uproot, state %d", got)
	}
}

// A creaking with a standing heart cannot be hurt by anything ordinary, and the
// blow is recorded for the heart to answer.
func TestHeartBoundCreakingIsInvulnerable(t *testing.T) {
	h := newHub(world.New(3))
	players := map[int32]*tracked{}
	m := h.spawnHostile(players, entityCreaking, 0, 0)
	if m == nil {
		t.Fatal("creaking spawn returned nil")
	}
	m.heartBound = true
	before := m.health

	for _, dt := range []dmgType{dtPlayerAttack, dtArrow, dtExplosion, dtLava, dtFall, dtMagic} {
		m.health = before
		m.heartHit = false
		m.hurtKind(50, dt)
		if m.health != before {
			t.Errorf("%s should not hurt a heart-bound creaking (health %d -> %d)",
				dt.name(), before, m.health)
		}
		if !m.heartHit {
			t.Errorf("%s should have been recorded for the heart to answer", dt.name())
		}
	}

	// …but /kill still works, because generic_kill bypasses invulnerability.
	m.health = before
	m.hurtKind(50, dtGenericKill)
	if m.health >= before {
		t.Error("generic_kill must get through — otherwise a creaking is unkillable by command")
	}
}

// Without a heart it is an ordinary one-health mob.
func TestUnboundCreakingTakesDamageNormally(t *testing.T) {
	h := newHub(world.New(3))
	m := h.spawnHostile(map[int32]*tracked{}, entityCreaking, 0, 0)
	if m == nil {
		t.Fatal("creaking spawn returned nil")
	}
	before := m.health
	m.hurtKind(50, dtPlayerAttack)
	if m.health >= before {
		t.Error("a creaking with no heart should take an ordinary blow")
	}
}

// The signature mechanic: watched, it cannot move.
func TestCreakingFreezesWhileWatched(t *testing.T) {
	h := newHub(world.New(3))
	m := h.spawnHostile(map[int32]*tracked{}, entityCreaking, 0, 0)
	if m == nil {
		t.Fatal("creaking spawn returned nil")
	}
	m.x, m.y, m.z = 0, 80, 0

	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 0, 80, -8 // eight blocks away on -z
	players := map[int32]*tracked{pl.p.eid: pl}

	pl.yaw, pl.pitch = 0, 0 // look vector (-sin0, cos0) = +z, straight at it
	if !h.creakingFrozen(players, m) {
		t.Error("a creaking should freeze while a player looks at it")
	}
	pl.yaw = 180 // turn around
	if h.creakingFrozen(players, m) {
		t.Error("a creaking should move once the player looks away")
	}
	// Out of range it does not care where anyone is looking.
	pl.yaw = 0
	pl.z = -40
	if h.creakingFrozen(players, m) {
		t.Error("a distant player should not freeze it")
	}
}

// Creative and dead players are not watchers — the freeze is a survival threat,
// and a corpse staring at it should not pin it in place.
func TestOnlyLivingSurvivalPlayersFreezeIt(t *testing.T) {
	h := newHub(world.New(3))
	m := h.spawnHostile(map[int32]*tracked{}, entityCreaking, 0, 0)
	if m == nil {
		t.Fatal("creaking spawn returned nil")
	}
	m.x, m.y, m.z = 0, 80, 0
	pl := testTracked()
	pl.x, pl.y, pl.z, pl.yaw = 0, 80, -8, 0
	players := map[int32]*tracked{pl.p.eid: pl}

	pl.gamemode = gmCreative
	if h.creakingFrozen(players, m) {
		t.Error("a creative player should not freeze a creaking")
	}
	pl.gamemode = gmSurvival
	pl.dead = true
	if h.creakingFrozen(players, m) {
		t.Error("a dead player should not freeze a creaking")
	}
}

// Break the heart and the creaking it sent out comes apart.
func TestBreakingTheHeartKillsItsCreaking(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	m := h.spawnHostile(players, entityCreaking, 0, 0)
	if m == nil {
		t.Fatal("creaking spawn returned nil")
	}
	m.home = pos
	m.heartBound = true
	link.creaking = m.eid

	h.updateCreakings(players) // still linked
	if _, alive := h.mobs[m.eid]; !alive {
		t.Fatal("the creaking should be alive while its heart stands")
	}

	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	link.nextAt = 0
	h.updateHearts(players)
	if _, alive := h.mobs[m.eid]; alive {
		t.Error("breaking the heart should have taken the creaking with it")
	}
}

// The state decoder has to read the axis/state/natural packing correctly, or
// every heart reads as dormant and none of the above works.
func TestCreakingHeartStateDecoding(t *testing.T) {
	for _, c := range []struct {
		state      uint32
		awake, isH bool
		what       string
	}{
		{worldgen.CreakingHeartDormant, false, true, "dormant"},
		{worldgen.CreakingHeartAwake, true, true, "awake"},
		{worldgen.CreakingHeartUproot, false, true, "uprooted"},
		{worldgen.PaleOakLog, false, false, "a log is not a heart"},
		{worldgen.Air, false, false, "air is not a heart"},
	} {
		awake, ok := creakingHeartState(c.state)
		if ok != c.isH || awake != c.awake {
			t.Errorf("%s: awake=%v ok=%v, want awake=%v ok=%v", c.what, awake, ok, c.awake, c.isH)
		}
	}
}

// creakingField is a summoned creaking (no heart) on a stone floor at y=180
// with one survival player eight blocks off on -z, facing away from it.
func creakingField(t *testing.T) (*hub, map[int32]*tracked, *mob, *tracked) {
	t.Helper()
	h, players := preyFixture(t)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.yaw = 0.5, 180, -7.5, 180 // looking away, along -z
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityCreaking, 0.5, 180, 0.5)
	return h, players, m, pl
}

func stepCreaking(h *hub, players map[int32]*tracked, n int) {
	for i := 0; i < n; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateCreakings(players)
		h.updateMobs(players)
	}
}

// Creaking.checkCanMove: a creaking nobody has looked at is not hunting —
// StartAttacking waits for isActive. It used to hunt anyone within 32 like
// any other monster.
func TestIdleCreakingDoesNotHunt(t *testing.T) {
	h, players, m, pl := creakingField(t)
	stepCreaking(h, players, 40)
	if h.mobs[m.eid] == nil {
		t.Fatal("a summoned creaking with no heart should stay; only a heart-bound one dies without it")
	}
	if m.creakActive || m.hasTarget || m.targetEID != 0 {
		t.Fatalf("unwatched, it should stay idle (active %v, hunting %v, target %d)", m.creakActive, m.hasTarget, m.targetEID)
	}
	if pl.health < 20 {
		t.Errorf("an idle creaking hurt the player (%v)", pl.health)
	}
}

// A look within 12 blocks wakes it: that player becomes its target and it
// freezes; looking away lets it come for them; the player leaving its
// follow range puts it back to sleep.
func TestLookingAtACreakingWakesIt(t *testing.T) {
	h, players, m, pl := creakingField(t)
	pl.yaw = 0 // now facing it
	stepCreaking(h, players, 1)
	if !m.creakActive || m.targetEID != pl.p.eid || !m.frozen {
		t.Fatalf("a look should wake it on that player and hold it (active %v target %d frozen %v)",
			m.creakActive, m.targetEID, m.frozen)
	}
	pl.yaw = 180
	stepCreaking(h, players, 2)
	if m.frozen || !m.hasTarget {
		t.Fatalf("unwatched and awake it should hunt (frozen %v hunting %v)", m.frozen, m.hasTarget)
	}
	pl.z = -60 // out of its 32-block follow range
	stepCreaking(h, players, 1)
	if m.creakActive || m.hasTarget {
		t.Errorf("with nobody in range it should go idle (active %v hunting %v)", m.creakActive, m.hasTarget)
	}
}
