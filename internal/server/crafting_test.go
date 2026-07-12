package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Item ids used in tests (minecraft-data 1.21.5).
var (
	tOakLog      = itemByName["oak_log"]
	tOakPlanks   = itemByName["oak_planks"]
	tStick       = itemByName["stick"]
	tCraftTable  = itemByName["crafting_table"]
	tWoodPickaxe = itemByName["wooden_pickaxe"]
	tWoodAxe     = itemByName["wooden_axe"]
	tIronSword   = itemByName["iron_sword"]
)

func TestMatchShapeless(t *testing.T) {
	grid := make([]invStack, 4)
	grid[0] = invStack{item: tOakLog, count: 1}
	item, count := matchRecipe(grid, 2)
	if item != tOakPlanks || count != 4 {
		t.Fatalf("1 oak log should craft 4 oak planks, got %d x%d", item, count)
	}
}

func TestMatchShaped2x2(t *testing.T) {
	grid := make([]invStack, 4)
	for i := range grid {
		grid[i] = invStack{item: tOakPlanks, count: 1}
	}
	item, count := matchRecipe(grid, 2)
	if item != tCraftTable || count != 1 {
		t.Fatalf("2x2 planks should craft a crafting table, got %d x%d", item, count)
	}
}

func TestMatchSticksAnywhereInGrid(t *testing.T) {
	// Two planks stacked vertically — in the RIGHT column of a 3x3 grid: the
	// bounding box must normalize position so the 1x2 pattern still matches.
	grid := make([]invStack, 9)
	grid[2] = invStack{item: tOakPlanks, count: 1} // row 0, col 2
	grid[5] = invStack{item: tOakPlanks, count: 1} // row 1, col 2
	item, count := matchRecipe(grid, 3)
	if item != tStick || count != 4 {
		t.Fatalf("2 vertical planks should craft 4 sticks, got %d x%d", item, count)
	}
}

func TestMatchShaped3x3Pickaxe(t *testing.T) {
	grid := make([]invStack, 9)
	grid[0] = invStack{item: tOakPlanks, count: 1}
	grid[1] = invStack{item: tOakPlanks, count: 1}
	grid[2] = invStack{item: tOakPlanks, count: 1}
	grid[4] = invStack{item: tStick, count: 1}
	grid[7] = invStack{item: tStick, count: 1}
	item, _ := matchRecipe(grid, 3)
	if item != tWoodPickaxe {
		t.Fatalf("planks-over-sticks should craft a wooden pickaxe, got %d", item)
	}
}

func TestMatchMirroredAxe(t *testing.T) {
	// Axe: XX / X# / -#  (X planks, # stick). The mirrored placement XX / #X / #-
	// must also match (vanilla allows horizontal mirror).
	direct := make([]invStack, 9)
	direct[0] = invStack{item: tOakPlanks, count: 1}
	direct[1] = invStack{item: tOakPlanks, count: 1}
	direct[3] = invStack{item: tOakPlanks, count: 1}
	direct[4] = invStack{item: tStick, count: 1}
	direct[7] = invStack{item: tStick, count: 1}
	if item, _ := matchRecipe(direct, 3); item != tWoodAxe {
		t.Fatalf("direct axe pattern should craft a wooden axe, got %d", item)
	}
	mirror := make([]invStack, 9)
	mirror[0] = invStack{item: tOakPlanks, count: 1}
	mirror[1] = invStack{item: tOakPlanks, count: 1}
	mirror[4] = invStack{item: tOakPlanks, count: 1}
	mirror[3] = invStack{item: tStick, count: 1}
	mirror[6] = invStack{item: tStick, count: 1}
	if item, _ := matchRecipe(mirror, 3); item != tWoodAxe {
		t.Fatalf("mirrored axe pattern should craft a wooden axe, got %d", item)
	}
}

func TestMatchEmptyGrid(t *testing.T) {
	if item, _ := matchRecipe(make([]invStack, 9), 3); item != 0 {
		t.Fatalf("empty grid must match nothing, got %d", item)
	}
}

