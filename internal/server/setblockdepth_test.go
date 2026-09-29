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

// The rest of BlockInput's block entity data: a sign's two sides, a banner's
// patterns and a hopper's Items; a /fill filter that asks for block entity
// data (NbtUtils.compareNbt, partial lists) and a mode after it; and the cap
// read from max_block_modifications, in vanilla's wording.
func TestSetblockBlockEntityKinds(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, `setblock 7 150 7 oak_sign{front_text:{messages:["Hello","big world","",""],color:"red",has_glowing_text:1b},is_waxed:1b}`)
	s.handleCommand(alice, `setblock 8 150 8 white_banner{patterns:[{pattern:"minecraft:stripe_top",color:"red"}]}`)
	s.handleCommand(alice, `setblock 9 150 9 hopper{Items:[{Slot:2b,id:"minecraft:iron_ingot",count:3}]}`)
	s.handleCommand(alice, `setblock 0 155 0 chest{Items:[{Slot:0b,id:"minecraft:diamond",count:1},{Slot:1b,id:"minecraft:stick",count:1}]}`)
	s.handleCommand(alice, `setblock 1 155 0 chest{Items:[{Slot:0b,id:"minecraft:dirt",count:1}]}`)
	s.handleCommand(alice, `setblock 2 155 0 chest`)
	settle(t, h, logs, "N0")
	s.handleCommand(alice, `fill 0 155 0 2 155 0 gold_block replace chest{Items:[{id:"minecraft:diamond"}]} strict`)
	s.handleCommand(alice, `fill 0 156 0 2 156 0 stone replace air outline`)
	s.handleCommand(alice, "gamerule max_block_modifications 10")
	s.handleCommand(alice, "fill 0 160 0 2 162 2 stone")
	settle(t, h, logs, "N1")
	a := linesBetween(logs["alice"], "N0", "N1")
	if !hasLine(a, "Too many blocks in the specified area (maximum 10, but specified 27)") {
		t.Errorf("no cap from max_block_modifications: %q", a)
	}
	onHub(t, h, func() {
		sd, ok := h.signs.get(0, 7, 150, 7)
		if !ok || sd.Front.Lines[0] != "Hello" || sd.Front.Lines[1] != "big world" || sd.Front.Color != "red" || !sd.Front.Glow || !sd.Waxed {
			t.Errorf("sign %+v %v", sd, ok)
		}
		if l := h.banners.get(0, 8, 150, 8); len(l) != 1 || l[0].Color != "red" || bannerPatternQualified(l[0].Pattern) != "minecraft:stripe_top" {
			t.Errorf("banner %+v", l)
		}
		if b := h.bins[simPos{blockPos: blockPos{9, 150, 9}}]; b == nil || b.slots[2].item != itemByName["iron_ingot"] || b.slots[2].count != 3 {
			t.Errorf("hopper %+v", b)
		}
		if st := h.world.Block(0, 155, 0); st != worldgen.BlockBase("gold_block") {
			t.Error("the chest holding a diamond was not replaced")
		}
		for x := 1; x <= 2; x++ {
			if st := h.world.Block(x, 155, 0); st == worldgen.BlockBase("gold_block") {
				t.Errorf("the chest at %d,155,0 matched a diamond it does not hold", x)
			}
		}
		for x := 0; x <= 2; x++ {
			if st := h.world.Block(x, 156, 0); st != worldgen.Stone {
				t.Errorf("replace air outline left %d,156,0 as %d", x, st)
			}
		}
		if st := h.world.Block(1, 161, 1); st == worldgen.Stone {
			t.Error("a fill over the gamerule cap ran")
		}
	})
}
