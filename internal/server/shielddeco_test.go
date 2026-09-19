package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// ShieldDecorationRecipe: a plain shield and a banner make the shield with
// the banner's layers over the banner's colour; the shield keeps its own
// wear. A decorated shield takes no second banner, and the base rides the
// wire as base_color and the save as its own column.
func TestCraftDecoratesShield(t *testing.T) {
	h := newHub(world.New(1))
	grid := make([]invStack, 9)
	red := int32(itemByName["red_banner"])
	grid[0] = invStack{item: int32(itemShield), count: 1, dmg: 7}
	grid[4] = invStack{item: red, count: 1, pats: [6]bannerLayer{{patPlus1: 3, color: 0}}}
	res, _ := h.craftResult(grid, 3)
	if res.item != int32(itemShield) || res.count != 1 || res.dmg != 7 || res.pats[0].patPlus1 != 3 || res.shieldBase != 15 {
		t.Fatalf("decorated shield: %+v", res)
	}
	grid[0] = res
	if again, _ := h.craftResult(grid, 3); again.item == int32(itemShield) {
		t.Fatal("a decorated shield takes no second banner")
	}
	grid[0] = invStack{item: int32(itemShield), count: 1}
	grid[4] = invStack{item: red, count: 1} // a blank banner still colours the base
	if res, _ := h.craftResult(grid, 3); res.item != int32(itemShield) || res.shieldBase != 15 || res.patCount() != 0 {
		t.Fatalf("blank banner: %+v", res)
	}

	comps := stackComponents(invStack{item: int32(itemShield), count: 1, shieldBase: 15})
	r := bytes.NewReader(comps)
	n, _ := protocol.ReadVarInt(r)
	protocol.ReadVarInt(r)
	found := false
	for i := int32(0); i < n; i++ {
		id, _ := protocol.ReadVarInt(r)
		if id == componentBaseColor {
			if dye, _ := protocol.ReadVarInt(r); dye != 14 {
				t.Fatalf("base_color dye %d, want 14 (red)", dye)
			}
			found = true
			break
		}
		t.Fatalf("unexpected component %d before base_color", id)
	}
	if !found {
		t.Fatal("base_color missing from the wire components")
	}
	if back := unpackStack(packStack(invStack{item: int32(itemShield), count: 1, shieldBase: 15})); back.shieldBase != 15 {
		t.Fatalf("the base does not survive the save row: %+v", back)
	}
}