// TestClickCraftLoop drives the real click path: put a log in the 2x2 grid,
// take the planks result, and check consumption + cursor.
func TestClickCraftLoop(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: tOakLog, count: 3}

	// The client reports the move: hotbar slot (window 36) now 2 logs, grid
	// slot 1 gets 1 log, cursor holds nothing.
	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 1, mode: 0,
		changed: []slotChange{
			{slot: 36, st: invStack{item: tOakLog, count: 2}},
			{slot: 1, st: invStack{item: tOakLog, count: 1}},
		},
	})
	if pl.craft[0].item != tOakLog || pl.inv.slots[0].count != 2 {
		t.Fatalf("grid/inventory not updated: craft=%+v inv0=%+v", pl.craft[0], pl.inv.slots[0])
	}

	// Click the result slot: planks land on the cursor, the log is consumed.
	h.handleClick(players, evClick{eid: 1, windowID: 0, slot: 0, mode: 0})
	if pl.cursor.item != tOakPlanks || pl.cursor.count != 4 {
		t.Fatalf("cursor should hold 4 planks, got %+v", pl.cursor)
	}
	if pl.craft[0].item != 0 {
		t.Fatalf("crafting should consume the log, grid=%+v", pl.craft[0])
	}

	// Click result again with planks on the cursor and an empty grid: no-op.
	h.handleClick(players, evClick{eid: 1, windowID: 0, slot: 0, mode: 0})
	if pl.cursor.count != 4 {
		t.Fatalf("empty grid must not craft again, cursor=%+v", pl.cursor)
	}
}

func TestShiftClickCraftsIntoInventory(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.craft[0] = invStack{item: tOakLog, count: 1}

	h.handleClick(players, evClick{eid: 1, windowID: 0, slot: 0, mode: 1})
	if pl.inv.slots[0].item != tOakPlanks || pl.inv.slots[0].count != 4 {
		t.Fatalf("shift-craft should put planks in the inventory, got %+v", pl.inv.slots[0])
	}
	if pl.craft[0].item != 0 {
		t.Fatalf("shift-craft should consume the log, grid=%+v", pl.craft[0])
	}
}

func TestOpenAndCloseCraftingTable(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl

	h.openCraftingTable(pl)
	if pl.winID == 0 || gridSize(pl) != 3 {
		t.Fatalf("crafting table should open a 3x3 window: winID=%d grid=%d", pl.winID, gridSize(pl))
	}
	// Leave something in the grid; closing must fold it back into the inventory.
	pl.craft[4] = invStack{item: tOakPlanks, count: 2}
	h.closeWindow(players, pl)
	if pl.winID != 0 || pl.craft[4].item != 0 {
		t.Fatalf("close should reset the window and reclaim the grid: winID=%d grid=%+v", pl.winID, pl.craft[4])
	}
	if pl.inv.slots[0].item != tOakPlanks || pl.inv.slots[0].count != 2 {
		t.Fatalf("reclaimed planks should be in the inventory, got %+v", pl.inv.slots[0])
	}
}

func TestWeaponDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.p.setHotbarSlot(0, tIronSword) // held index defaults to 0

	m := &mob{eid: 2, etype: entityCow, health: cowHealth, x: 1, z: 0}
	h.mobs[2] = m
	h.attackMob(players, 1, 2)
	if want := cowHealth - 6; m.health != want {
		t.Fatalf("iron sword should hit for 6: health=%d want %d", m.health, want)
	}
}

func TestPlaceRecipeFromBook(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[3] = invStack{item: tOakLog, count: 2}

	// Find the shapeless oak-log→planks recipe's display id.
	id := -1
	for i, r := range shapelessRecipes {
		if len(r.Ingredients) == 1 && r.Ingredients[0] == tOakLog {
			id = len(shapedRecipes) + i
			break
		}
	}
	if id < 0 {
		t.Fatal("no log→planks recipe found")
	}
	h.placeRecipe(players, pl, evCraftRequest{eid: 1, windowID: 0, recipeID: int32(id)})
	if pl.craft[0].item != tOakLog || pl.craft[0].count != 1 {
		t.Fatalf("book click should place the log in the grid, got %+v", pl.craft[0])
	}
	if pl.inv.slots[3].count != 1 {
		t.Fatalf("one log should be consumed from the inventory, got %+v", pl.inv.slots[3])
	}
	if item, _ := matchRecipe(pl.craft[:4], 2); item != tOakPlanks {
		t.Fatalf("filled grid should yield planks, got %d", item)
	}
}

