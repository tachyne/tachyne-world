package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An anvil mends a tool with its own material — the common case that used to
// be impossible (only two of the same item combined).
func TestAnvilRepairsWithMaterial(t *testing.T) {
	max := itemMaxDurability[tDiamondSword]
	sword := invStack{item: tDiamondSword, count: 1, dmg: max - 1,
		ench: enchList{{id: enchSharpness, lvl: 3}}}
	diamonds := invStack{item: itemByName["diamond"], count: 5}
	res, cost := anvilResult(sword, diamonds, "")
	if res.item != tDiamondSword || res.enchLvl(enchSharpness) != 3 {
		t.Fatalf("repair must keep the item and its enchantments: %+v", res)
	}
	// Each diamond mends a quarter of the maximum; four bring it to full.
	if res.dmg != 0 {
		t.Fatalf("four diamonds should mend a nearly-broken sword fully, dmg=%d", res.dmg)
	}
	if cost != 4 {
		t.Fatalf("one level per diamond used: cost=%d", cost)
	}
	// An undamaged item has nothing to mend.
	if r, _ := anvilResult(invStack{item: tDiamondSword, count: 1}, diamonds, ""); r.item != 0 {
		t.Fatal("a pristine sword + diamonds must yield no result")
	}
	// The wrong material is still rejected.
	if r, _ := anvilResult(sword, invStack{item: itemByName["iron_ingot"], count: 5}, ""); r.item != 0 {
		t.Fatal("iron must not repair a diamond sword")
	}
	// Tier and named materials both land.
	if !repairsWith(itemByName["iron_pickaxe"], itemByName["iron_ingot"]) ||
		!repairsWith(itemByName["turtle_helmet"], itemByName["turtle_scute"]) ||
		!repairsWith(itemByName["elytra"], itemByName["phantom_membrane"]) ||
		!repairsWith(itemByName["shield"], itemByName["oak_planks"]) {
		t.Fatal("repair materials table is missing entries")
	}
}

// Two enchanted books combine into one: a book takes any enchantment, which
// is how a library is built up before anything touches a tool.
func TestAnvilMergesBookOntoBook(t *testing.T) {
	a := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	b := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	if res, _ := anvilResult(a, b, ""); res.enchLvl(enchSharpness) != 4 {
		t.Fatalf("two Sharpness III books make a IV: %+v", res)
	}
	// Different enchantments both ride on the result even though neither can
	// go on a book's "item".
	c := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchUnbreaking, lvl: 2}}}
	res, cost := anvilResult(a, c, "")
	if res.enchLvl(enchSharpness) != 3 || res.enchLvl(enchUnbreaking) != 2 || cost < 1 {
		t.Fatalf("book library merge: %+v cost=%d", res, cost)
	}
	// A book of Smite onto a sword already carrying Sharpness is refused.
	sword := invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 1}}}
	smite := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSmite, lvl: 1}}}
	if res, _ := anvilResult(sword, smite, ""); res.item != 0 {
		t.Fatalf("an incompatible book alone yields nothing: %+v", res)
	}
}

// Taking a material repair consumes only the materials it actually used.
func TestAnvilMaterialRepairConsumesWhatItUses(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	max := itemMaxDurability[tDiamondSword]
	pl.winID, pl.winKind = 5, winAnvil
	pl.anvil[0] = invStack{item: tDiamondSword, count: 1, dmg: max / 4}
	pl.anvil[1] = invStack{item: itemByName["diamond"], count: 9}
	pl.xpLevel = 30
	h.takeTwoSlotResult(players, pl, 0)
	if pl.cursor.item != tDiamondSword || pl.cursor.dmg != 0 {
		t.Fatalf("result should be the mended sword: %+v", pl.cursor)
	}
	if pl.anvil[1].count != 8 {
		t.Fatalf("one diamond covers a quarter of the bar, 8 left; got %d", pl.anvil[1].count)
	}
}

// Renaming forever: a rename-only job never passes 39 levels and never raises
// the item's prior-work cost.
func TestAnvilRenameOnlyStaysCheap(t *testing.T) {
	st := invStack{item: tDiamondSword, count: 1, repairCost: 60, name: "old"}
	res, cost := anvilResult(st, invStack{}, "new")
	if cost != anvilMaxCost-1 {
		t.Fatalf("rename-only caps at 39, got %d", cost)
	}
	if res.repairCost != 60 {
		t.Fatalf("rename-only must not raise the prior-work cost: %d", res.repairCost)
	}
	// A rename that rides along with real work is charged normally.
	dmg := invStack{item: tDiamondSword, count: 1, dmg: 100, repairCost: 1}
	res, _ = anvilResult(dmg, invStack{item: itemByName["diamond"], count: 1}, "new")
	if res.repairCost != 3 {
		t.Fatalf("real work doubles the prior-work cost: %d", res.repairCost)
	}
}

