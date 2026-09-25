package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The .item death messages name what the killer holds when it has a name of
// its own, whatever the blow came from: a Thorns kill names the wearer's
// named sword, and an arrow credited to a player holding a named bow reads
// "was shot by X using Y" (DamageSource.getLocalizedDeathMessage reads the
// causing entity's main hand).
func TestDeathMessageItemVariants(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.PvP = true
	a, b, players := pvpPair(h)
	h.playersRef = players
	b.p.setHotbarSlot(0, itemByName["iron_sword"])
	b.inv.slots[0] = invStack{item: itemByName["iron_sword"], count: 1, name: "Excalibur"}
	for i := range b.armor {
		b.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1, ench: enchList{{id: enchThorns, lvl: 3}}}
	}
	for i := 0; i < 60 && !a.dead; i++ {
		a.health, b.health = 1, 20
		a.lastAttack = 0
		h.tick.Add(20)
		h.onAttack(players, evAttack{attacker: a.p.eid, target: b.p.eid})
	}
	if !a.dead {
		t.Fatal("Thorns never killed the attacker")
	}
	if got, want := h.combatDeathMessage(a), "tester was killed by Excalibur while trying to hurt victim"; got != want {
		t.Errorf("thorns death: %q, want %q", got, want)
	}
}

// /damage … by <player> is the same message path.
func TestDamageByNamesTheHeldItem(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			switch tr.p.name {
			case "alice":
				tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemByName["bow"], count: 1, name: "Longshot"}
			case "bob":
				tr.gamemode, tr.health = gmSurvival, 20
			}
		}
	})
	s.handleCommand(alice, "damage bob 40 minecraft:arrow by alice")
	settle(t, h, logs, "N1")
	if all := strings.Join(logs["carol"].all(), "\n"); !strings.Contains(all, "bob was shot by alice using Longshot") {
		t.Errorf("no .item death message: %q", all)
	}
}
