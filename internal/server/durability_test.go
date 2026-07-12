package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

var tWoodPick = itemByName["wooden_pickaxe"] // 59 durability, from items_meta_gen

func TestToolWearAndBreak(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.inv.slots[0] = invStack{item: tWoodPick, count: 1}
	max := itemMaxDurability[tWoodPick]
	if max == 0 {
		t.Fatal("wooden pickaxe must have a durability entry")
	}
	for i := 0; i < max-1; i++ {
		h.applyToolWear(pl, 0, 1)
	}
	if s := pl.inv.slots[0]; s.item != tWoodPick || s.dmg != max-1 {
		t.Fatalf("tool should be at max-1 wear, got %+v", s)
	}
	h.applyToolWear(pl, 0, 1)
	if s := pl.inv.slots[0]; s.item != 0 || s.count != 0 {
		t.Fatalf("tool should break at max durability, got %+v", s)
	}
}

func TestToolWearIgnoresNonTools(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.inv.slots[0] = invStack{item: 1, count: 64} // stone
	h.applyToolWear(pl, 0, 1)
	if s := pl.inv.slots[0]; s.dmg != 0 || s.count != 64 {
		t.Fatalf("non-durability items must not wear, got %+v", s)
	}
}

// TestCopperEquipmentTier locks in the 1.21.11 copper tier across all three
// stat tables: melee damage, mining speed, and armor points (helmet 2, chest 4,
// legs 5, boots 1 = 12 total, no toughness — vanilla copper values).
func TestCopperEquipmentTier(t *testing.T) {
	melee := map[string]int{"copper_sword": 5, "copper_axe": 9, "copper_pickaxe": 3, "copper_shovel": 3}
	for name, want := range melee {
		id := itemByName[name]
		if got := meleeDamage[id]; got != want {
			t.Errorf("meleeDamage[%s]=%d want %d", name, got, want)
		}
		if got := toolSpeed[id]; got != 5 {
			t.Errorf("toolSpeed[%s]=%v want 5", name, got)
		}
	}
	armor := map[string]int{"copper_helmet": 2, "copper_chestplate": 4, "copper_leggings": 5, "copper_boots": 1}
	total := 0
	for name, want := range armor {
		p, ok := armorInfo[itemByName[name]]
		if !ok {
			t.Fatalf("%s missing from armorInfo", name)
		}
		if p.Points != want {
			t.Errorf("armorInfo[%s].Points=%d want %d", name, p.Points, want)
		}
		if p.Toughness != 0 {
			t.Errorf("copper %s toughness=%v want 0", name, p.Toughness)
		}
		total += p.Points
	}
	if total != 12 {
		t.Errorf("copper armor total=%d want 12", total)
	}
}

func TestArmorReducesDamage(t *testing.T) {
	pl := testTracked()
	if got := pl.armorReduce(3); got != 3 {
		t.Fatalf("no armor = full damage, got %v", got)
	}
	// Full iron: 15 points, 0 toughness. Vanilla formula for a 3-damage hit:
	// def = min(20, max(15/5, 15-3/2)) = 13.5 -> 3 * (1-13.5/25) = 1.38
	equipSet(t, pl, [4]int{2, 6, 5, 2}, 0) // iron points per piece
	got := pl.armorReduce(3)
	if got < 1.37 || got > 1.39 {
		t.Fatalf("full iron vs 3 damage should reduce to ~1.38, got %v", got)
	}
}

// equipSet fills pl.armor with pieces matching the wanted points signature.
func equipSet(t *testing.T, pl *tracked, points [4]int, toughness float64) {
	t.Helper()
	for id, p := range armorInfo {
		if p.Slot >= 0 && p.Slot < 4 && p.Points == points[p.Slot] && p.Toughness == toughness {
			pl.armor[p.Slot] = invStack{item: id, count: 1}
		}
	}
	for i, a := range pl.armor {
		if a.item == 0 {
			t.Fatalf("could not find armor piece for slot %d", i)
		}
	}
}

func TestArmorWearsAndShatters(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	equipSet(t, pl, [4]int{2, 6, 5, 2}, 0)
	helmet := pl.armor[0].item
	max := itemMaxDurability[helmet]
	if max == 0 {
		t.Fatal("armor must have durability")
	}
	h.wearArmor(players, pl, 3) // 3 damage -> max(1, 3/4) = 1 wear per piece
	if pl.armor[0].dmg != 1 || pl.armor[3].dmg != 1 {
		t.Fatalf("each piece should wear 1, got %d/%d", pl.armor[0].dmg, pl.armor[3].dmg)
	}
	pl.armor[0].dmg = max - 1
	h.wearArmor(players, pl, 3)
	if pl.armor[0].item != 0 {
		t.Fatalf("helmet should shatter at max, got %+v", pl.armor[0])
	}
}

func TestSwordsDoNotStack(t *testing.T) {
	if stackCap(itemByName["wooden_sword"]) != 1 { // wooden_sword
		t.Fatalf("swords must not stack, cap=%d", stackCap(itemByName["wooden_sword"]))
	}
	if stackCap(1) != 64 { // stone
		t.Fatalf("stone should stack to 64, cap=%d", stackCap(1))
	}
}

func TestClickPreservesToolDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: tWoodPick, count: 1, dmg: 30}
	// Client moves the pick from hotbar (window 36) to a main slot (window 9);
	// its declared states carry no damage — the server must keep the wear.
	h.handleClick(players, evClick{
		eid: 1, windowID: 0, slot: 9, mode: 0,
		changed: []slotChange{
			{slot: 36, st: invStack{}},
			{slot: 9, st: invStack{item: tWoodPick, count: 1}},
		},
	})
	if s := pl.inv.slots[9]; s.dmg != 30 {
		t.Fatalf("inventory move must keep tool damage, got %+v", s)
	}
}

func TestPickupAndDeathDropKeepDamage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.x, pl.y, pl.z = 0.5, h.world.SurfaceY(0, 0), 0.5
	pl.inv.slots[0] = invStack{item: tWoodPick, count: 1, dmg: 40}

	h.damage(players, pl, 25) // death scatters the inventory
	var dropped *itemEntity
	for _, it := range h.items {
		dropped = it
	}
	if dropped == nil || dropped.dmg != 40 {
		t.Fatalf("death drop must carry tool damage, got %+v", dropped)
	}

	h.respawn(pl)
	pl.x, pl.y, pl.z = dropped.x, dropped.y, dropped.z
	h.tick.Add(pickupDelay + 1)
	h.pickupItems(players)
	found := false
	for _, s := range pl.inv.slots {
		if s.item == tWoodPick && s.dmg == 40 {
			found = true
		}
	}
	if !found {
		t.Fatalf("picked-up tool must keep its damage: %+v", pl.inv.slots[:3])
	}
}

func TestInvStoreKeepsDamage(t *testing.T) {
	path := t.TempDir() + "/inv.json"
	s := newInvStore(path)
	pl := testTracked()
	pl.inv.slots[4] = invStack{item: tWoodPick, count: 1, dmg: 17}
	s.save("Steve", pl)
	got := testTracked()
	newInvStore(path).loadInto(got, "Steve")
	if got.inv.slots[4].dmg != 17 {
		t.Fatalf("durability must survive the store round-trip, got %+v", got.inv.slots[4])
	}
}
