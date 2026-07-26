package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An ender chest is a door onto the PLAYER's storage, so two of them anywhere
// in the world show the same 27 slots.
func TestEnderChestIsPerPlayerNotPerBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.openEnderChest(players, pl, 0, 70, 0)
	if pl.viewChest != pl.enderChest() {
		t.Fatal("the window is not looking at the player's own storage")
	}
	pl.enderChest().slots[0] = invStack{item: itemByName["diamond"], count: 3}

	// A different block, far away: same contents.
	h.openEnderChest(players, pl, 5000, 70, -5000)
	if got := pl.viewChest.slots[0]; got.item != itemByName["diamond"] || got.count != 3 {
		t.Errorf("a second ender chest showed %+v, want the same diamonds", got)
	}
	// …and it is not backed by any block storage.
	if len(h.chests) != 0 {
		t.Errorf("%d block chests created by opening ender chests, want 0", len(h.chests))
	}
}

// Two players never see each other's ender contents.
func TestEnderChestsAreSeparatePerPlayer(t *testing.T) {
	h := newHub(world.New(1))
	a, b := survPlayer(h), survPlayer(h)
	a.enderChest().slots[0] = invStack{item: itemByName["diamond"], count: 1}
	if b.enderChest().slots[0].item != 0 {
		t.Error("a second player can see the first's ender chest")
	}
}

// The ender chest survives a logout, since it rides the player's saved
// inventory rather than any block.
func TestEnderChestPersists(t *testing.T) {
	store := newInvStore(t.TempDir() + "/inv.json")
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.enderChest().slots[4] = invStack{item: itemByName["emerald"], count: 7, dmg: 0}
	store.save("wesley", pl)

	back := survPlayer(h)
	store.loadInto(back, "wesley")
	if got := back.enderChest().slots[4]; got.item != itemByName["emerald"] || got.count != 7 {
		t.Errorf("reloaded ender slot %+v, want 7 emeralds", got)
	}
}

// The point of a shulker box: breaking it keeps what is inside.
func TestShulkerBoxKeepsItsContents(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{3, 70, 3}

	c := &chest{}
	c.slots[0] = invStack{item: itemByName["diamond"], count: 5}
	c.slots[26] = invStack{item: itemByName["emerald"], count: 2}
	h.chests[pos] = c

	boxID := h.stowShulkerBox(pos)
	if boxID == 0 {
		t.Fatal("a box with contents stowed as empty")
	}
	if _, still := h.chests[pos]; still {
		t.Error("the block still has storage after being stowed")
	}

	// Placed again somewhere else, the contents come back.
	dest := blockPos{-40, 12, 900}
	h.restoreShulkerBox(dest, boxID)
	back := h.chests[dest]
	if back == nil {
		t.Fatal("the box restored no storage")
	}
	if got := back.slots[0]; got.item != itemByName["diamond"] || got.count != 5 {
		t.Errorf("slot 0 came back as %+v, want 5 diamonds", got)
	}
	if got := back.slots[26]; got.item != itemByName["emerald"] || got.count != 2 {
		t.Errorf("slot 26 came back as %+v, want 2 emeralds", got)
	}
	if _, leaked := h.boxes[boxID]; leaked {
		t.Error("the box id was not retired once its contents were placed")
	}
}

// An EMPTY box needs no id — it drops as a plain item.
func TestEmptyShulkerBoxNeedsNoIdentity(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{1, 70, 1}
	h.chests[pos] = &chest{}
	if id := h.stowShulkerBox(pos); id != 0 {
		t.Errorf("an empty box minted id %d, want 0", id)
	}
	if len(h.boxes) != 0 {
		t.Errorf("%d box records for an empty box, want 0", len(h.boxes))
	}
}

// Contents in transit survive a restart.
func TestShulkerContentsPersist(t *testing.T) {
	dir := t.TempDir()
	store := newContainerStore(dir + "/containers.json")
	boxes := map[int32]*chest{}
	c := &chest{}
	c.slots[2] = invStack{item: itemByName["diamond"], count: 9}
	boxes[7] = c
	store.recordBoxes(boxes, 7)

	back, next := store.loadBoxes()
	if next != 7 {
		t.Errorf("next box id %d, want 7", next)
	}
	if back[7] == nil {
		t.Fatal("box 7 did not come back")
	}
	if got := back[7].slots[2]; got.item != itemByName["diamond"] || got.count != 9 {
		t.Errorf("reloaded contents %+v, want 9 diamonds", got)
	}
}

// The box identity has to ride the stack through the save format, or a box in
// a chest comes back empty.
func TestBoxIDSurvivesTheStackRow(t *testing.T) {
	st := invStack{item: itemByName["shulker_box"], count: 1, boxID: 42}
	if got := unpackStack(packStack(st)); got.boxID != 42 {
		t.Errorf("boxID %d after a round trip, want 42", got.boxID)
	}
	// A short row from an older file must still decode, zero-filling the new
	// column rather than failing.
	var old stackRow
	old[0], old[1] = itemByName["stone"], 3
	if got := unpackStack(old); got.item != itemByName["stone"] || got.count != 3 || got.boxID != 0 {
		t.Errorf("legacy row decoded as %+v", got)
	}
}

// isShulkerBox must cover every colour, not just the plain one.
func TestShulkerBoxStatesCoverEveryColour(t *testing.T) {
	for _, name := range []string{"shulker_box", "white_shulker_box", "red_shulker_box",
		"black_shulker_box", "cyan_shulker_box"} {
		lo, hi := worldgen.BlockRange(name)
		if lo == 0 {
			t.Fatalf("%s has no block states", name)
		}
		for _, s := range []uint32{lo, hi} {
			if !isShulkerBox(s) {
				t.Errorf("%s state %d not recognised as a shulker box", name, s)
			}
		}
	}
	if isShulkerBox(worldgen.BlockBase("chest")) {
		t.Error("a plain chest was taken for a shulker box")
	}
}
