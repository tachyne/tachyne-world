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
