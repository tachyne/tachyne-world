package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func angerRig(t *testing.T, etype int) (*hub, map[int32]*tracked, *tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	h.arrows = map[int32]*arrowEntity{}
	for x := -6; x <= 6; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	a, b := survPlayer(h), survPlayer(h)
	b.p.eid = a.p.eid + 1
	a.x, a.y, a.z = 3.5, 180, 0.5
	b.x, b.y, b.z = -2.5, 180, 0.5
	players := map[int32]*tracked{a.p.eid: a, b.p.eid: b}
	h.playersRef = players
	m := h.spawnSpecies(players, etype, 0, 0.5, 180, 0.5)
	m.health = 1000
	return h, players, a, b, m
}

// A wolf you hit holds the grudge against you, not against the next
// player it sees, and calms down once its anger runs out with you gone.
func TestProvokedWolfCalmsDown(t *testing.T) {
	h, players, a, b, m := angerRig(t, entityWolf)
	h.attackMob(players, a.p.eid, m.eid)
	if !m.hostile || m.targetEID != a.p.eid {
		t.Fatalf("a hit wolf turns on its attacker: hostile %v target %d", m.hostile, m.targetEID)
	}
	if m.anger < neutralAngerMin || m.anger > neutralAngerMin+neutralAngerRange {
		t.Fatalf("PERSISTENT_ANGER_TIME is 20-39 s: %d updates", m.anger)
	}
	delete(players, a.p.eid) // the attacker leaves
	h.updateMobs(players)
	if m.targetEID == b.p.eid || m.tx == b.x {
		t.Fatal("an angry wolf went for a bystander")
	}
	if !m.hostile {
		t.Fatal("it is still angry while its anger lasts")
	}
	m.anger = 1
	h.updateMobs(players)
	h.updateMobs(players)
	if m.hostile || m.hasTarget {
		t.Fatalf("its anger spent and its attacker gone, the wolf calms: hostile %v target %v", m.hostile, m.hasTarget)
	}
}

// LlamaHurtByTargetGoal: a hit llama spits at the one who hit it, once,
// and is peaceful again.
func TestLlamaSpitsOnceAtItsAttacker(t *testing.T) {
	h, players, a, b, m := angerRig(t, entityLlama)
	a.x, b.x = 5.5, -1.5 // the bystander is the nearer
	h.attackMob(players, a.p.eid, m.eid)
	if !m.hostile {
		t.Fatal("a hit llama turns on its attacker")
	}
	for i := 0; i < 60 && len(h.arrows) == 0; i++ {
		h.updateMobs(players)
	}
	if len(h.arrows) != 1 {
		t.Fatalf("one spit, got %d", len(h.arrows))
	}
	for _, s := range h.arrows {
		if s.vx <= 0 {
			t.Fatalf("the spit flew toward the bystander (vx %.2f)", s.vx)
		}
	}
	if m.hostile || m.hasTarget {
		t.Fatal("having spat, the llama drops its target")
	}
	for i := 0; i < 40; i++ {
		h.updateMobs(players)
	}
	if len(h.arrows) > 1 {
		t.Fatalf("it spat again: %d spits", len(h.arrows))
	}
}

// A bee you hit chases you for 20-39 seconds and then gives up, even if it
// never caught you: Bee.customServerAiStep runs updatePersistentAnger without
// refreshing the timer while it holds its target. It goes back to being a
// bee, not a generic wanderer.
func TestProvokedBeeCalmsDown(t *testing.T) {
	h, players, a, b, m := angerRig(t, entityBee)
	full := b.health
	h.attackMob(players, a.p.eid, m.eid)
	if !m.hostile || m.targetEID != a.p.eid {
		t.Fatalf("a hit bee turns on its attacker: hostile %v target %d", m.hostile, m.targetEID)
	}
	calmAt := -1
	for i := 0; i < 1000; i++ {
		// Keep ahead of it: always twelve blocks off, in plain sight.
		a.x, a.z = m.x+12, m.z
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if !m.hostile {
			calmAt = i
			break
		}
		if m.targetEID != a.p.eid {
			t.Fatalf("update %d: the bee should still be after its attacker, target %d", i, m.targetEID)
		}
	}
	if calmAt < 0 {
		t.Fatal("the bee never calmed down")
	}
	if b.health != full || m.beeStingDie != 0 {
		t.Fatalf("the bee stung a bystander on its way: health %v→%v", full, b.health)
	}
	if calmAt < neutralAngerMin-2 || calmAt > neutralAngerMin+neutralAngerRange+2 {
		t.Fatalf("PERSISTENT_ANGER_TIME is 20-39 s; the bee gave up after %d updates", calmAt)
	}
	if _, ok := m.behavior.(beeBehavior); !ok || m.anger != 0 || m.hasTarget {
		t.Fatalf("a calmed bee flies its errands again: behavior %T anger %d target %v", m.behavior, m.anger, m.hasTarget)
	}
}

// Once it has stung, a bee is done being angry (doHurtTarget calls
// stopBeingAngry); it wanders off to die rather than hounding its victim.
func TestStungBeeStopsBeingAngry(t *testing.T) {
	h, players, a, _, m := angerRig(t, entityBee)
	h.attackMob(players, a.p.eid, m.eid)
	for i := 0; i < 400 && m.beeStingDie == 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if m.beeStingDie == 0 {
		t.Fatal("the bee never stung its attacker")
	}
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if m.hostile || m.anger != 0 {
		t.Fatalf("a bee that has stung is no longer angry: hostile %v anger %d", m.hostile, m.anger)
	}
}
