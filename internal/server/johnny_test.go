package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A vindicator named Johnny attacks every living thing; an ordinary one
// does not.
func TestVindicatorJohnny(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	v := h.spawnMob(players, entityVindicator, 0.5, 70, 0.5)
	cow := h.spawnMob(players, entityCow, 3.5, 70, 0.5)
	if mobHuntPrey(v, cow) {
		t.Fatal("an ordinary vindicator leaves the livestock alone")
	}
	v.customName = "Johnny"
	if !mobHuntPrey(v, cow) {
		t.Fatal("Johnny attacks everything")
	}
	other := h.spawnMob(players, entityVindicator, 5.5, 70, 0.5)
	if mobHuntPrey(v, other) {
		t.Error("…except another vindicator")
	}
	// And he actually goes for it.
	if !h.mobHuntStep(players, v) {
		t.Fatal("Johnny should steer at his prey")
	}
	if v.wolfPrey != cow.eid {
		t.Errorf("his target should be the cow, got eid %d", v.wolfPrey)
	}
}

// Pillagers advance at half pace with a charged or charging crossbow.
func TestPillagerHalfPaceWhileLoaded(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 12.5, 70, 0.5 // inside the follow range, outside crossbow reach
	m := h.spawnMob(players, entityPillager, 0.5, 70, 0.5)
	m.hostile = true
	m.setMoveSpeed(speedFor(entityPillager))

	m.cbState = cbUncharged
	m.vx, m.vz = 1, 0
	h.pillagerTick(players, m)
	if m.vx != 1 {
		t.Fatalf("an empty crossbow runs at full pace, got %v", m.vx)
	}
	m.cbState = cbCharging
	m.vx, m.vz = 1, 0
	h.pillagerTick(players, m)
	if m.vx != 0.5 {
		t.Fatalf("a charging pillager advances at half pace, got %v", m.vx)
	}
}
