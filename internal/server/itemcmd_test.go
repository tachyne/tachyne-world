package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /item through the dispatcher: replace fills the first slot of a range,
// fill every slot, override empties the rest; from copies another holder's
// slots; and the not-a-container, no-such-slot and nobody-accepted cases
// fail with vanilla's lines.
func TestCommandItem(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	chestSt, _ := parseBlockState("chest[type=single,waterlogged=false]")
	cp := simPos{dim: 0, blockPos: blockPos{0, 100, 0}}
	onHub(t, h, func() { h.world.SetBlock(0, 100, 0, chestSt) })
	diamond, stone := itemByName["diamond"], itemByName["stone"]

	s.handleCommand(alice, "item replace block 0 100 0 container.* with diamond 5")
	settle(t, h, logs, "I1")
	if a := linesBetween(logs["alice"], "", "I1"); !hasLine(a, "Replaced 1 slot(s) at 0, 100, 0 with [Diamond]") {
		t.Fatalf("replace: %q", a)
	}
	onHub(t, h, func() {
		c := h.chests[cp]
		if c == nil || c.slots[0] != (invStack{item: diamond, count: 5}) || c.slots[1].item != 0 {
			t.Errorf("replace put: %+v", c)
		}
	})

	s.handleCommand(alice, "item fill block 0 100 0 container.* with stone")
	s.handleCommand(alice, "item override block 0 100 0 container.* from block 0 100 0 container.0")
	s.handleCommand(alice, "item replace entity bob hotbar.0 from block 0 100 0 container.0")
	settle(t, h, logs, "I2")
	a := linesBetween(logs["alice"], "I1", "I2")
	for _, want := range []string{"Replaced 27 slot(s) at 0, 100, 0 with [Stone]",
		"Replaced 27 slot(s) at 0, 100, 0", "Replaced 1 slot(s) on bob"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		c := h.chests[cp]
		if c.slots[0] != (invStack{item: stone, count: 1}) || c.slots[26].item != 0 {
			t.Errorf("override: slot 0 %+v, slot 26 %+v", c.slots[0], c.slots[26])
		}
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		if bob == nil || bob.inv == nil || bob.inv.slots[0] != (invStack{item: stone, count: 1}) {
			t.Errorf("bob's hotbar: %+v", bob)
		}
	})

	onHub(t, h, func() { h.world.SetBlock(5, 100, 5, worldgen.BlockID("stone")) })
	s.handleCommand(alice, "item replace block 5 100 5 container.0 with diamond")
	s.handleCommand(alice, "item replace block 0 100 0 container.40 with diamond")
	s.handleCommand(alice, "item replace entity @a armor.chest with diamond")
	s.handleCommand(alice, "item replace block 0 100 0 nowhere.1 with diamond")
	s.handleCommand(ps["carol"], "item replace entity @s hotbar.0 with diamond")
	settle(t, h, logs, "I3")
	a = linesBetween(logs["alice"], "I2", "I3")
	for _, want := range []string{"Target position 5, 100, 5 is not a container",
		"The target does not have slot container.40",
		"No targets accepted item [Diamond] into specified slots",
		"Unknown slot 'nowhere.1'"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "I2", "I3"); len(c) != 1 || !strings.Contains(c[0], "permission") {
		t.Errorf("non-op: %q", c)
	}
}
