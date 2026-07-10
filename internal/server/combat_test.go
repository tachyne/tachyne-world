package server

import (
	"testing"

	"tachyne/internal/world"
)

func TestMobCombatKillDropsBeef(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 5, 70, 5)
	if m.health != cowHealth {
		t.Fatalf("cow should start at %d hp, got %d", cowHealth, m.health)
	}

	h.attackMob(players, 999, m.eid) // one hit (no attacker in map → no knockback)
	if m.health != cowHealth-fistDamage {
		t.Errorf("after one hit hp=%d want %d", m.health, cowHealth-fistDamage)
	}
	if h.mobs[m.eid] == nil {
		t.Fatal("cow should still be alive after one hit")
	}

	for m.dying == 0 { // beat it to death (starts the death animation)
		h.attackMob(players, 999, m.eid)
	}
	for h.mobs[m.eid] != nil { // let the death animation play out → despawn + drops
		h.updateMobs(players)
	}
	beef := 0
	for _, it := range h.items {
		if it.item == itemBeef {
			beef += it.count
		}
	}
	if beef < 1 || beef > 3 {
		t.Errorf("dead cow should drop 1-3 beef, got %d", beef)
	}
}

func TestHitCowPanicsAndFlees(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(0, 0)
	atk := &tracked{p: newPlayer(1, "a", [16]byte{}), x: float64(lx) + 3, y: 70, z: float64(lz)}
	players := map[int32]*tracked{1: atk}
	m := h.spawnMob(players, entityCow, float64(lx), float64(h.world.GroundY(lx, lz)), float64(lz))
	atk.y = m.y // stand level with the cow (reach is validated server-side now)

	h.attackMob(players, 1, m.eid)
	if m.panic != panicTicks || m.fleeX != atk.x {
		t.Fatalf("hit cow should panic away from attacker: panic=%d fleeX=%v", m.panic, m.fleeX)
	}
	x0 := m.x
	for i := 0; i < 20; i++ {
		h.updateMobs(players)
	}
	if m.x > x0+0.01 { // attacker is to the +x side, so the cow must flee toward -x
		t.Errorf("fleeing cow drifted toward the attacker: x0=%v x=%v", x0, m.x)
	}
}

func TestAttackNonMobIsNoop(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.attackMob(players, 1, 12345) // unknown target — must not panic
}
