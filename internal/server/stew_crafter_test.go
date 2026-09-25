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
// recipes work in it — a tipped arrow here, and the map recipes too
// (CrafterBlock.dispenseFrom assembles every crafting recipe; a crafter
// used to refuse map cloning and extending and book cloning).
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
}

// crafterRig is a powered-off crafter facing east at (5,180,5) with its
// grid, on loaded chunks.
func crafterRig(t *testing.T) (*hub, simPos, uint32, *bin) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	h.maps, h.books = newMapStore(""), newBookStore("")
	h.playersRef = map[int32]*tracked{}
	pos := simPos{blockPos: blockPos{5, 180, 5}}
	state := crafterMin + 24 + uint32(9*2) + 1 // facing east, untriggered
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	c := &bin{slots: make([]invStack, 9)}
	h.bins[pos] = c
	return h, pos, state, c
}

func ejected(h *hub) []invStack {
	var out []invStack
	for _, it := range h.items {
		out = append(out, it.stack())
	}
	return out
}

// A crafter extends a map (a new map one scale out) and clones one (the
// same map id, one copy per empty map).
func TestCrafterExtendsAndClonesMaps(t *testing.T) {
	h, pos, state, c := crafterRig(t)
	src := h.maps.create(0, 0, 0, 0)
	for i := 0; i < 9; i++ {
		c.slots[i] = invStack{item: itemPaper, count: 1}
	}
	c.slots[4] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
	if !h.crafterCraft(h.playersRef, pos, state) {
		t.Fatal("a crafter should extend a map")
	}
	out := ejected(h)
	if len(out) != 1 || out[0].item != itemFilledMap || out[0].mapID == src.ID {
		t.Fatalf("one new map should come out: %+v", out)
	}
	if md := h.maps.get(out[0].mapID); md == nil || md.Scale != src.Scale+1 {
		t.Fatalf("the new map is one scale out: %+v", md)
	}
	for i := range c.slots {
		if c.slots[i].count != 0 {
			t.Fatalf("every ingredient is used: slot %d %+v", i, c.slots[i])
		}
	}

	for eid := range h.items {
		delete(h.items, eid)
	}
	c.slots[0] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
	c.slots[1] = invStack{item: itemEmptyMap, count: 1}
	if !h.crafterCraft(h.playersRef, pos, state) {
		t.Fatal("a crafter should clone a map")
	}
	out = ejected(h)
	if len(out) != 1 || out[0].count != 2 || out[0].mapID != src.ID {
		t.Fatalf("two copies of the map should come out: %+v", out)
	}
}

// A crafter copies a written book: the copies (a generation up) come out,
// and so does the original, which getRemainingItems hands back; a cake
// gives back its three milk buckets' empties.
func TestCrafterClonesBooksAndEjectsRemainders(t *testing.T) {
	h, pos, state, c := crafterRig(t)
	id := h.books.create(savedBook{Title: "T", Author: "A", Pages: []string{"p"}})
	c.slots[0] = invStack{item: itemWrittenBook, count: 1, bookID: id}
	c.slots[1] = invStack{item: itemWritableBook, count: 1}
	if !h.crafterCraft(h.playersRef, pos, state) {
		t.Fatal("a crafter should copy a book")
	}
	var original, copies int
	for _, st := range ejected(h) {
		switch {
		case st.item == itemWrittenBook && st.bookID == id:
			original += st.count
		case st.item == itemWrittenBook:
			if b, _ := h.books.get(st.bookID); b.Gen != 1 || b.Title != "T" {
				t.Fatalf("the copy is generation 1 with the content: %+v", b)
			}
			copies += st.count
		}
	}
	if original != 1 || copies != 1 || c.slots[0].count != 0 || c.slots[1].count != 0 {
		t.Fatalf("want the original and one copy out and an empty grid: original %d copies %d grid %+v", original, copies, c.slots[:2])
	}

	for eid := range h.items {
		delete(h.items, eid)
	}
	milk, wheat := int32(itemByName["milk_bucket"]), int32(itemByName["wheat"])
	for i := 0; i < 3; i++ {
		c.slots[i] = invStack{item: milk, count: 1}
		c.slots[6+i] = invStack{item: wheat, count: 1}
	}
	c.slots[3] = invStack{item: itemByName["sugar"], count: 1}
	c.slots[4] = invStack{item: itemByName["egg"], count: 1}
	c.slots[5] = invStack{item: itemByName["sugar"], count: 1}
	if !h.crafterCraft(h.playersRef, pos, state) {
		t.Fatal("a crafter should bake a cake")
	}
	buckets := 0
	for _, st := range ejected(h) {
		if st.item == itemBucket {
			buckets += st.count
		}
	}
	if buckets != 3 {
		t.Fatalf("the cake's three milk buckets come back empty, got %d", buckets)
	}
}
