package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

func sherdGrid(t *testing.T, back, left, right, front string) []invStack {
	t.Helper()
	grid := make([]invStack, 9)
	for cell, name := range map[int]string{1: back, 3: left, 5: right, 7: front} {
		if name == "" {
			continue
		}
		id := int32(itemByName[name])
		if id == 0 {
			t.Fatalf("no item %q", name)
		}
		grid[cell] = invStack{item: id, count: 1}
	}
	return grid
}

// The decorated pot had no crafting recipe at all — it could not be made.
// Four sherds in a diamond make one, and it wears them in vanilla's order.
func TestDecoratedPotRecipe(t *testing.T) {
	res, ok := decoratedPotMatch(sherdGrid(t,
		"angler_pottery_sherd", "brick", "heart_pottery_sherd", "snort_pottery_sherd"))
	if !ok {
		t.Fatal("four sherds in a diamond should make a decorated pot")
	}
	if res.item != itemDecoratedPot || res.count != 1 {
		t.Fatalf("made %+v, want one decorated pot", res)
	}
	want := potSherds{
		int32(itemByName["angler_pottery_sherd"]), int32(itemByName["brick"]),
		int32(itemByName["heart_pottery_sherd"]), int32(itemByName["snort_pottery_sherd"])}
	if res.sherds != want {
		t.Errorf("faces %v, want %v (back, left, right, front)", res.sherds, want)
	}
	// …and it survives a save.
	if got := unpackStack(packStack(res)); got.sherds != want {
		t.Errorf("faces %v did not survive a save", got.sherds)
	}

	// What is refused: a gap, something that is not a sherd, anything in the
	// corners.
	for _, bad := range [][4]string{
		{"angler_pottery_sherd", "", "brick", "brick"}, // only three
	} {
		if _, ok := decoratedPotMatch(sherdGrid(t, bad[0], bad[1], bad[2], bad[3])); ok {
			t.Errorf("%v should not make a pot", bad)
		}
	}
	corner := sherdGrid(t, "brick", "brick", "brick", "brick")
	corner[0] = invStack{item: int32(itemByName["brick"]), count: 1}
	if _, ok := decoratedPotMatch(corner); ok {
		t.Error("a fifth ingredient in a corner should not make a pot")
	}
	notSherd := sherdGrid(t, "brick", "brick", "brick", "")
	notSherd[7] = invStack{item: int32(itemByName["stick"]), count: 1}
	if _, ok := decoratedPotMatch(notSherd); ok {
		t.Error("a stick is not a pottery ingredient")
	}
}

// The faces reach the client twice: on the item, and on the block entity of a
// placed pot (which is what draws them on the pot itself).
func TestPotFacesOnTheWire(t *testing.T) {
	st, ok := decoratedPotMatch(sherdGrid(t,
		"angler_pottery_sherd", "brick", "heart_pottery_sherd", "snort_pottery_sherd"))
	if !ok {
		t.Fatal("no pot")
	}
	body := appendStack(nil, st)
	if _, _, ok := protocol.ReadSlot770(bytes.NewReader(body)); !ok {
		t.Fatal("the slot copier rejected the pot")
	}
	r := bytes.NewReader(body)
	for i := 0; i < 4; i++ {
		protocol.ReadVarInt(r)
	}
	if cid, _ := protocol.ReadVarInt(r); cid != componentPotDecorations {
		t.Fatalf("component %d, want pot_decorations (%d)", cid, componentPotDecorations)
	}
	n, _ := protocol.ReadVarInt(r)
	if n != 4 {
		t.Fatalf("%d faces on the wire, want 4", n)
	}
	for i := 0; i < 4; i++ {
		if got, _ := protocol.ReadVarInt(r); got != st.sherds[i] {
			t.Errorf("face %d is item %d, want %d", i, got, st.sherds[i])
		}
	}
	// The block entity speaks names, not ids.
	names := st.sherds.names()
	if names[0] != "minecraft:angler_pottery_sherd" || names[3] != "minecraft:snort_pottery_sherd" {
		t.Errorf("block-entity names %v", names)
	}
}

// A pot placed from a decorated stack keeps its faces, and a broken one hands
// them back on the drop.
func TestPotKeepsItsFacesThroughPlaceAndBreak(t *testing.T) {
	h := newHub(world.New(1))
	pos := simPos{blockPos: blockPos{5, 70, 5}}
	faces := potSherds{int32(itemByName["angler_pottery_sherd"]), 0, 0, int32(itemByName["brick"])}

	h.potSherds.set(pos, faces)
	if got, ok := h.potSherds.get(0, 5, 70, 5); !ok || got != faces {
		t.Fatalf("the store lost the faces: %v ok=%v", got, ok)
	}
	// …and the save round-trips them.
	h.containers = newContainerStore("")
	h.containers.recordPotSherds(h.potSherds)
	fresh := newPotSherdStore()
	fresh.restore(h.containers.loadPotSherds())
	if got, ok := fresh.get(0, 5, 70, 5); !ok || got != faces {
		t.Errorf("the faces did not survive a restart: %v ok=%v", got, ok)
	}
	// Breaking the block forgets them (the drop is what carries them on).
	h.spillPot(map[int32]*tracked{}, 0, pos.blockPos, 0)
	if _, ok := h.potSherds.get(0, 5, 70, 5); ok {
		t.Error("a broken pot left its faces behind in the store")
	}
}