// The grindstone keeps curses (from both inputs), merges durability, rebuilds
// the prior-work cost from what survives, and pays vanilla's XP.
func TestGrindstoneVanillaRules(t *testing.T) {
	max := itemMaxDurability[tDiamondSword]
	a := invStack{item: tDiamondSword, count: 1, dmg: max / 2, repairCost: 15,
		ench: enchList{{id: enchSharpness, lvl: 4}, {id: enchVanishingCurse, lvl: 1}}}
	res, _ := grindResult(a, invStack{})
	if res.enchLvl(enchSharpness) != 0 {
		t.Fatal("the grindstone must strip ordinary enchantments")
	}
	if res.enchLvl(enchVanishingCurse) != 1 {
		t.Fatal("curses survive the grindstone")
	}
	if res.repairCost != 1 {
		t.Fatalf("one surviving curse rebuilds the prior-work cost to 1, got %d", res.repairCost)
	}
	// Two of the same item merge: durability adds, plus 5% of the maximum.
	b := invStack{item: tDiamondSword, count: 1, dmg: max / 2,
		ench: enchList{{id: enchBindingCurse, lvl: 1}}}
	res, xp := grindResult(a, b)
	left := (max - a.dmg) + (max - b.dmg) + max*5/100
	if res.dmg != maxInt(max-left, 0) {
		t.Fatalf("merged durability wrong: dmg=%d want %d", res.dmg, maxInt(max-left, 0))
	}
	if res.enchLvl(enchVanishingCurse) != 1 || res.enchLvl(enchBindingCurse) != 1 {
		t.Fatalf("both inputs' curses come across: %+v", res.ench)
	}
	// XP back is the sum of the non-curse enchantments' minimum cost.
	if want := enchDefs[enchSharpness].minCost(4); xp != want {
		t.Fatalf("grindstone XP: got %d want %d", xp, want)
	}
	// A cursed-only item has nothing to give back.
	if _, xp := grindResult(invStack{item: tDiamondSword, count: 1,
		ench: enchList{{id: enchVanishingCurse, lvl: 1}}}, invStack{}); xp != 0 {
		t.Fatalf("curses pay no XP, got %d", xp)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// A bookshelf only powers the table when the cell halfway to it is open.
func TestBookshelfNeedsAnAirGap(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(10, 10)
	y := h.world.SurfaceFeet(lx, lz)
	pos := simPos{blockPos: blockPos{lx, y, lz}}
	w := h.world
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			for dy := 0; dy <= 2; dy++ {
				w.SetBlock(lx+dx, y+dy, lz+dz, worldgen.Air)
			}
		}
	}
	w.SetBlock(lx+2, y, lz, bookshelfState)
	if n := h.countBookshelves(pos); n != 1 {
		t.Fatalf("a shelf with a clear gap counts: %d", n)
	}
	w.SetBlock(lx+1, y, lz, worldgen.Stone) // wall the gap off
	if n := h.countBookshelves(pos); n != 0 {
		t.Fatalf("a walled-off shelf must not count: %d", n)
	}
}

// The offers hold until something is enchanted: same seed, same three.
func TestEnchantOffersHoldUntilUsed(t *testing.T) {
	h, pl, players := enchSetup(t)
	pl.xpLevel = 30
	pl.enchSlots[0] = invStack{item: tDiamondSword, count: 1}
	pl.enchSlots[1] = invStack{item: itemLapisLazuli, count: 3}
	h.rollEnchOptions(pl)
	first, seed := pl.enchOpts, pl.enchSeed
	if seed == 0 {
		t.Fatal("rolling must give the player a seed")
	}
	h.rollEnchOptions(pl) // reopening the table re-rolls nothing
	if pl.enchOpts != first {
		t.Fatalf("offers must hold: %+v then %+v", first, pl.enchOpts)
	}
	row := -1
	for i, o := range pl.enchOpts {
		if o.cost > 0 && o.cost <= pl.xpLevel {
			row = i
		}
	}
	if row < 0 {
		t.Skip("no affordable offer rolled for this seed")
	}
	h.handleEnchant(players, pl, int32(row))
	if pl.enchSeed == seed {
		t.Fatal("a spent enchant must burn the seed")
	}
	pl.enchSlots[0] = invStack{item: tDiamondSword, count: 1}
	h.rollEnchOptions(pl)
	if pl.enchOpts == first {
		t.Fatal("the next item should get a fresh three")
	}
}

// The seed rides the player's saved data, so a relog keeps the same offers.
func TestEnchantSeedPersists(t *testing.T) {
	s := &invStore{m: map[string]*savedInv{}}
	pl := testTracked()
	pl.enchSeed = 12345
	s.record("tester", pl)
	back := testTracked()
	s.loadInto(back, "tester")
	if back.enchSeed != 12345 {
		t.Fatalf("enchantment seed must survive a relog, got %d", back.enchSeed)
	}
}
