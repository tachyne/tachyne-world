package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /setblock and /fill depth through the dispatcher: block entity data (a
// chest's name and contents), a #tag filter and a property filter for
// /fill, and strict placement, which leaves an unsupported torch standing
// where a normal set would knock it off.
func TestSetblockFillDepth(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	y := 150
	s.handleCommand(alice, `setblock 3 150 3 chest{CustomName:"Loot",Items:[{Slot:0b,id:"minecraft:diamond",count:4}]}`)
	s.handleCommand(alice, "fill 0 151 0 2 151 0 oak_leaves[persistent=true]")
	s.handleCommand(alice, "fill 0 151 1 2 151 1 oak_planks")
	s.handleCommand(alice, "fill 0 151 0 2 151 1 stone replace #leaves")
	s.handleCommand(alice, "fill 0 152 0 2 152 0 oak_log[axis=y]")
	s.handleCommand(alice, "setblock 1 152 0 oak_log[axis=z]")
	s.handleCommand(alice, "fill 0 152 0 2 152 0 glass replace oak_log[axis=y]")
	s.handleCommand(alice, "setblock 5 159 5 stone")
	s.handleCommand(alice, "setblock 5 160 5 torch")
	s.handleCommand(alice, "setblock 6 159 6 stone")
	s.handleCommand(alice, "setblock 6 160 6 torch")
	settle(t, h, logs, "F0")
	s.handleCommand(alice, "setblock 5 159 5 air strict")
	s.handleCommand(alice, "setblock 6 159 6 air")
	settle(t, h, logs, "F1")
	onHub(t, h, func() {
		c := h.chests[simPos{blockPos: blockPos{3, y, 3}}]
		if c == nil || c.slots[0].item != itemByName["diamond"] || c.slots[0].count != 4 {
			t.Errorf("chest %+v", c)
		}
		if got := h.blockNames.get(simPos{blockPos: blockPos{3, y, 3}}); got != "Loot" {
			t.Errorf("chest name %q", got)
		}
		for x := 0; x <= 2; x++ {
			if st := h.world.Block(x, 151, 0); st != worldgen.Stone {
				t.Errorf("log at %d,151,0 not replaced by #leaves: %d", x, st)
			}
			if st := h.world.Block(x, 151, 1); st != worldgen.BlockBase("oak_planks") {
				t.Errorf("planks at %d,151,1 were replaced by the #leaves filter", x)
			}
		}
		if st := h.world.Block(1, 152, 0); !sameBlockFamily(st, worldgen.BlockBase("oak_log")) {
			t.Errorf("the axis=z log was replaced by an axis=y filter")
		}
		if st := h.world.Block(0, 152, 0); st != worldgen.BlockBase("glass") {
			t.Errorf("the axis=y log was not replaced")
		}
	})
	onHub(t, h, func() {})
	onHub(t, h, func() {
		if st := h.world.Block(5, 160, 5); st != worldgen.BlockBase("torch") {
			t.Error("a strict removal of its floor knocked the torch off")
		}
		if st := h.world.Block(6, 160, 6); st == worldgen.BlockBase("torch") {
			t.Error("a normal removal of its floor left the torch standing")
		}
	})
}
