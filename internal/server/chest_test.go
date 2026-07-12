package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func chestSetup() (*hub, map[int32]*tracked, *tracked, *chest) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	h.world.SetBlock(10, 70, 10, chestStateMin+1) // a placed chest (default state)
	h.openChest(pl, 10, 70, 10)
	return h, players, pl, h.chests[blockPos{10, 70, 10}]
}

func TestChestStoresAcrossReopen(t *testing.T) {
	h, players, pl, c := chestSetup()
	if c == nil || pl.winID == 0 || pl.winKind != winChest {
		t.Fatalf("chest window should be open: win=%d kind=%d", pl.winID, pl.winKind)
	}
	pl.inv.slots[0] = invStack{item: 1, count: 10} // stone in the hotbar
	// Client moves 4 stone from hotbar (chest window slot 54) into chest slot 5.
	h.handleClick(players, evClick{
		eid: 1, windowID: pl.winID, slot: 5, mode: 0,
		changed: []slotChange{
			{slot: 54, st: invStack{item: 1, count: 6}},
			{slot: 5, st: invStack{item: 1, count: 4}},
		},
	})
	if c.slots[5].item != 1 || c.slots[5].count != 4 {
		t.Fatalf("chest slot 5 should hold 4 stone, got %+v", c.slots[5])
	}
	if pl.inv.slots[0].count != 6 {
		t.Fatalf("hotbar should have 6 left, got %+v", pl.inv.slots[0])
	}
	h.closeWindow(players, pl)
	if pl.winID != 0 || pl.winKind != winPlayer {
		t.Fatalf("window should be closed: %d/%d", pl.winID, pl.winKind)
	}
	h.openChest(pl, 10, 70, 10)
	c2 := h.chests[blockPos{10, 70, 10}]
	if c2 != c || c2.slots[5].count != 4 {
		t.Fatalf("chest must keep contents across reopen, got %+v", c2.slots[5])
	}
}

func TestChestSlot0IsStorageNotCraftResult(t *testing.T) {
	h, players, pl, c := chestSetup()
	pl.inv.slots[0] = invStack{item: 1, count: 1}
	// A click landing an item in chest slot 0 must be trust-applied, NOT treated
	// as a crafting-result take (slot 0 special-case only applies to craft windows).
	h.handleClick(players, evClick{
		eid: 1, windowID: pl.winID, slot: 0, mode: 0,
		changed: []slotChange{
			{slot: 54, st: invStack{}},
			{slot: 0, st: invStack{item: 1, count: 1}},
		},
	})
	if c.slots[0].item != 1 {
		t.Fatalf("chest slot 0 should hold the stone, got %+v", c.slots[0])
	}
}

func TestChestSpillsOnBreak(t *testing.T) {
	h, players, pl, c := chestSetup()
	c.slots[3] = invStack{item: 1, count: 12}
	c.slots[20] = invStack{item: itemByName["wheat_seeds"], count: 5}
	h.closeWindow(players, pl)
	before := len(h.items)
	// The chest block is broken (dig path posts evBlock with the new air state).
	h.world.SetBlock(10, 70, 10, worldgen.Air)
	h.onBlock(players, evBlock{x: 10, y: 70, z: 10, state: worldgen.Air})
	if len(h.items)-before != 2 {
		t.Fatalf("breaking the chest should spill 2 stacks, spawned %d", len(h.items)-before)
	}
	if h.chests[blockPos{10, 70, 10}] != nil {
		t.Fatal("chest state should be forgotten after the break")
	}
}

func TestChestNotSpilledByOwnStateChange(t *testing.T) {
	h, players, pl, c := chestSetup()
	c.slots[0] = invStack{item: 1, count: 1}
	h.closeWindow(players, pl)
	// A chest→chest state change (e.g. waterlogged flag) must NOT spill.
	h.onBlock(players, evBlock{x: 10, y: 70, z: 10, state: chestStateMin})
	if h.chests[blockPos{10, 70, 10}] == nil {
		t.Fatal("chest must survive a same-block state change")
	}
}

func TestContainerStoreChestRoundTrip(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	s := newContainerStore(path)
	in := map[blockPos]*chest{{-4, 65, 900}: {}}
	in[blockPos{-4, 65, 900}].slots[0] = invStack{item: 1, count: 64}
	in[blockPos{-4, 65, 900}].slots[26] = invStack{item: itemByName["wheat_seeds"], count: 7}
	s.recordChests(in)
	s.flush()

	got := newContainerStore(path).loadChests()
	c := got[blockPos{-4, 65, 900}]
	if c == nil {
		t.Fatal("chest not restored")
	}
	if c.slots[0] != (invStack{item: 1, count: 64}) || c.slots[26] != (invStack{item: itemByName["wheat_seeds"], count: 7}) {
		t.Fatalf("slots mismatch: %+v %+v", c.slots[0], c.slots[26])
	}
}
