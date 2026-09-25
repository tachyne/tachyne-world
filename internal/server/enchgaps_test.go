package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Efficiency was not modelled at all, and the blanket allowance the fast-break
// check used instead was far too small: a legitimately enchanted player broke
// blocks faster than allowed and had every break reverted.
func TestEfficiencyIsAllowedByTheFastBreakCheck(t *testing.T) {
	stone := worldgen.BlockBase("stone")
	pick := int32(itemByName["wooden_pickaxe"])

	plain := minDigTicks(stone, pick, 0, 1)
	effV := minDigTicks(stone, pick, float64(efficiencyBonus(invStack{
		item: pick, count: 1, ench: enchList{{id: enchEfficiency, lvl: 5}}})), 1)

	if effV >= plain {
		t.Errorf("Efficiency V allowed %d ticks, plain %d — it must allow a faster break", effV, plain)
	}
	// Efficiency V adds 26 to a wooden pickaxe's speed of 2: a fourteen-fold
	// speed-up, far beyond the old flat allowance.
	if got := efficiencyBonus(invStack{item: pick, count: 1,
		ench: enchList{{id: enchEfficiency, lvl: 5}}}); got != 26 {
		t.Errorf("Efficiency V bonus = %d, want 26 (5^2+1)", got)
	}
}

// Mending: experience mends held and worn gear before it reaches the bar.
func TestMendingRepairsBeforeBanking(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)

	pick := itemByName["diamond_pickaxe"]
	pl.inv.slots[0] = invStack{item: pick, count: 1, dmg: 100,
		ench: enchList{{id: enchMending, lvl: 1}}}

	left := h.mendingRepair(pl, 10) // 10 xp = 20 durability
	if got := pl.inv.slots[0].dmg; got != 80 {
		t.Errorf("damage %d after mending, want 80", got)
	}
	if left != 0 {
		t.Errorf("%d experience left over, want 0 — it should all have been spent", left)
	}

	// An undamaged item takes nothing, so the xp passes straight through.
	pl.inv.slots[0].dmg = 0
	if got := h.mendingRepair(pl, 7); got != 7 {
		t.Errorf("undamaged gear swallowed experience: %d left, want 7", got)
	}

	// Worn armour counts too, and a nearly-mended item leaves the remainder.
	pl.armor[0] = invStack{item: itemByName["diamond_helmet"], count: 1, dmg: 3,
		ench: enchList{{id: enchMending, lvl: 1}}}
	left = h.mendingRepair(pl, 10)
	if pl.armor[0].dmg != 0 {
		t.Errorf("armour damage %d after mending, want 0", pl.armor[0].dmg)
	}
	if left != 8 { // 3 damage costs 2 xp (rounded up), 8 remain
		t.Errorf("%d experience left, want 8", left)
	}
}

// Without Mending, nothing is repaired and all the experience is banked.
func TestNoMendingNoRepair(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.inv.slots[0] = invStack{item: itemByName["diamond_pickaxe"], count: 1, dmg: 100}
	if got := h.mendingRepair(pl, 10); got != 10 {
		t.Errorf("%d experience left, want all 10", got)
	}
	if pl.inv.slots[0].dmg != 100 {
		t.Error("gear without Mending was repaired anyway")
	}
}

// getRandomItemWith(REPAIR_WITH_XP): only EQUIPPED items are mended — both
// hands and the armour — not a tool lying idle on the hotbar.
func TestMendingOnlyEquipped(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pick := invStack{item: itemByName["diamond_pickaxe"], count: 1, dmg: 100, ench: enchList{{id: enchMending, lvl: 1}}}
	pl.inv.slots[5] = pick // not the held slot
	if got := h.mendingRepair(pl, 10); got != 10 || pl.inv.slots[5].dmg != 100 {
		t.Errorf("an idle hotbar tool was mended (%d xp left, dmg %d)", got, pl.inv.slots[5].dmg)
	}
	pl.offhand = pick
	h.mendingRepair(pl, 10)
	if pl.offhand.dmg != 80 {
		t.Errorf("the offhand item was not mended (dmg %d)", pl.offhand.dmg)
	}
}

// isValidBookShelf (26.3): the cell between the table and a shelf may hold
// anything in #replaceable — grass counts, a torch still blocks.
func TestBookshelfTransmitter(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			for y := 100; y <= 101; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(2, 100, 0, bookshelfState) // one shelf, due east
	pos := simPos{blockPos: blockPos{0, 100, 0}}
	h.world.SetBlock(1, 100, 0, worldgen.BlockBase("short_grass"))
	if n := h.countBookshelves(pos); n != 1 {
		t.Errorf("a shelf behind short grass counts %d, want 1", n)
	}
	h.world.SetBlock(1, 100, 0, worldgen.BlockBase("torch"))
	if n := h.countBookshelves(pos); n != 0 {
		t.Errorf("a shelf behind a torch counts %d, want 0", n)
	}
}
