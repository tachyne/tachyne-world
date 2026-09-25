package server

import (
	"strings"
	"testing"
)

// /damage's attributed forms through the dispatcher: `by` blames the named
// entity for a player's death and credits a mob's kill to the player, `from`
// blames the cause rather than the direct entity, and `at` is accepted with
// a source position.
func TestDamageCommandAttributed(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	var zombie *mob
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" || tr.p.name == "carol" {
				tr.gamemode = gmSurvival
				tr.health = 20
			}
		}
	})
	s.handleCommand(alice, "summon zombie 3 ~ 3")
	settle(t, h, logs, "D0")
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityZombie {
				zombie = m
			}
		}
		if zombie == nil {
			t.Error("no zombie summoned")
		}
	})
	if zombie == nil {
		return
	}
	s.handleCommand(alice, "damage bob 1 minecraft:generic at 10 100 0")
	s.handleCommand(alice, "damage bob 40 minecraft:player_attack by alice")
	s.handleCommand(alice, "damage carol 40 minecraft:mob_attack by bob from @e[type=zombie,limit=1]")
	settle(t, h, logs, "D1")
	a := linesBetween(logs["alice"], "D0", "D1")
	all := strings.Join(logs["carol"].all(), "\n")
	if !hasLine(a, "Applied 1.0 damage to bob") {
		t.Errorf("the `at` form was not applied: %q", a)
	}
	if !strings.Contains(all, "bob was slain by alice") {
		t.Errorf("no death message blaming alice: %q", all)
	}
	if !strings.Contains(all, "carol was slain by Zombie") {
		t.Errorf("no death message blaming the cause: %q", all)
	}

	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name == "alice" {
				tr.gamemode = gmSurvival // a creative player is no target
			}
		}
	})
	s.handleCommand(alice, "damage @e[type=zombie,limit=1] 2 minecraft:player_attack by alice")
	settle(t, h, logs, "D2")
	onHub(t, h, func() {
		if zombie.hurtByPlayer != alice.eid || !zombie.hitByPlayer {
			t.Errorf("the zombie's hurt was not credited to alice: by %d", zombie.hurtByPlayer)
		}
		if zombie.targetEID != alice.eid {
			t.Errorf("the zombie holds no grudge against alice: target %d", zombie.targetEID)
		}
	})
}
