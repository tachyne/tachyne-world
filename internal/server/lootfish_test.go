package server

import (
	"strings"
	"testing"
)

// /loot … fish through the dispatcher: the fish pool gives one of the four
// fish, and an unknown table is refused.
func TestLootFish(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "loot give alice fish minecraft:gameplay/fishing/fish ~ ~ ~")
	s.handleCommand(alice, "loot give alice fish minecraft:gameplay/fishing ~ ~ ~ mainhand")
	s.handleCommand(alice, "loot give alice fish minecraft:nope ~ ~ ~")
	s.handleCommand(alice, "loot give alice fish minecraft:gameplay/fishing/fish ~ ~ ~ fishing_rod[enchantments={luck_of_the_sea:3}]")
	s.handleCommand(alice, "loot give alice fish minecraft:gameplay/fishing/fish ~ ~ ~ no_such_rod")
	settle(t, h, logs, "LF")
	a := linesBetween(logs["alice"], "", "LF")
	joined := strings.Join(a, "\n")
	if strings.Count(joined, "Dropped 1 ") < 3 {
		t.Errorf("fishing loot replies: %q", a)
	}
	if !hasLine(a, "Unknown item 'minecraft:no_such_rod'") {
		t.Errorf("an unknown tool was not refused: %q", a)
	}
	if !hasLine(a, "The loot table minecraft:nope is not available on this server") {
		t.Errorf("no refusal: %q", a)
	}
}
