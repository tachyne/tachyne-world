package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /clone through the dispatcher: blocks and a chest's contents are copied,
// the success line counts the cells, a move clears the source without a
// drop, and an overlapping normal clone or a too-large box is refused.
func TestCommandClone(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	chestSt, ok := parseBlockState("chest[type=single,waterlogged=false]")
	if !ok {
		t.Fatal("chest state")
	}
	src := simPos{dim: 0, blockPos: blockPos{0, 100, 0}}
	onHub(t, h, func() {
		h.world.SetBlock(1, 100, 0, worldgen.BlockID("stone"))
		h.world.SetBlock(0, 100, 0, chestSt)
		c := &chest{}
		c.slots[4] = invStack{item: itemByName["diamond"], count: 7}
		h.chests[src] = c
	})

	s.handleCommand(alice, "clone 0 100 0 1 100 0 5 100 0")
	settle(t, h, logs, "C1")
	if a := linesBetween(logs["alice"], "", "C1"); !hasLine(a, "Successfully cloned 2 block(s)") {
		t.Fatalf("clone feedback: %q", a)
	}
	onHub(t, h, func() {
		if got := h.world.At(6, 100, 0); got != worldgen.BlockID("stone") {
			t.Errorf("stone not cloned: %d", got)
		}
		if got := h.world.At(5, 100, 0); got != chestSt {
			t.Errorf("chest not cloned: %d", got)
		}
		dst := h.chests[simPos{dim: 0, blockPos: blockPos{5, 100, 0}}]
		if dst == nil || dst.slots[4].count != 7 || dst.slots[4].item != itemByName["diamond"] {
			t.Errorf("chest contents not carried: %+v", dst)
		}
		if orig := h.chests[src]; orig == nil || orig.slots[4].count != 7 {
			t.Error("a copy must leave the source chest as it was")
		}
		if dst != nil && h.chests[src] == dst {
			t.Error("the copy shares the source's storage")
		}
	})

	// Overlap is refused in normal mode…
	s.handleCommand(alice, "clone 0 100 0 1 100 0 1 100 0")
	// …and a box over the block limit.
	s.handleCommand(alice, "clone 0 0 0 40 40 40 0 100 0")
	settle(t, h, logs, "C2")
	a := linesBetween(logs["alice"], "C1", "C2")
	if !hasLine(a, "The source and destination areas cannot overlap") {
		t.Errorf("overlap not refused: %q", a)
	}
	if !hasLine(a, "Too many blocks in the specified area (maximum 32768, but specified 68921)") {
		t.Errorf("block limit not enforced: %q", a)
	}

	// move may overlap its own source: the source is emptied with nothing
	// dropped, and the moved chest keeps its contents.
	var itemsBefore int
	onHub(t, h, func() { itemsBefore = len(h.items) })
	s.handleCommand(alice, "clone 5 100 0 6 100 0 6 100 0 replace move")
	settle(t, h, logs, "C3")
	onHub(t, h, func() {
		if got := h.world.At(5, 100, 0); got != worldgen.Air {
			t.Errorf("move left the source: %d", got)
		}
		if h.chests[simPos{dim: 0, blockPos: blockPos{5, 100, 0}}] != nil {
			t.Error("move left the source chest's storage behind")
		}
		if got := h.world.At(7, 100, 0); got != worldgen.BlockID("stone") {
			t.Errorf("moved stone: %d", got)
		}
		c := h.chests[simPos{dim: 0, blockPos: blockPos{6, 100, 0}}]
		if c == nil || c.slots[4].count != 7 {
			t.Errorf("move lost the contents: %+v", c)
		}
		if len(h.items) != itemsBefore {
			t.Errorf("move dropped items: %d → %d", itemsBefore, len(h.items))
		}
	})

	// masked over air clones nothing.
	s.handleCommand(alice, "clone 20 120 20 21 120 20 25 120 20 masked")
	// A non-operator is refused.
	s.handleCommand(ps["carol"], "clone 0 100 0 1 100 0 5 100 0")
	settle(t, h, logs, "C4")
	if a := linesBetween(logs["alice"], "C3", "C4"); !hasLine(a, "No blocks were cloned") {
		t.Errorf("masked air: %q", a)
	}
	if c := linesBetween(logs["carol"], "C3", "C4"); permissionRefusals(c) != 1 {
		t.Errorf("non-op: %q", c)
	}
}

// The grammar's optional parts.
func TestCloneParse(t *testing.T) {
	p := newPlayer(1, "x", [16]byte{})
	r, msg := parseClone(strings.Fields("from the_nether 0 0 0 1 1 1 to minecraft:the_end 5 5 5 strict filtered #minecraft:leaves force"), p)
	if msg != "" || r.srcDim != 1 || r.dstDim != 2 || !r.strict || r.mode != cloneForce || r.mask == nil {
		t.Fatalf("full form: %+v %q", r, msg)
	}
	if !r.mask(worldgen.BlockID("oak_leaves")) || r.mask(worldgen.BlockID("stone")) {
		t.Error("a #leaves filter")
	}
	if _, msg := parseClone(strings.Fields("0 0 0 1 1 1 5 5 5 sideways"), p); msg == "" {
		t.Error("an unknown mode is refused")
	}
	if _, msg := parseClone(strings.Fields("from mars 0 0 0 1 1 1 5 5 5"), p); msg != "Unknown dimension 'mars'" {
		t.Errorf("unknown dimension: %q", msg)
	}
}

// permissionRefusals counts the permission refusals among a player's lines
// (the HUD's action-bar refresh may land in between).
func permissionRefusals(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "permission") {
			n++
		}
	}
	return n
}