func TestPlaceRecipeMissingIngredients(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl // empty inventory

	h.placeRecipe(players, pl, evCraftRequest{eid: 1, windowID: 0, recipeID: int32(len(shapedRecipes))})
	for i, c := range pl.craft {
		if c.item != 0 {
			t.Fatalf("missing ingredients must not fill the grid: cell %d = %+v", i, c)
		}
	}
}

func TestPlaceRecipeTooBigForPlayerGrid(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: tOakPlanks, count: 64}
	pl.inv.slots[1] = invStack{item: tStick, count: 64}

	// Find the wooden pickaxe (3-wide) shaped recipe and request it in window 0.
	id := -1
	for i, r := range shapedRecipes {
		if r.Result == tWoodPickaxe {
			id = i
			break
		}
	}
	if id < 0 {
		t.Fatal("no pickaxe recipe found")
	}
	h.placeRecipe(players, pl, evCraftRequest{eid: 1, windowID: 0, recipeID: int32(id)})
	for i, c := range pl.craft {
		if c.item != 0 {
			t.Fatalf("3x3 recipe must not fill the 2x2 grid: cell %d = %+v", i, c)
		}
	}
}

// TestTossHeldQ: Q with no window open drops one of the held item as an entity.
func TestTossHeldQ(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: tOakPlanks, count: 5}

	h.tossHeld(players, pl, 0, false) // Q: one
	if pl.inv.slots[0].count != 4 {
		t.Fatalf("Q should consume one, slot=%+v", pl.inv.slots[0])
	}
	if len(h.items) != 1 {
		t.Fatalf("Q should spawn one item entity, got %d", len(h.items))
	}
	h.tossHeld(players, pl, 0, true) // ctrl+Q: rest of the stack
	if pl.inv.slots[0].item != 0 {
		t.Fatalf("ctrl+Q should empty the slot, got %+v", pl.inv.slots[0])
	}
	if len(h.items) != 2 {
		t.Fatalf("ctrl+Q should spawn a second entity, got %d", len(h.items))
	}
}

// TestClickOutsideDropsCursor: clicking outside the window (slot -999) with a
// stack on the cursor must spawn it as a drop, not delete it.
func TestClickOutsideDropsCursor(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.cursor = invStack{item: tOakPlanks, count: 7}

	h.handleClick(players, evClick{eid: 1, windowID: 0, slot: -999, mode: 0}) // cursor declared empty
	if pl.cursor.item != 0 {
		t.Fatalf("cursor should be empty after the throw, got %+v", pl.cursor)
	}
	found := false
	for _, it := range h.items {
		if it.item == tOakPlanks && it.count == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the thrown stack should exist as an item entity: %d entities", len(h.items))
	}
}

// TestQOverSlotDropsOne: mode-4 click (Q while hovering a slot in a window)
// reports the slot decrement; the difference must spawn as a drop.
func TestQOverSlotDropsOne(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[9] = invStack{item: tStick, count: 3}

	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 4,
		changed: []slotChange{{slot: 9, st: invStack{item: tStick, count: 2}}},
	})
	if pl.inv.slots[9].count != 2 {
		t.Fatalf("slot should have 2 left, got %+v", pl.inv.slots[9])
	}
	if len(h.items) != 1 {
		t.Fatalf("one stick should be dropped, got %d entities", len(h.items))
	}
}

// TestMoveClickDropsNothing: an ordinary move (slot -> cursor) conserves items
// and must not spawn drops.
func TestMoveClickDropsNothing(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[9] = invStack{item: tStick, count: 3}

	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 0,
		changed: []slotChange{{slot: 9, st: invStack{}}},
		cursor:  invStack{item: tStick, count: 3},
	})
	if len(h.items) != 0 {
		t.Fatalf("a pure move must not drop items, got %d entities", len(h.items))
	}
	if pl.cursor.count != 3 || pl.inv.slots[9].item != 0 {
		t.Fatalf("move not applied: cursor=%+v slot=%+v", pl.cursor, pl.inv.slots[9])
	}
}

