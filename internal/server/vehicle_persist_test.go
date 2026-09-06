package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A parked boat survives the store round trip: same type, same spot.
func TestVehiclesPersist(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.SetBlock(40, 62, 40, worldgen.WaterBase)
	if !h.spawnVehicleAt(players, 0, boatEntities["oak_boat"], 40, 62, 40) {
		t.Fatal("boat should spawn on water")
	}
	h.world.SetBlock(41, 63, 40, worldgen.BlockBase("rail"))
	if !h.spawnVehicleAt(players, 0, entityMinecart, 41, 63, 40) {
		t.Fatal("cart should spawn on a rail")
	}
	saved := h.snapshotVehicles()
	if len(saved) != 2 {
		t.Fatalf("snapshot has %d vehicles, want 2", len(saved))
	}
	h2 := newHub(world.New(1))
	h2.restoreVehicles(saved)
	if len(h2.vehicles) != 2 {
		t.Fatalf("restored %d vehicles, want 2", len(h2.vehicles))
	}
	boats, carts := 0, 0
	for _, v := range h2.vehicles {
		if v.isBoat() {
			boats++
			if v.x != 40.5 || v.z != 40.5 {
				t.Errorf("boat restored at (%.1f, %.1f)", v.x, v.z)
			}
		} else {
			carts++
		}
	}
	if boats != 1 || carts != 1 {
		t.Errorf("restored %d boats and %d carts", boats, carts)
	}
}

// A chest boat carries a chest: it opens as a chest window, its cargo
// persists with the vehicle, and breaking the boat spills it.
func TestChestBoatCargo(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 40.5, 63, 42.5)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	h.world.SetBlock(40, 62, 40, worldgen.WaterBase)
	et := boatEntities["oak_chest_boat"]
	if et == 0 {
		t.Skip("no chest boat in this build")
	}
	if !h.spawnVehicleAt(players, 0, et, 40, 62, 40) {
		t.Fatal("chest boat should spawn on water")
	}
	var v *vehicle
	for _, c := range h.vehicles {
		v = c
	}
	if v.chest == nil {
		t.Fatal("a chest boat carries a chest")
	}
	v.chest.slots[3] = invStack{item: itemByName["diamond"], count: 5}
	h.openVehicleChest(players, pl, v)
	if pl.winKind != winChest || pl.viewChest != v.chest {
		t.Fatal("sneak-click should open the boat's chest")
	}
	saved := h.snapshotVehicles()
	h2 := newHub(world.New(1))
	h2.restoreVehicles(saved)
	for _, c := range h2.vehicles {
		if c.chest == nil || c.chest.slots[3].item != itemByName["diamond"] || c.chest.slots[3].count != 5 {
			t.Errorf("cargo lost in the store: %+v", c.chest)
		}
	}
	h.closeWindow(players, pl)
	h.breakVehicle(players, v)
	found := false
	for _, it := range h.items {
		if it.item == itemByName["diamond"] && it.count == 5 {
			found = true
		}
	}
	if !found {
		t.Error("breaking a chest boat should spill its cargo")
	}
}

// A boat placed on Nether lava does not float, but a cart on a Nether rail
// rolls; vehicles carry their dimension and are only shown to players in it.
func TestVehiclesInOtherDimensions(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	if h.worldFor(dimNether) == nil {
		t.Skip("no nether world in this hub")
	}
	h.worldFor(dimNether).SetBlock(10, 40, 10, worldgen.BlockBase("rail"))
	if !h.spawnVehicleAt(players, dimNether, entityMinecart, 10, 40, 10) {
		t.Fatal("a cart should place on a Nether rail")
	}
	for _, v := range h.vehicles {
		if v.dim != dimNether {
			t.Errorf("cart dim %d, want the Nether", v.dim)
		}
	}
	saved := h.snapshotVehicles()
	if len(saved) != 1 || saved[0].Dim != dimNether {
		t.Errorf("the dimension must persist: %+v", saved)
	}
}
