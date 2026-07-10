package server

import (
	"testing"

	"tachyne/internal/world"
)

// ridingSetup spawns a player and a fresh mob of etype next to it.
func ridingSetup(t *testing.T, etype int) (*hub, *tracked, map[int32]*tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnSpecies(players, etype, 0, 101.5, 70, 100.5)
	return h, pl, players, m
}

func give(t *tracked, item int32) {
	t.p.held = 0
	t.inv.slots[0] = invStack{item: item, count: 1}
}

func TestSaddleThenMount(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	give(pl, itemSaddle)
	if !h.tryMount(players, pl, m) || !m.saddled {
		t.Fatalf("holding a saddle should saddle the horse: saddled=%v", m.saddled)
	}
	if m.rider != 0 {
		t.Fatal("saddling should not also mount in one click")
	}
	// Saddle consumed; second right-click (now empty-ish) mounts.
	if !h.tryMount(players, pl, m) || m.rider != pl.p.eid {
		t.Fatalf("a saddled horse should mount: rider=%d", m.rider)
	}
}

func TestRiddenMobPausesAI(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	m.saddled, m.rider = true, pl.p.eid
	m.x = 200 // park it far; if AI ran it would drift
	before := m.x
	for i := 0; i < 5; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if m.x != before {
		t.Fatalf("a ridden mob's AI must be paused: x %v -> %v", before, m.x)
	}
}

func TestMountMoveDragsRider(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	m.rider = pl.p.eid
	handled := h.applyMountMove(players, pl, evVehicleMove{eid: pl.p.eid, x: 102.5, y: 70, z: 100.5, yaw: 90})
	if !handled {
		t.Fatal("applyMountMove should handle a rider's vehicle_move")
	}
	if m.x != 102.5 {
		t.Fatalf("mount should move to the client position: x=%v", m.x)
	}
	if pl.x != 102.5 || pl.y != 70.6 {
		t.Fatalf("rider should ride along: (%v,%v)", pl.x, pl.y)
	}
}

func TestDismountMob(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	m.rider = pl.p.eid
	if !h.dismountMob(players, pl) || m.rider != 0 {
		t.Fatalf("dismount should clear the rider: rider=%d", m.rider)
	}
}

func TestOnlyRideableCanBeMounted(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityCow) // cows aren't rideable
	give(pl, itemSaddle)
	if h.tryMount(players, pl, m) {
		t.Fatal("a cow must not accept a saddle / mount")
	}
}

func TestBabyCannotBeRidden(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	m.baby = true
	give(pl, itemSaddle)
	if h.tryMount(players, pl, m) {
		t.Fatal("a foal must not be saddleable/rideable")
	}
}
