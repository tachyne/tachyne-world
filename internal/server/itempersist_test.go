package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The four fields that existed on invStack but never reached the persisted
// row — so every restart turned potions into water, stripped anvil names,
// reset the prior-work cost and made every goat horn play ponder.
func TestStackRowCarriesPotionNameRepairAndInstrument(t *testing.T) {
	st := invStack{item: itemByName["potion"], count: 1, potion: potHealing,
		name: "Elixir of Not Dying", repairCost: 7, instrument: 5, bundleID: 3}
	got := unpackStack(packStack(st))
	if got != st {
		t.Fatalf("round trip changed the stack:\n got %+v\nwant %+v", got, st)
	}
	// A row from a file written before these columns existed zero-fills them.
	var old [16]int32
	old[0], old[1] = itemByName["goat_horn"], 1
	var widened stackRow
	copy(widened[:], old[:])
	legacy := unpackStack(widened)
	if legacy.potion != 0 || legacy.name != "" || legacy.repairCost != 0 || legacy.instrument != 0 {
		t.Errorf("legacy row decoded with phantom values: %+v", legacy)
	}
}

// Names are interned: the same string always yields the same id, and the id
// resolves back, so two stacks named alike share one table entry.
func TestNameStoreInterns(t *testing.T) {
	n := newNameStore()
	a, b := n.intern("Excalibur"), n.intern("Excalibur")
	if a != b || a == 0 {
		t.Fatalf("intern gave %d and %d", a, b)
	}
	if n.intern("Other") == a {
		t.Fatal("distinct names share an id")
	}
	if n.get(a) != "Excalibur" || n.get(0) != "" || n.get(999) != "" {
		t.Fatalf("get: %q / %q / %q", n.get(a), n.get(0), n.get(999))
	}
	if n.intern("") != 0 {
		t.Fatal("empty name got an id")
	}
}

// The whole path a player's inventory takes across a restart, with the
// interned name table saved and reloaded through containers.json.
func TestNamedPotionSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	cs := newContainerStore(filepath.Join(dir, "containers.json"))
	names := newNameStore()
	globalNames.Store(names)
	t.Cleanup(func() { globalNames.Store(newNameStore()) })

	inv := newInvStore(filepath.Join(dir, "inventories.json"))
	pl := testTracked()
	pl.inv.slots[0] = invStack{item: itemByName["potion"], count: 1, potion: potSwiftness, name: "Go Juice", repairCost: 3}
	pl.inv.slots[1] = invStack{item: itemByName["goat_horn"], count: 1, instrument: 6}
	inv.save("Steve", pl)
	cs.recordNames(names)
	cs.flush()

	// A fresh process: the name table comes back from containers.json FIRST,
	// then the inventory decodes against it.
	cs2 := newContainerStore(filepath.Join(dir, "containers.json"))
	globalNames.Store(cs2.loadNames())
	got := testTracked()
	newInvStore(filepath.Join(dir, "inventories.json")).loadInto(got, "Steve")
	if got.inv.slots[0] != pl.inv.slots[0] {
		t.Errorf("potion after restart: %+v, want %+v", got.inv.slots[0], pl.inv.slots[0])
	}
	if got.inv.slots[1] != pl.inv.slots[1] {
		t.Errorf("horn after restart: %+v, want %+v", got.inv.slots[1], pl.inv.slots[1])
	}
}

// Dropped items carry the same fields — into the save file and back, and
// through the drop itself (they were not on the item entity at all).
func TestDroppedItemKeepsItsIdentityAcrossRestart(t *testing.T) {
	h := newHub(world.New(1))
	none := map[int32]*tracked{}
	it := h.spawnItemIn(none, 0, itemByName["potion"], 1, 10, 70, 10)
	if it == nil {
		t.Fatal("no drop")
	}
	it.potion, it.name, it.repairCost, it.instrument, it.bundleID = potPoison, "Nasty", 2, 0, 9
	saved := h.snapshotItems()
	if len(saved) != 1 {
		t.Fatalf("%d saved items", len(saved))
	}
	h2 := newHub(world.New(1))
	h2.restoreItems(saved)
	var back *itemEntity
	for _, e := range h2.items {
		back = e
	}
	if back == nil {
		t.Fatal("no item restored")
	}
	if back.potion != potPoison || back.name != "Nasty" || back.repairCost != 2 || back.bundleID != 9 {
		t.Errorf("restored drop: potion=%d name=%q repair=%d bundle=%d", back.potion, back.name, back.repairCost, back.bundleID)
	}
}

// Two drops that differ only in name must not merge on the ground; identical
// ones still do. (Stackable item on purpose — a potion stacks to one, so two
// potions never merge regardless.)
func TestDropsWithDifferentNamesDoNotMerge(t *testing.T) {
	h := newHub(world.New(1))
	none := map[int32]*tracked{}
	a := h.spawnItemIn(none, 0, itemByName["stone"], 1, 10, 70, 10)
	b := h.spawnItemIn(none, 0, itemByName["stone"], 1, 10.1, 70, 10)
	a.name = "Mine"
	h.updateItems(none)
	if len(h.items) != 2 {
		t.Fatalf("%d items after update; a named and an unnamed stack merged", len(h.items))
	}
	b.name = "Mine" // now identical
	h.updateItems(none)
	if len(h.items) != 1 {
		t.Fatalf("%d items after update; identical named stacks should merge", len(h.items))
	}
}

// What lies on the floor is rendered from the full stack: a renamed potion is
// not indistinguishable from a bottle of water until someone picks it up.
func TestGroundItemMetadataCarriesTheFullStack(t *testing.T) {
	plain := invStack{item: itemByName["potion"], count: 1}
	named := invStack{item: itemByName["potion"], count: 1, potion: potHealing, name: "Elixir"}
	a, b := itemMetadata(7, plain), itemMetadata(7, named)
	if string(a) == string(b) {
		t.Fatal("named potion renders identically to a plain one on the ground")
	}
	if len(b) <= len(a) {
		t.Errorf("named metadata (%d bytes) not longer than plain (%d)", len(b), len(a))
	}
}
