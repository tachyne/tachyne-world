package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func craftPlayer(h *hub) (*tracked, map[int32]*tracked) {
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.winKind = winCraft
	return pl, players
}

// RepairItemRecipe: two damaged pickaxes make one with both remainders
// plus five percent, curses kept and every other enchantment dropped.
func TestCraftRepairsTwoTools(t *testing.T) {
	h := newHub(world.New(1))
	pick := int32(itemByName["iron_pickaxe"])
	maxDmg := itemMaxDurability[pick]
	grid := make([]invStack, 9)
	grid[0] = invStack{item: pick, count: 1, dmg: maxDmg - 50, ench: enchList{{id: enchVanishingCurse, lvl: 1}, {id: 1, lvl: 3}}}
	grid[1] = invStack{item: pick, count: 1, dmg: maxDmg - 30}
	res, _ := h.craftResult(grid, 3)
	if res.item != pick || res.count != 1 {
		t.Fatalf("no repair: %+v", res)
	}
	if want := max(maxDmg-(50+30+maxDmg*5/100), 0); res.dmg != want {
		t.Fatalf("repaired damage %d, want %d", res.dmg, want)
	}
	if res.ench[0].id != enchVanishingCurse || res.ench[1].id != 0 {
		t.Fatalf("only curses survive a repair: %+v", res.ench)
	}
	grid[1].dmg = 0
	grid[1].item = int32(itemByName["diamond_pickaxe"])
	if res, _ := h.craftResult(grid, 3); res.item != 0 {
		t.Fatal("different tools do not combine")
	}
}

// TippedArrowRecipe: a lingering potion ringed by eight arrows makes eight
// tipped arrows carrying it.
func TestCraftTippedArrows(t *testing.T) {
	h := newHub(world.New(1))
	grid := make([]invStack, 9)
	for i := range grid {
		grid[i] = invStack{item: itemArrow, count: 1}
	}
	grid[4] = invStack{item: itemLingerPotion, count: 1, potion: 7}
	res, _ := h.craftResult(grid, 3)
	if res.item != itemTippedArrow || res.count != 8 || res.potion != 7 {
		t.Fatalf("tipped arrows: %+v", res)
	}
	grid[4].item = itemPotion
	if res, _ := h.craftResult(grid, 3); res.item == itemTippedArrow {
		t.Fatal("only a lingering potion tips arrows")
	}
}

// crafting_transmute: a shulker box and a dye recolour it with its contents;
// so does a bundle.
func TestCraftTransmutesBoxesAndBundles(t *testing.T) {
	h := newHub(world.New(1))
	grid := make([]invStack, 4)
	grid[0] = invStack{item: int32(itemByName["shulker_box"]), count: 1, boxID: 42}
	grid[1] = invStack{item: int32(itemByName["red_dye"]), count: 1}
	res, _ := h.craftResult(grid, 2)
	if res.item != int32(itemByName["red_shulker_box"]) || res.count != 1 || res.boxID != 42 {
		t.Fatalf("red shulker box with its contents: %+v", res)
	}
	grid[0] = invStack{item: int32(itemByName["bundle"]), count: 1, bundleID: 7}
	res, _ = h.craftResult(grid, 2)
	if res.item != int32(itemByName["red_bundle"]) || res.bundleID != 7 {
		t.Fatalf("red bundle with its contents: %+v", res)
	}
	grid[0] = invStack{item: int32(itemByName["red_bundle"]), count: 1}
	if res, _ := h.craftResult(grid, 2); res.item == int32(itemByName["red_bundle"]) {
		t.Fatal("the same colour again is no recipe")
	}
}

// BannerDuplicateRecipe: a patterned banner and a blank one of its colour
// make a copy, and the patterned one is left in the grid.
func TestCraftDuplicatesBanner(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := craftPlayer(h)
	banner := int32(itemByName["white_banner"])
	pl.craft[0] = invStack{item: banner, count: 1, pats: [6]bannerLayer{{patPlus1: 3, color: 14}}}
	pl.craft[1] = invStack{item: banner, count: 2}
	res, kind := h.craftResult(pl.craft[:9], 3)
	if res.item != banner || res.count != 1 || res.pats[0].patPlus1 != 3 || kind != craftKeepPattern {
		t.Fatalf("banner copy: %+v kind %d", res, kind)
	}
	h.takeCraftResult(players, pl, 0)
	if pl.cursor.item != banner || pl.cursor.pats[0].patPlus1 != 3 {
		t.Fatalf("the copy should be on the cursor: %+v", pl.cursor)
	}
	if pl.craft[0].count != 1 || pl.craft[1].count != 1 {
		t.Fatalf("the patterned banner stays, one blank is spent: %+v / %+v", pl.craft[0], pl.craft[1])
	}
}

// FireworkRocketRecipe: paper and one to three gunpowder make three rockets.
func TestCraftFireworkRockets(t *testing.T) {
	h := newHub(world.New(1))
	grid := make([]invStack, 9)
	grid[0] = invStack{item: itemPaper, count: 1}
	grid[1] = invStack{item: itemGunpowder, count: 1}
	grid[2] = invStack{item: itemGunpowder, count: 1}
	if res, _ := h.craftResult(grid, 3); res.item != itemFireworks || res.count != 3 {
		t.Fatalf("rockets: %+v", res)
	}
	grid[3], grid[4] = invStack{item: itemGunpowder, count: 1}, invStack{item: itemGunpowder, count: 1}
	if res, _ := h.craftResult(grid, 3); res.item == itemFireworks {
		t.Fatal("four gunpowder is too much")
	}
}

// BookCloningRecipe: a written book and quills make copies a generation up
// (born at take time), the original stays, and a copy of a copy is final.
func TestCraftClonesBooks(t *testing.T) {
	h := newHub(world.New(1))
	h.books = newBookStore("")
	pl, players := craftPlayer(h)
	id := h.books.create(savedBook{Title: "T", Author: "A", Pages: []string{"p"}})
	pl.craft[0] = invStack{item: itemWrittenBook, count: 1, bookID: id}
	pl.craft[1] = invStack{item: itemWritableBook, count: 2}
	res, kind := h.craftResult(pl.craft[:9], 3)
	if res.item != itemWrittenBook || res.count != 2 || kind != craftBookClone {
		t.Fatalf("book copies: %+v kind %d", res, kind)
	}
	h.takeCraftResult(players, pl, 0)
	if pl.cursor.item != itemWrittenBook || pl.cursor.count != 2 || pl.cursor.bookID == id || pl.cursor.bookID == 0 {
		t.Fatalf("two fresh copies on the cursor: %+v", pl.cursor)
	}
	if b, _ := h.books.get(pl.cursor.bookID); b.Gen != 1 || b.Title != "T" {
		t.Fatalf("a copy is generation 1 with the content: %+v", b)
	}
	if pl.craft[0].count != 1 || pl.craft[1].count != 1 {
		t.Fatalf("the original stays, one quill spent: %+v / %+v", pl.craft[0], pl.craft[1])
	}
	final := h.books.create(savedBook{Title: "T", Gen: 2})
	pl.craft[0] = invStack{item: itemWrittenBook, count: 1, bookID: final}
	if res, _ := h.craftResult(pl.craft[:9], 3); res.item != 0 {
		t.Fatal("a copy of a copy cannot be copied")
	}
}
