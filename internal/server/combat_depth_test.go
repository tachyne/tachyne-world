package server

import (
	"testing"

	"tachyne/internal/world"
)

// combatSetup: an attacker at melee range of a fresh zombie, clock live.
func combatSetup() (*hub, *tracked, map[int32]*tracked, *mob) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	m := &mob{eid: 2, etype: entityZombie, hostile: true, health: 100, x: 2.0, y: 70, z: 0.5}
	h.mobs[2] = m
	h.tick.Store(100)
	return h, pl, players, m
}

func TestSpamClickingIsScaledDown(t *testing.T) {
	h, pl, players, m := combatSetup()
	pl.p.setHotbarSlot(0, tDiamondSword) // 7 damage
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1}

	h.attackMob(players, 1, 2) // first swing: full charge → 7
	full := 100 - m.health
	if full != 7 {
		t.Fatalf("first swing should land full damage, dealt %d", full)
	}
	h.tick.Add(1) // swing again immediately (1 tick of a 13-tick recovery)
	before := m.health
	h.attackMob(players, 1, 2)
	if spam := before - m.health; spam > 2 {
		t.Fatalf("a 1-tick spam-click should deal ~20%%, dealt %d", spam)
	}
	h.tick.Add(20) // fully recovered
	before = m.health
	h.attackMob(players, 1, 2)
	if got := before - m.health; got != 7 {
		t.Fatalf("recovered swing should land full damage, dealt %d", got)
	}
}

func TestFallingCritHitsHarder(t *testing.T) {
	h, pl, players, m := combatSetup()
	pl.p.setHotbarSlot(0, tDiamondSword)
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1}
	pl.airborne, pl.peakY, pl.y = true, 75, 72 // descending mid-jump
	h.attackMob(players, 1, 2)
	if got := 100 - m.health; got != 11 { // round(7 × 1.5)
		t.Fatalf("jump crit should deal 11 (7×1.5), dealt %d", got)
	}
}

func TestHitKnocksTheMobBack(t *testing.T) {
	h, _, players, m := combatSetup()
	h.attackMob(players, 1, 2)
	if m.kb == 0 || m.vx <= 0 {
		t.Fatalf("hit must shove the mob away from the attacker: kb=%d vx=%v", m.kb, m.vx)
	}
}

func TestSweepClipsAdjacentMobs(t *testing.T) {
	h, pl, players, m := combatSetup()
	pl.p.setHotbarSlot(0, tDiamondSword)
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1}
	pl.onGround = true
	side := &mob{eid: 3, etype: entityZombie, hostile: true, health: 100, x: 2.5, y: 70, z: 1.2}
	h.mobs[3] = side
	h.attackMob(players, 1, 2)
	if side.health != 99 {
		t.Fatalf("sweep should clip the adjacent mob for 1, health=%d", side.health)
	}
	_ = m
}