// TestTossedItemSurvivesDespawnSweep: tosses extend pickup via noPickupUntil.
// Faking it by moving born FORWARD underflowed the unsigned despawn age and
// vanished thrown items within a second (field report).
func TestTossedItemSurvivesDespawnSweep(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: tOakPlanks, count: 1}

	h.tossHeld(players, pl, 0, false)
	if len(h.items) != 1 {
		t.Fatalf("toss should spawn an entity, got %d", len(h.items))
	}
	h.updateItems(players) // the despawn sweep must NOT eat the fresh toss
	if len(h.items) != 1 {
		t.Fatal("tossed item despawned immediately (unsigned age underflow)")
	}
	// The thrower can't hoover it back instantly…
	pl.x, pl.z = 0, 0
	for _, it := range h.items {
		pl.x, pl.y, pl.z = it.x, it.y, it.z
	}
	h.pickupItems(players)
	if len(h.items) != 1 {
		t.Fatal("toss should not be re-collectable before its delay")
	}
	// …but after the delay it is.
	for i := 0; i < 45; i++ {
		h.tick.Add(1)
	}
	h.pickupItems(players)
	if len(h.items) != 0 {
		t.Fatal("toss should be collectable after the delay")
	}
}

// TestCreativeSlotWritesThrough: a block picked in creative must land in the
// hub inventory, so pushes don't revert the hotbar and it survives a switch
// back to survival.
func TestCreativeSlotWritesThrough(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.gamemode = gmCreative
	players[1] = pl

	// Creative client sets hotbar slot 2 (window slot 38) to stone x1.
	if ptr, hot := h.winSlotPtr(pl, 38); ptr != nil {
		*ptr = invStack{item: 1, count: 1}
		if hot >= 0 {
			pl.p.setHotbarSlot(hot, 1)
		}
	}
	if pl.inv.slots[2].item != 1 {
		t.Fatalf("creative pick should land in inv slot 2, got %+v", pl.inv.slots[2])
	}
}

// TestLedgeEndDropLandsBeside: breaking the end block of a one-thick ledge must
// leave the drop on the adjacent ledge block, not sunk to the bottom of the
// cliff (items have no horizontal physics; the neighbour catch emulates the
// vanilla bounce).
func TestLedgeEndDropLandsBeside(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	// A one-block-thick ledge at y=90, hanging in open air.
	for x := 10; x <= 12; x++ {
		w.SetBlock(x, 90, 10, worldgen.Stone)
	}
	// The end block (12) was just broken: its cell is air, nothing below it.
	w.SetBlock(12, 90, 10, worldgen.Air)
	h.spawnBlockDrop(players, 35, 1, 12, 90, 10)
	if len(h.items) != 1 {
		t.Fatalf("expected one drop, got %d", len(h.items))
	}
	for _, it := range h.items {
		if it.y != 91 {
			t.Fatalf("drop should rest ON the ledge (y=91), got y=%v (x=%v z=%v)", it.y, it.x, it.z)
		}
		if int(it.x) != 11 {
			t.Fatalf("drop should land on the neighbouring ledge block x=11, got x=%v", it.x)
		}
	}
}

// TestFloorDropStaysPut: a normal floor break (support below intact) drops in
// its own cell, no neighbour magic.
func TestFloorDropStaysPut(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	w.SetBlock(20, 80, 20, worldgen.Stone) // support
	w.SetBlock(20, 81, 20, worldgen.Air)   // the broken cell
	h.spawnBlockDrop(players, 35, 1, 20, 81, 20)
	for _, it := range h.items {
		if int(it.x) != 20 || it.y != 81 {
			t.Fatalf("floor drop should stay in its cell, got (%v,%v,%v)", it.x, it.y, it.z)
		}
	}
}
