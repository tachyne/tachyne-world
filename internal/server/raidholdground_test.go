package server

import "testing"

// Raider.HoldGroundAttackGoal: a patrolling pillager that spots a player
// beyond ten blocks stands and watches — crossbow down, no shot — and only
// turns aggressive (with the raiders beside it) once the player comes
// within ten. One a player has just hurt skips the stand-off.
func TestPatrolPillagerHoldsGround(t *testing.T) {
	h, players, pl := skeletonRig(t)
	m := h.spawnHostileYIn(players, entityPillager, dimOverworld, 4.5, 200, 0.5)
	mate := h.spawnHostileYIn(players, entityPillager, dimOverworld, 0.5, 200, 0.5)
	m.patrolling, mate.patrolling = true, true
	x0 := m.x
	for i := 0; i < 20; i++ {
		h.updateMobs(players)
	}
	if !m.hasTarget {
		t.Fatal("the pillager should have seen the player")
	}
	if !m.holdingGround || m.aggressive || m.cbState != cbUncharged {
		t.Fatalf("fourteen blocks off it holds its ground: holding=%v aggressive=%v crossbow=%d",
			m.holdingGround, m.aggressive, m.cbState)
	}
	if m.x != x0 {
		t.Errorf("a pillager holding its ground stays put, moved %.2f", m.x-x0)
	}
	pl.x = 11.5 // seven blocks from the first, eleven from its mate (four behind it)
	h.updateMobs(players)
	h.updateMobs(players)
	if !m.aggressive || !mate.aggressive || m.holdingGround || mate.holdingGround {
		t.Fatalf("within ten blocks the stand-off ends for both: %v/%v", m.aggressive, mate.aggressive)
	}
	for i := 0; i < 4; i++ {
		h.updateMobs(players)
	}
	if m.cbState == cbUncharged {
		t.Error("once aggressive the crossbow goal draws")
	}

	// Hurt by a player, a patrol pillager goes straight for them.
	h2, players2, pl2 := skeletonRig(t)
	o := h2.spawnHostileYIn(players2, entityPillager, dimOverworld, 4.5, 200, 0.5)
	o.patrolling = true
	o.hurtByPlayer, o.hurtByPlayerTil = pl2.p.eid, h2.tick.Load()+1000
	h2.updateMobs(players2)
	h2.updateMobs(players2)
	if o.holdingGround || !o.aggressive {
		t.Error("a pillager a player has hurt does not hold its ground")
	}
}
