package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestAnvilMergesBookOntoSword(t *testing.T) {
	sword := invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	book := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchLooting, lvl: 2}}}
	res, cost := anvilResult(sword, book, "")
	if res.enchLvl(enchSharpness) != 3 || res.enchLvl(enchLooting) != 2 {
		t.Fatalf("book merge wrong: %+v", res)
	}
	if cost < 2 {
		t.Fatalf("merging 2 levels must cost at least 2, got %d", cost)
	}
	// Equal levels combine upward, capped.
	a := invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	b := invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	res, _ = anvilResult(a, b, "")
	if res.enchLvl(enchSharpness) != 4 {
		t.Fatalf("3+3 should combine to 4, got %d", res.enchLvl(enchSharpness))
	}
	// Incompatible sacrifice → no result.
	if r, _ := anvilResult(sword, invStack{item: 35, count: 1}, ""); r.item != 0 {
		t.Fatal("wool onto a sword must yield nothing")
	}
}

func TestAnvilRepairAndRename(t *testing.T) {
	max := itemMaxDurability[tDiamondSword]
	a := invStack{item: tDiamondSword, count: 1, dmg: max - 10} // nearly broken
	b := invStack{item: tDiamondSword, count: 1, dmg: max - 10}
	res, cost := anvilResult(a, b, "Ol' Reliable")
	if res.dmg >= a.dmg {
		t.Fatalf("repair must restore durability: dmg %d → %d", a.dmg, res.dmg)
	}
	if res.name != "Ol' Reliable" || cost < 3 {
		t.Fatalf("rename+repair: name=%q cost=%d", res.name, cost)
	}
	// Rename alone works and costs 1.
	res, cost = anvilResult(invStack{item: 35, count: 5}, invStack{}, "Wooly")
	if res.name != "Wooly" || cost != 1 {
		t.Fatalf("rename-only: %+v cost=%d", res, cost)
	}
}

func TestAnvilChargesLevels(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	pl.winID, pl.winKind = 5, winAnvil
	pl.anvil[0] = invStack{item: tDiamondSword, count: 1}
	pl.anvil[1] = invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 5}}}
	pl.xpLevel = 0
	h.takeTwoSlotResult(players, pl) // broke — rejected
	if pl.cursor.item != 0 || pl.anvil[0].item == 0 {
		t.Fatal("AUTHORITY: anvil must reject an unaffordable take")
	}
	pl.xpLevel = 10
	h.takeTwoSlotResult(players, pl)
	if pl.cursor.item != tDiamondSword || pl.cursor.enchLvl(enchSharpness) != 5 {
		t.Fatalf("result should be on the cursor: %+v", pl.cursor)
	}
	if pl.xpLevel >= 10 || pl.anvil[0].item != 0 || pl.anvil[1].item != 0 {
		t.Fatalf("inputs consumed + levels paid: lvl=%d anvil=%v", pl.xpLevel, pl.anvil)
	}
}

func TestGrindstoneStripsAndRefunds(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	pl.winID, pl.winKind = 5, winGrind
	pl.anvil[0] = invStack{item: tDiamondSword, count: 1, ench: enchList{{id: enchSharpness, lvl: 5}}}
	h.takeTwoSlotResult(players, pl)
	if pl.cursor.item != tDiamondSword || pl.cursor.enchanted() {
		t.Fatalf("grindstone must strip enchants: %+v", pl.cursor)
	}
	if len(h.orbs) != 1 {
		t.Fatal("stripping must refund XP as an orb")
	}
	// An enchanted book grinds back to a plain book.
	res, _ := grindResult(invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchFortune, lvl: 2}}}, invStack{})
	if res.item != itemBook {
		t.Fatalf("ground book should be plain, got item %d", res.item)
	}
}

func TestSilkTouchAndFortune(t *testing.T) {
	if silkTouchDrop[worldgen.DiamondOre] != 78 {
		t.Fatal("silk map should yield the ore block item")
	}
	if !isOreState(worldgen.CoalOre) || isOreState(worldgen.Stone) {
		t.Fatal("ore-state test wrong")
	}
}

func TestLootingBoostsMobDrops(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	total := 0
	for i := 0; i < 40; i++ {
		m := h.spawnHostile(players, entityZombie, 5, 5)
		m.hitByPlayer, m.looting = true, 3
		h.despawnMob(players, m)
	}
	for _, it := range h.items {
		if it.item == itemRottenFlesh {
			total += it.count
		}
	}
	// Plain zombies average 1/kill (0-2); Looting III should push well past that.
	if total < 60 {
		t.Fatalf("looting III over 40 kills yielded only %d flesh", total)
	}
}

// AnvilMenu.onTake: an anvil wears with use — one take in eight chips it a
// stage, and a damaged one goes altogether — with the use/broken sounds as
// level events the client plays.
func TestAnvilWearsWithUse(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pos := blockPos{2, 70, 2}
	h.world.SetBlock(pos.x, pos.y, pos.z, anvilStateMin)
	h.openAnvil(pl, pos)
	if pl.winPos.blockPos != pos {
		t.Fatalf("the anvil window should know its block: %+v", pl.winPos)
	}
	stages := 0
	for i := 0; i < 400 && h.world.At(pos.x, pos.y, pos.z) != worldgen.Air; i++ {
		before := h.world.At(pos.x, pos.y, pos.z)
		h.wearAnvil(players, pl)
		if after := h.world.At(pos.x, pos.y, pos.z); after != before {
			stages++
			if after != worldgen.Air && after != before+4 {
				t.Fatalf("wear should step a stage (facing kept): %d -> %d", before, after)
			}
		}
	}
	if stages != 3 || h.world.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Fatalf("four hundred uses should chip it twice and break it: %d changes, block %d", stages, h.world.At(pos.x, pos.y, pos.z))
	}
	drainEvents(pl)
	h.world.SetBlock(pos.x, pos.y, pos.z, anvilStateMin)
	pl.gamemode = gmCreative
	for i := 0; i < 200; i++ {
		h.wearAnvil(players, pl)
	}
	if h.world.At(pos.x, pos.y, pos.z) != anvilStateMin {
		t.Fatal("creative use must not wear the anvil")
	}
}
