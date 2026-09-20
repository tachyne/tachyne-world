package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestCrafterKeepsSuspiciousStew: a crafter that assembles a suspicious stew
// ejects it with its flower, and a tossed one keeps it too.
func TestCrafterKeepsSuspiciousStew(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := crafterMin + 24 + uint32(9*2) + 1 // facing east, untriggered
	onHub(t, h, func() {
		pos := blockPos{5, 70, 5}
		w.SetBlock(pos.x, pos.y, pos.z, state)
		c := &bin{slots: make([]invStack, 9)}
		c.slots[0] = invStack{item: itemByName["bowl"], count: 1}
		c.slots[1] = invStack{item: itemByName["red_mushroom"], count: 1}
		c.slots[2] = invStack{item: itemByName["brown_mushroom"], count: 1}
		c.slots[3] = invStack{item: itemByName["dandelion"], count: 1}
		h.bins[simPos{blockPos: pos}] = c
		res := h.crafterResult(c)
		if res.item != itemSuspiciousStew || res.stew == 0 {
			t.Fatalf("the preview should be a suspicious stew with a flower: %+v", res)
		}
		h.crafterCraft(h.playersRef, simPos{blockPos: pos}, state)
		found := false
		for _, it := range h.items {
			if it.item == itemSuspiciousStew {
				found = true
				if it.stew != res.stew || it.stack().stew != res.stew {
					t.Fatalf("the ejected stew lost its flower: entity %d stack %d want %d", it.stew, it.stack().stew, res.stew)
				}
			}
		}
		if !found {
			t.Fatal("no stew ejected")
		}
		// A player tossing one keeps it as well.
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		pl.x, pl.y, pl.z = 20.5, 70, 20.5
		h.tossItem(players, pl, invStack{item: itemSuspiciousStew, count: 1, stew: res.stew})
		for _, it := range h.items {
			if it.thrower == pl.p.eid && it.stew != res.stew {
				t.Fatalf("the tossed stew lost its flower: %d", it.stew)
			}
		}
	})
}

// The crafter runs the same resolver a crafting table does, so the special
// recipes work in it — a tipped arrow here — while the map recipes, which
// mint a new map when a player takes them, do not.
func TestCrafterMakesSpecialRecipes(t *testing.T) {
	h := newHub(world.New(1))
	c := &bin{slots: make([]invStack, 9)}
	// Eight arrows around a lingering potion: vanilla's tipped-arrow recipe.
	for i := 0; i < 9; i++ {
		c.slots[i] = invStack{item: itemByName["arrow"], count: 1}
	}
	c.slots[4] = invStack{item: itemLingerPotion, count: 1, potion: potSwiftness}
	res := h.crafterResult(c)
	if res.item != itemByName["tipped_arrow"] || res.potion != potSwiftness {
		t.Fatalf("a crafter should tip arrows: %+v", res)
	}
	// A filled map ringed by paper zooms out in a table, but not in a crafter.
	for i := 0; i < 9; i++ {
		c.slots[i] = invStack{item: itemByName["paper"], count: 1}
	}
	c.slots[4] = invStack{item: itemFilledMap, count: 1, mapID: 1}
	if got := h.crafterResult(c); got.item != 0 {
		t.Errorf("the map recipes stay out of the crafter: %+v", got)
	}
}
