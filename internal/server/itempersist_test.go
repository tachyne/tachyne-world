package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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
	globalNames.Store(h.names) // the name table survives the restart, as boot loads it before the drops
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

// A dropped stack comes back whole through the save FILE: every field a slot
// carries, not the subset the flat columns once listed (dyed colour, a
// rocket's flight and bursts, and a pot's faces were lost on every restart).
func TestDroppedItemSurvivesTheSaveFileWhole(t *testing.T) {
	h := newHub(world.New(1))
	none := map[int32]*tracked{}
	want := invStack{item: itemByName["leather_chestplate"], count: 1, dmg: 7, name: "Red Coat",
		color: 0xB02E26, potion: potPoison, repairCost: 3, instrument: 2, flight: 3, starID: 5,
		sherds: potSherds{itemByName["arms_up_pottery_sherd"], 0, 0, itemByName["skull_pottery_sherd"]},
		stew:   2, shieldBase: 4, bundleID: 6, boxID: 8, hiveID: 9, bookID: 10, mapID: 11,
		trimMat: 2, trimPat: 3}
	want.ench[0] = enchApply{id: 1, lvl: 2}
	want.lode = lodeTracker{has: true, target: true, x: 5, y: 64, z: -9}
	want.pats[0] = bannerLayer{patPlus1: 3, color: 1}
	it := h.spawnItemIn(none, 0, want.item, want.count, 10, 70, 10)
	if it == nil {
		t.Fatal("no drop")
	}
	it.setFrom(want)

	path := filepath.Join(t.TempDir(), "containers.json")
	cs := newContainerStore(path)
	cs.recordItems(h.snapshotItems())
	cs.recordNames(h.names)
	cs.flush()

	h2 := newHub(world.New(1))
	back := newContainerStore(path)
	globalNames.Store(back.loadNames())
	h2.restoreItems(back.loadItems())
	if len(h2.items) != 1 {
		t.Fatalf("%d items restored", len(h2.items))
	}
	for _, e := range h2.items {
		if got := e.stack(); got != want {
			t.Errorf("restored drop:\n got %+v\nwant %+v", got, want)
		}
	}
}

// A drop saved before stacks were packed whole (flat columns) still restores.
func TestDroppedItemLegacyFlatRowRestores(t *testing.T) {
	h := newHub(world.New(1))
	h.restoreItems([]savedItem{{X: 10, Y: 70, Z: 10, Item: itemByName["potion"], Count: 1, Potion: potPoison, Repair: 2}})
	for _, e := range h.items {
		if e.item != itemByName["potion"] || e.potion != potPoison || e.repairCost != 2 {
			t.Errorf("legacy drop restored as %+v", e.stack())
		}
		return
	}
	t.Fatal("no legacy drop restored")
}

// The shutdown save writes the name table AFTER every store has packed its
// stacks: a name first interned by a frame's stack at that moment used to be
// minted after the table was recorded and was gone after the restart.
func TestShutdownSaveKeepsANameInternedLate(t *testing.T) {
	h := newHub(world.New(1))
	path := filepath.Join(t.TempDir(), "containers.json")
	h.containers = newContainerStore(path)
	startHub(t, h)
	onHub(t, h, func() { // after boot, which restores the frames from the store
		h.itemFrames[900] = &itemFrame{eid: 900, x: 1, y: 70, z: 1, dir: 2,
			held: invStack{item: itemByName["diamond"], count: 1, name: "Late Name 6c1f"}}
	})
	done := make(chan struct{})
	h.post(evSaveState{done: done})
	select {
	case <-done:
	case <-time.After(hubTestWait):
		t.Fatal("save did not finish")
	}
	var f containerFile
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Frames) != 1 {
		t.Fatalf("%d frames saved", len(f.Frames))
	}
	id := f.Frames[0].Item[19]
	if got := f.Names[strconv.Itoa(int(id))]; got != "Late Name 6c1f" {
		t.Errorf("frame's name id %d resolves to %q in the saved table", id, got)
	}
}
