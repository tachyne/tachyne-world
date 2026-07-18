package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestEatRestoresFood(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.food = 10
	pl.inv.slots[0] = invStack{item: itemByName["apple"], count: 2} // 2 apples (4 hunger each)
	h.eat(nil, pl, 0)
	if pl.food != 14 {
		t.Errorf("eating an apple should restore 4 hunger: food=%d", pl.food)
	}
	if pl.inv.slots[0].count != 1 {
		t.Errorf("eating should consume one apple: count=%d", pl.inv.slots[0].count)
	}
	pl.food = maxFood // already full → can't eat
	h.eat(nil, pl, 0)
	if pl.inv.slots[0].count != 1 {
		t.Errorf("should not eat when full: count=%d", pl.inv.slots[0].count)
	}
}

func TestInventoryPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inv.json")
	st := newInvStore(path)
	pl := testTracked()
	pl.inv.add(35, 64)                                                           // a stack of cobblestone
	pl.inv.add(itemByName["apple"], 3)                                           // some apples
	pl.armor = [4]invStack{{item: itemByName["stone_shovel"], count: 1, dmg: 9}} // a worn helmet stays worn
	pl.offhand = invStack{item: itemByName["wheat_seeds"], count: 4}             // torches in the offhand
	pl.xpLevel, pl.xpPoints = 12, 5                                              // hard-won levels persist too
	st.save("Steve", pl)

	reloaded := newInvStore(path)
	got := testTracked()
	reloaded.loadInto(got, "Steve")
	if got.inv.slots[0] != (invStack{item: 35, count: 64}) || got.inv.slots[1] != (invStack{item: itemByName["apple"], count: 3}) {
		t.Fatalf("inventory did not survive round-trip: %+v", got.inv.slots[:2])
	}
	if got.armor[0] != pl.armor[0] {
		t.Fatalf("worn armor did not survive round-trip: %+v", got.armor[0])
	}
	if got.offhand != pl.offhand {
		t.Fatalf("offhand did not survive round-trip: %+v", got.offhand)
	}
	if got.xpLevel != 12 || got.xpPoints != 5 {
		t.Fatalf("experience did not survive round-trip: level=%d points=%d", got.xpLevel, got.xpPoints)
	}
	// An unknown player loads as empty.
	empty := testTracked()
	reloaded.loadInto(empty, "Nobody")
	if empty.inv.slots[0].count != 0 || empty.xpLevel != 0 {
		t.Errorf("unknown player should load empty, got %+v", empty.inv.slots[0])
	}
}

func TestInvStoreMigratesLegacyFormat(t *testing.T) {
	// Pre-armor files stored a bare 36-row array per player. Loading one must
	// keep the slots and default armor/offhand to empty — NOT error out and
	// silently wipe everyone's inventory.
	path := filepath.Join(t.TempDir(), "inv.json")
	legacy := fmt.Sprintf(`{"Steve": [[35,64,0],[%d,3,0]`, itemByName["apple"]) // 2 filled + 34 empty rows
	for i := 0; i < 34; i++ {
		legacy += `,[0,0,0]`
	}
	legacy += `]}`
	if err := writeFileForTest(path, legacy); err != nil {
		t.Fatal(err)
	}
	st := newInvStore(path)
	got := testTracked()
	st.loadInto(got, "Steve")
	if got.inv.slots[0] != (invStack{item: 35, count: 64}) || got.inv.slots[1] != (invStack{item: itemByName["apple"], count: 3}) {
		t.Fatalf("legacy inventory not migrated: %+v", got.inv.slots[:2])
	}
	if got.armor[0].item != 0 || got.offhand.item != 0 {
		t.Fatalf("legacy load should leave armor/offhand empty: %+v %+v", got.armor[0], got.offhand)
	}
}

// TestMigrateItemIDs: inventory + container item-id migration remaps saved ids
// through the map, skips empty (0), and leaves count/dmg untouched.
func TestMigrateItemIDs(t *testing.T) {
	remap := func(id int32) int32 {
		if id == 840 {
			return 893 // apple 1.21.5 -> 1.21.11
		}
		return id
	}
	// inventory
	inv := &invStore{m: map[string]*savedInv{"Steve": {}}}
	inv.m["Steve"].Slots[0] = [13]int32{840, 5} // apple
	inv.m["Steve"].Slots[1] = [13]int32{}       // empty
	inv.m["Steve"].Armor[0] = [13]int32{840, 1, 3}
	if n := inv.migrateItemIDs(remap); n != 2 {
		t.Fatalf("inv migrate n=%d, want 2", n)
	}
	if got := inv.m["Steve"].Slots[0]; got != [13]int32{893, 5} {
		t.Errorf("slot0 = %v, want [893 5 0 0]", got)
	}
	if got := inv.m["Steve"].Armor[0]; got != [13]int32{893, 1, 3} {
		t.Errorf("armor0 = %v, want [893 1 3 0]", got)
	}
	// container: chest row (slot,item,count,dmg,ench) + furnace slot (item,count,dmg)
	cs := &containerStore{}
	cs.m.Chests = map[string][][14]int32{"0,0,0": {{0, 840, 3}}}
	cs.m.Furnaces = map[string]savedFurnace{"1,1,1": {Slots: [3][3]int32{{840, 1, 0}, {}, {}}}}
	if n := cs.migrateItemIDs(remap); n != 2 {
		t.Fatalf("container migrate n=%d, want 2", n)
	}
	if cs.m.Chests["0,0,0"][0][1] != 893 {
		t.Errorf("chest item = %d, want 893", cs.m.Chests["0,0,0"][0][1])
	}
	if cs.m.Furnaces["1,1,1"].Slots[0][0] != 893 {
		t.Errorf("furnace item = %d, want 893", cs.m.Furnaces["1,1,1"].Slots[0][0])
	}
}

// TestSavedPositionRoundTrip: a recorded player's last position comes back via
// savedPos; new players and legacy (no-position) entries return ok=false so the
// caller falls back to world spawn.
func TestSavedPositionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventories.json")
	s := newInvStore(path)

	tr := testTracked()
	tr.x, tr.y, tr.z = 123.5, 72, -840.25
	tr.yaw, tr.pitch = 90, -12
	tr.dim = 0
	s.save("wanderer", tr) // record + flush

	// New player → no saved position.
	if _, _, _, _, _, _, ok := s.savedPos("stranger"); ok {
		t.Fatal("a player who never played should have no saved position")
	}

	// Reload from disk and confirm the position survives.
	s2 := newInvStore(path)
	x, y, z, yaw, pitch, dim, ok := s2.savedPos("wanderer")
	if !ok {
		t.Fatal("a recorded player's position should reload")
	}
	if x != 123.5 || y != 72 || z != -840.25 || yaw != 90 || pitch != -12 || dim != 0 {
		t.Fatalf("position round-trip lost data: (%v,%v,%v) yaw=%v pitch=%v dim=%v", x, y, z, yaw, pitch, dim)
	}

	// A legacy entry with no position (HasPos false) → ok=false.
	s2.m["legacy"] = &savedInv{}
	if _, _, _, _, _, _, ok := s2.savedPos("legacy"); ok {
		t.Fatal("a legacy entry without a position must fall back to world spawn")
	}
}
