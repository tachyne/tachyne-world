package server

import (
	"testing"

	"tachyne/internal/world"
)

func TestTameWolfWithBone(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityWolf)
	// Force the tame roll to succeed: seed the rng path by feeding until tamed.
	give(pl, itemBone)
	tamed := false
	for i := 0; i < 200 && !tamed; i++ {
		pl.inv.slots[0] = invStack{item: itemBone, count: 1}
		h.tryTame(players, pl, m)
		tamed = m.tamed
	}
	if !tamed {
		t.Fatal("feeding bones should eventually tame a wolf")
	}
	if m.owner != pl.p.eid {
		t.Fatalf("tamed wolf should belong to the feeder: owner=%d", m.owner)
	}
	if m.hostile {
		t.Fatal("a tamed wolf must not hunt on its own")
	}
}

func TestPetSitToggle(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityWolf)
	m.tamed, m.owner = true, pl.p.eid
	pl.p.held = 0
	pl.inv.slots[0] = invStack{} // empty hand
	if !h.tryTame(players, pl, m) || !m.sitting {
		t.Fatalf("empty-hand right-click should sit the pet: sitting=%v", m.sitting)
	}
	if !h.tryTame(players, pl, m) || m.sitting {
		t.Fatal("a second click should stand it back up")
	}
}

func TestPetFollowsOwner(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnSpecies(players, entityWolf, 0, 100.5, 70, 100.5)
	m.tamed, m.owner = true, pl.p.eid

	// Owner walks out of range (but within teleport distance) → target them.
	pl.x = 115.5 // 15 blocks: past follow-start (10), under teleport (20)
	if h.petAcquire(players, m); !m.hasTarget {
		t.Fatalf("a pet should follow an owner past %v blocks", petFollowStart)
	}
	if m.tx != pl.x {
		t.Fatalf("pet should target the owner's position, got tx=%v", m.tx)
	}
	// Owner right beside it → stop following.
	pl.x = 100.5
	m.x = 100.5
	if h.petAcquire(players, m); m.hasTarget {
		t.Fatal("a pet next to its owner should stop following")
	}
}

func TestPetTeleportsWhenFar(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnSpecies(players, entityWolf, 0, 100.5, 70, 100.5)
	m.tamed, m.owner = true, pl.p.eid
	pl.x, pl.z = 200.5, 200.5 // way past the teleport range
	h.petAcquire(players, m)
	if dist2 := (m.x-pl.x)*(m.x-pl.x) + (m.z-pl.z)*(m.z-pl.z); dist2 > 9 {
		t.Fatalf("a pet left too far should teleport to the owner, still at (%v,%v)", m.x, m.z)
	}
}

func TestSitInterruptsFollow(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityWolf)
	m.tamed, m.owner, m.sitting = true, pl.p.eid, true
	if !h.petAcquire(players, m) {
		t.Fatal("petAcquire should report a sitting pet")
	}
	if m.hasTarget {
		t.Fatal("a sitting pet must not chase the owner")
	}
}
