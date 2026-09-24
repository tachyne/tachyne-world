package server

import (
	"strings"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /loot through the dispatcher: a block's table into a player's
// inventory and into a container slot, a chest table inserted into a
// chest, a mob's death table, and the failure lines.
func TestCommandLoot(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	chestSt, _ := parseBlockState("chest[type=single,waterlogged=false]")
	cp := simPos{dim: 0, blockPos: blockPos{0, 100, 0}}
	onHub(t, h, func() {
		h.world.SetBlock(0, 100, 0, chestSt)
		h.world.SetBlock(3, 100, 3, worldgen.BlockID("stone"))
	})
	cobble := itemByName["cobblestone"]

	s.handleCommand(alice, "loot give bob mine 3 100 3 diamond_pickaxe")
	s.handleCommand(alice, "loot replace block 0 100 0 container.4 mine 3 100 3")
	settle(t, h, logs, "L1")
	a := linesBetween(logs["alice"], "", "L1")
	if n := strings.Count(strings.Join(a, "\n"), "Dropped 1 [Cobblestone] from loot table minecraft:blocks/stone"); n != 2 {
		t.Fatalf("mine lines: %q", a)
	}
	onHub(t, h, func() {
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		got := 0
		for _, st := range bob.inv.slots {
			if st.item == cobble {
				got += st.count
			}
		}
		if got != 1 {
			t.Errorf("bob holds %d cobblestone, want 1", got)
		}
		if c := h.chests[cp]; c == nil || c.slots[4] != (invStack{item: cobble, count: 1}) {
			t.Errorf("chest slot 4: %+v", c)
		}
	})

	s.handleCommand(alice, "loot insert 0 100 0 loot chests/simple_dungeon")
	s.handleCommand(alice, "summon cow 2 100 2")
	settle(t, h, logs, "L2")
	a = linesBetween(logs["alice"], "L1", "L2")
	if !hasPrefixLine(a, "Dropped ") {
		t.Errorf("loot table insert: %q", a)
	}
	onHub(t, h, func() {
		n := 0
		for _, st := range h.chests[cp].slots {
			if st.item != 0 {
				n++
			}
		}
		if n < 2 {
			t.Errorf("the dungeon table put %d stacks in the chest", n)
		}
	})
	// The cow's own table, as a kill by alice.
	deadline := time.Now().Add(hubTestWait)
	for {
		var cows int
		onHub(t, h, func() {
			for _, m := range h.mobs {
				if m.etype == entityCow {
					cows++
				}
			}
		})
		if cows > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the cow never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.handleCommand(alice, "loot spawn 2 101 2 kill @e[type=cow,limit=1]")
	settle(t, h, logs, "L3")
	a = linesBetween(logs["alice"], "L2", "L3")
	if len(a) != 1 || !strings.HasPrefix(a[0], "Dropped ") || !strings.HasSuffix(a[0], "from loot table minecraft:entities/cow") {
		t.Errorf("kill: %q", a)
	}

	onHub(t, h, func() { h.world.SetBlock(5, 100, 5, worldgen.BlockID("stone")) })
	s.handleCommand(alice, "loot insert 5 100 5 loot chests/simple_dungeon")
	s.handleCommand(alice, "loot give bob loot chests/nope")
	s.handleCommand(alice, "loot give bob fish gameplay/fishing 0 100 0")
	s.handleCommand(alice, "loot replace block 0 100 0 container.* mine 3 100 3")
	s.handleCommand(ps["carol"], "loot give @s mine 3 100 3")
	settle(t, h, logs, "L4")
	a = linesBetween(logs["alice"], "L3", "L4")
	for _, want := range []string{"Target position 5, 100, 5 is not a container",
		"The loot table minecraft:chests/nope is not available on this server",
		"/loot fish is not supported yet",
		"Only single slots allowed: got 'container.*'"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "L3", "L4"); !hasLine(c, "You don't have permission.") || hasPrefixLine(c, "Dropped") {
		t.Errorf("non-op: %q", c)
	}
}
