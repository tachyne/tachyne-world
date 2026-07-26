package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Sheep were always white: the fleece byte's colour bits were hard-zero, so
// every sheep in the world looked the same and dye did nothing.
func TestSheepColoursVary(t *testing.T) {
	h := newHub(world.New(1))
	seen := map[int8]int{}
	for i := 0; i < 3000; i++ {
		seen[h.rollSheepColor()]++
	}
	if len(seen) < 4 {
		t.Errorf("only %d distinct colours in 3000 rolls, want the vanilla spread", len(seen))
	}
	// White dominates, as in vanilla (~82%).
	if w := seen[0]; w < 2200 || w > 2700 {
		t.Errorf("%d white of 3000, want about 2450", w)
	}
	if seen[6] == 0 {
		t.Log("no pink in 3000 rolls — rare but possible at 1-in-600")
	}
}

// The fleece byte carries colour in the low bits and sheared at 0x10.
func TestFleeceByteLayout(t *testing.T) {
	for _, c := range []struct {
		color   int8
		sheared bool
		want    byte
	}{
		{0, false, 0x00},
		{15, false, 0x0f},
		{0, true, 0x10},
		{12, true, 0x1c},
	} {
		got := sheepFleeceMeta(1, c.color, c.sheared)
		// [eid][index][type][value][end]
		if v := got[len(got)-2]; v != c.want {
			t.Errorf("colour %d sheared=%v -> %#x, want %#x", c.color, c.sheared, v, c.want)
		}
	}
}

// A dye recolours a live sheep, and the wool follows the fleece.
func TestDyeSheepAndWool(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMobIn(players, entitySheep, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.color = 0

	if !h.dyeSheep(players, m, int32(itemByName["red_dye"])) {
		t.Fatal("red dye did not take")
	}
	if m.color != 14 {
		t.Errorf("colour %d after red dye, want 14", m.color)
	}
	if got, want := sheepWool(m), int32(itemByName["red_wool"]); got != want {
		t.Errorf("wool %d, want red %d", got, want)
	}
	// The same dye twice is a no-op, so it does not eat the second dye.
	if h.dyeSheep(players, m, int32(itemByName["red_dye"])) {
		t.Error("re-dyeing the same colour reported a change")
	}
	// Not a dye, and not a sheep.
	if h.dyeSheep(players, m, int32(itemByName["stone"])) {
		t.Error("stone dyed a sheep")
	}
	cow := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if cow != nil && h.dyeSheep(players, cow, int32(itemByName["red_dye"])) {
		t.Error("a cow was dyed")
	}
}

// A name tag renames a mob and makes it persistent — the second part is what
// keeps a pet where you left it.
func TestNameTagRenamesAndPersists(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	m := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}

	// A blank tag does nothing: vanilla makes you name it on an anvil first.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["name_tag"]), count: 1}
	if h.tryNameTag(players, pl, m) {
		t.Error("an unnamed tag renamed the mob")
	}

	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["name_tag"]), count: 1, name: "Bessie"}
	if !h.tryNameTag(players, pl, m) {
		t.Fatal("a named tag did not rename the mob")
	}
	if m.customName != "Bessie" {
		t.Errorf("name %q, want Bessie", m.customName)
	}
	if !m.named() {
		t.Error("a named mob does not report as named — it would still despawn")
	}
	if pl.inv.slots[pl.p.heldSlot()].item != 0 {
		t.Error("the tag was not consumed")
	}
}

// Colour and name survive a save/reload round trip.
func TestSheepColourAndNamePersist(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMobIn(players, entitySheep, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.color, m.customName = 11, "Cloud"

	sm := toSavedMob(m)
	h2 := newHub(world.New(1))
	back := h2.reloadMob(players, &sm)
	if back == nil {
		t.Fatal("reload returned nil")
	}
	if back.color != 11 {
		t.Errorf("reloaded colour %d, want 11", back.color)
	}
	if back.customName != "Cloud" {
		t.Errorf("reloaded name %q, want Cloud", back.customName)
	}
}
