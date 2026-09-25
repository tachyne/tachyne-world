package server

import (
	"testing"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// ZombifiedPiglin: hit one and it and its pack go for the attacker, it
// calls in newcomers every four to six seconds, it moves faster while
// angry, and when the attacker is gone it neither turns on anybody else
// nor stays angry past its 20-39 seconds.
func TestZombifiedPiglinGrudge(t *testing.T) {
	h, players := preyFixture(t)
	a, b := survPlayer(h), survPlayer(h)
	a.p.eid, b.p.eid = 900, 901
	a.x, a.y, a.z = 2.0, 180, 0.5
	b.x, b.y, b.z = -6.5, 180, 0.5
	players[a.p.eid], players[b.p.eid] = a, b
	zp := h.spawnHostileY(players, entityZombifiedPiglin, 0.5, 180, 0.5)
	pal := h.spawnHostileY(players, entityZombifiedPiglin, 4.5, 180, 2.5)
	zp.baby, pal.baby = false, false
	step := func(n int) {
		for i := 0; i < n; i++ {
			h.tick.Add(mobMoveInterval)
			a.health, b.health = 20, 20 // the fight is not the point
			h.updateMobs(players)
		}
	}
	step(3)
	if zp.hasTarget || pal.hasTarget {
		t.Fatal("zombified piglins leave players alone until one is hit")
	}
	h.attackMob(players, a.p.eid, zp.eid)
	step(2)
	if zp.targetEID != a.p.eid || pal.targetEID != a.p.eid {
		t.Fatalf("the struck one and its pack go for the attacker: %d / %d", zp.targetEID, pal.targetEID)
	}
	if !zp.mobAttrs().Get(attr.MovementSpeed).HasModifier(zpSpeedSource) {
		t.Error("an angry zombified piglin gets SPEED_MODIFIER_ATTACKING")
	}
	// A newcomer is called in on the next alert, four to six seconds on.
	late := h.spawnHostileY(players, entityZombifiedPiglin, 6.5, 180, -2.5)
	late.baby = false
	step(zpAlertMin + zpAlertSpan + 1)
	if late.targetEID != a.p.eid {
		t.Errorf("a zombified piglin that arrives later is alerted within ALERT_INTERVAL, target %d", late.targetEID)
	}
	// The attacker leaves: nobody else becomes the target, and the anger
	// runs out.
	a.x = 400
	for i := 0; i < zpAngerMin+zpAngerSpan+hurtByUnseenMemory/mobMoveInterval+10; i++ {
		step(1)
		if zp.hasTarget && zp.targetEID == b.p.eid {
			t.Fatalf("update %d: it turned on a player it was never angry at", i)
		}
	}
	if zp.anger != 0 || zp.angryAt != 0 || zp.hasTarget {
		t.Errorf("the grudge should have run out: anger=%d angryAt=%d", zp.anger, zp.angryAt)
	}
	if zp.mobAttrs().Get(attr.MovementSpeed).HasModifier(zpSpeedSource) {
		t.Error("the attacking speed goes with the anger")
	}
}

// forgive_dead_players: a zombified piglin whose quarry died calms down —
// and is still a zombified piglin, ready to be provoked again, not a
// peaceful wanderer.
func TestZombifiedPiglinForgivesTheDeadAndStaysHostile(t *testing.T) {
	h, players := preyFixture(t)
	h.rules.ForgiveDead = true
	a := survPlayer(h)
	a.p.eid = 900
	a.x, a.y, a.z = 2.5, 180, 0.5
	players[a.p.eid] = a
	zp := h.spawnHostileY(players, entityZombifiedPiglin, 0.5, 180, 0.5)
	h.attackMob(players, a.p.eid, zp.eid)
	h.deathForgiveness(players, a)
	if zp.anger != 0 || zp.angryAt != 0 || zp.targetEID != 0 {
		t.Fatalf("the grudge goes with the player: anger=%d angryAt=%d target=%d", zp.anger, zp.angryAt, zp.targetEID)
	}
	if !zp.hostile {
		t.Fatal("a forgiving zombified piglin is still a monster")
	}
	h.attackMob(players, a.p.eid, zp.eid)
	if zp.targetEID != a.p.eid {
		t.Error("hit again, it takes the attacker for its target again")
	}
}
