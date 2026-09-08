package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func shelfState(name, facing string, powered bool, part string) uint32 {
	base := worldgen.BlockBase(name)
	info, _ := worldgen.InfoForState(base)
	s := worldgen.SetProperty(info, base, "facing", facing)
	s = setBoolProp(s, "powered", powered)
	s = worldgen.SetProperty(info, s, "side_chain", part)
	return setBoolProp(s, "waterlogged", false)
}

// An unpowered shelf swaps the held stack in and out of the clicked column;
// a powered row of three swaps its nine slots with the hotbar; the
// comparator reads a bit per filled slot; a broken shelf drops its stacks.
func TestWoodShelf(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	pos := blockPos{5, 180, 5}
	w.SetBlock(pos.x, pos.y, pos.z, shelfState("oak_shelf", "north", false, "unconnected"))
	diamond := int32(itemByName["diamond"])
	pl.inv.slots[0] = invStack{item: diamond, count: 7}
	pl.p.setHotbarSlot(0, diamond)
	pl.p.held = 0
	// North face (2): face-local x = 1-cx, so cx=0.9 is the left column (0).
	h.useWoodShelf(players, evUseWoodShelf{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, face: 2, cx: 0.9, cy: 0.5, cz: 0})
	sh := h.woodShelves[simPos{blockPos: pos}]
	if sh == nil || sh[0].item != diamond || sh[0].count != 7 || pl.inv.slots[0].item != 0 {
		t.Fatalf("the whole stack should go onto the shelf: shelf=%v hand=%+v", sh, pl.inv.slots[0])
	}
	if sig := h.analogSignal(simPos{blockPos: pos}); sig != 1 {
		t.Fatalf("one filled slot reads %d, want 1", sig)
	}
	if v, ok := h.shelfView.get(0, pos.x, pos.y, pos.z); !ok || v.Items[0].Name != "minecraft:diamond" || v.Items[0].Count != 7 {
		t.Fatalf("the chunk view should show the diamonds: %+v", v)
	}
	// The side face is not a slot.
	h.useWoodShelf(players, evUseWoodShelf{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, face: 4, cx: 0, cy: 0.5, cz: 0.5})
	if sh[0].item != diamond {
		t.Fatal("a click on the side must not touch the slots")
	}
	// Empty hand takes it back.
	h.useWoodShelf(players, evUseWoodShelf{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, face: 2, cx: 0.9, cy: 0.5, cz: 0})
	if sh[0].item != 0 || pl.inv.slots[0].item != diamond || pl.inv.slots[0].count != 7 {
		t.Fatalf("an empty hand takes the stack back: shelf=%v hand=%+v", sh[0], pl.inv.slots[0])
	}

	// A powered row of three facing north: left is east (clockwise of north).
	for i, part := range []string{"left", "center", "right"} {
		w.SetBlock(pos.x+2-i*1, pos.y, pos.z, shelfState("oak_shelf", "north", true, part)) // x=7 left … x=5 right
	}
	chain := h.shelfChain(w, blockPos{6, pos.y, pos.z}, w.At(6, pos.y, pos.z))
	if len(chain) != 3 || chain[0].x != 7 || chain[2].x != 5 {
		t.Fatalf("chain = %v, want x 7,6,5 left to right", chain)
	}
	stick := int32(itemByName["stick"])
	for i := 0; i < 9; i++ {
		pl.inv.slots[i] = invStack{item: stick, count: i + 1}
	}
	h.useWoodShelf(players, evUseWoodShelf{eid: pl.p.eid, x: 6, y: pos.y, z: pos.z, face: 2, cx: 0.5, cy: 0.5, cz: 0})
	right := h.woodShelves[simPos{blockPos: blockPos{5, pos.y, pos.z}}]
	left := h.woodShelves[simPos{blockPos: blockPos{7, pos.y, pos.z}}]
	if right == nil || left == nil || right[0].count != 7 || right[2].count != 9 || left[0].count != 1 {
		t.Fatalf("the hotbar should map onto the row left to right: left=%v right=%v", left, right)
	}
	for i := 0; i < 9; i++ {
		if pl.inv.slots[i].item != 0 {
			t.Fatalf("hotbar slot %d should be empty after the swap: %+v", i, pl.inv.slots[i])
		}
	}
	// Breaking a shelf drops what it holds.
	items := len(h.items)
	h.spillWoodShelf(players, simPos{blockPos: blockPos{5, pos.y, pos.z}})
	if len(h.items) != items+3 || h.woodShelves[simPos{blockPos: blockPos{5, pos.y, pos.z}}] != nil {
		t.Fatalf("breaking should drop the three stacks: %d→%d", items, len(h.items))
	}
}

// Powering a shelf beside a powered one links them; unpowering unlinks.
func TestWoodShelfChainsOnPower(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	y := 180
	a := shelfState("oak_shelf", "north", true, "unconnected")
	w.SetBlock(5, y, 5, a)
	w.SetBlock(6, y, 5, a) // to the "left" (east) of x=5
	h.shelfPowerUp(players, 0, blockPos{5, y, 5}, a, worldgen.Air)
	if p5, p6 := shelfPart(w.At(5, y, 5)), shelfPart(w.At(6, y, 5)); p5 != "right" || p6 != "left" {
		t.Fatalf("parts after linking = %q,%q; want right,left", p5, p6)
	}
	w.SetBlock(4, y, 5, a)
	h.shelfPowerUp(players, 0, blockPos{4, y, 5}, a, worldgen.Air)
	if p5, p4 := shelfPart(w.At(5, y, 5)), shelfPart(w.At(4, y, 5)); p5 != "center" || p4 != "right" {
		t.Fatalf("a third shelf makes the middle a center: %q,%q", p5, p4)
	}
	w.SetBlock(3, y, 5, a)
	h.shelfPowerUp(players, 0, blockPos{3, y, 5}, a, worldgen.Air)
	if shelfPart(w.At(3, y, 5)) != "unconnected" {
		t.Fatal("a fourth shelf cannot join a full chain")
	}
	// Unpowering the middle breaks the chain.
	off := setBoolProp(w.At(5, y, 5), "powered", false)
	h.shelfPowerDown(players, 0, blockPos{5, y, 5}, off)
	if shelfPart(w.At(6, y, 5)) != "unconnected" || shelfPart(w.At(4, y, 5)) != "unconnected" {
		t.Fatalf("neighbours should let go: %q %q", shelfPart(w.At(6, y, 5)), shelfPart(w.At(4, y, 5)))
	}
}
