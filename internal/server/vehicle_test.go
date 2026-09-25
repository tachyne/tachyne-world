package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func vehSetup(t *testing.T) (*hub, *tracked, map[int32]*tracked, int, int, int) {
	h, w, _, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, railMin+1) // a rail to click
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+0.5
	players := map[int32]*tracked{1: pl}
	return h, pl, players, x, y, z
}

func firstVehicle(h *hub) *vehicle {
	for _, v := range h.vehicles {
		return v
	}
	return nil
}

func TestMinecartPlacesOnRailOnly(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	cart := itemByName["minecart"]
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: cart, x: x, y: y, z: z, slot: 0})
	if len(h.vehicles) != 1 {
		t.Fatal("cart should spawn on a rail")
	}
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: cart, x: x + 3, y: y, z: z, slot: 0})
	if len(h.vehicles) != 1 {
		t.Fatal("cart must NOT spawn off-rail")
	}
}

// A boat is client-driven: its rider's move packets carry it, within a
// sanity cap. A minecart is not — the server rolls it and a rider's client
// move is ignored (vanilla: no controlling passenger since 1.21.2).
func TestRideValidateAndSnapBack(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.world.SetBlock(x, y, z, worldgen.WaterBase)
	if !h.spawnVehicleAt(players, 0, boatEntities["oak_boat"], x, y, z) {
		t.Fatal("boat should spawn on water")
	}
	v := firstVehicle(h)
	h.mountVehicle(players, pl, v)
	if v.rider != 1 {
		t.Fatal("interact should mount")
	}
	// Sane move: accepted.
	h.applyVehicleMove(players, pl, evVehicleMove{eid: 1, x: v.x + 1, y: v.y, z: v.z})
	if v.x != float64(x)+1.5 {
		t.Fatalf("sane vehicle move should apply, x=%v", v.x)
	}
	if pl.x != v.x {
		t.Fatal("rider position must follow the vehicle")
	}
	// Teleport hack: rejected, position unchanged.
	before := v.x
	h.applyVehicleMove(players, pl, evVehicleMove{eid: 1, x: v.x + 50, y: v.y, z: v.z})
	if v.x != before {
		t.Fatal("AUTHORITY: oversized vehicle move must be rejected")
	}
	// Dismount stands the rider beside it.
	h.dismount(players, pl)
	if v.rider != 0 || pl.x == v.x {
		t.Fatal("dismount should clear the rider and move them aside")
	}
	// A cart pays no attention to its rider's client.
	h.world.SetBlock(x+2, y, z, railMin+1)
	if !h.spawnVehicleAt(players, 0, entityMinecart, x+2, y, z) {
		t.Fatal("cart should spawn on the rail")
	}
	var cart *vehicle
	for _, c := range h.vehicles {
		if !c.isBoat() {
			cart = c
		}
	}
	pl.x, pl.y, pl.z = cart.x, cart.y, cart.z
	h.mountVehicle(players, pl, cart)
	cx := cart.x
	h.applyVehicleMove(players, pl, evVehicleMove{eid: 1, x: cart.x + 1, y: cart.y, z: cart.z})
	if cart.x != cx {
		t.Fatalf("a minecart is server-driven; the client's move must be ignored, x=%v", cart.x)
	}
}

func TestBreakVehicleDropsItem(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
	h.breakVehicle(players, firstVehicle(h))
	if len(h.vehicles) != 0 {
		t.Fatal("punched vehicle should despawn")
	}
	found := false
	for _, it := range h.items {
		if it.item == itemByName["minecart"] {
			found = true
		}
	}
	if !found {
		t.Fatal("broken cart should drop its item")
	}
}

func TestDetectorRailPressesUnderCart(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.world.SetBlock(x, y, z, railWith(detectorRailMin, shapeEW, false))
	h.world.SetBlock(x+1, y, z, lampOff)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
	h.updateVehicles(players)
	stepTicks(h, players, 4)
	if !railPowered(h.world.At(x, y, z)) || h.world.At(x+1, y, z) != lampOn {
		t.Fatalf("detector should press + light: rail=%d lamp=%d", h.world.At(x, y, z), h.world.At(x+1, y, z))
	}
	h.breakVehicle(players, firstVehicle(h))
	h.updateVehicles(players)
	stepTicks(h, players, 20) // the rail releases 20 ticks after the cart last sat on it …
	h.updateVehicles(players)
	stepTicks(h, players, 8) // … and the lamp goes dark 4 after that
	if railPowered(h.world.At(x, y, z)) || h.world.At(x+1, y, z) != lampOff {
		t.Fatal("detector should release when the cart goes")
	}
}

func TestBoatPlacesOnWater(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.world.SetBlock(x+2, y, z, worldgen.Water)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["oak_boat"], x: x + 2, y: y, z: z, slot: 0})
	if len(h.vehicles) != 1 {
		t.Fatal("boat should spawn on water")
	}
	// By NAME from the canonical registry — the literal that used to sit here
	// (84) was a 1.21.5 ordinal that means "marker" in 1.21.11.
	if v, want := firstVehicle(h), entityByName["oak_boat"]; v.etype != want {
		t.Fatalf("oak boat entity type %d (%s), want %d", v.etype, entityNameOf(v.etype), want)
	}
}

// Boat entity types come from the canonical registry BY NAME. They were once
// hardcoded 1.21.5 ordinals, which the 1.21.11 retarget silently invalidated:
// an oak boat spawned a marker, a spruce boat a sniffer, a mangrove boat a
// llama. Pin every wood so a future canonical bump can't repeat it.
func TestBoatEntityIDsTrackTheRegistry(t *testing.T) {
	for _, wood := range []string{"oak", "spruce", "birch", "jungle", "acacia",
		"dark_oak", "cherry", "mangrove", "pale_oak", "poplar", "bamboo"} {
		// Bamboo floats a raft; every other wood has a boat of its own name.
		item := wood + "_boat"
		if wood == "bamboo" {
			item = "bamboo_raft"
		}
		got, ok := boatEntities[item]
		if !ok {
			t.Errorf("%s has no boat entity", item)
			continue
		}
		if _, ok := vehicleItems[int32(itemByName[item])]; !ok {
			t.Errorf("the %s item places nothing", item)
		}
		want := entityByName[item]
		if got != want {
			t.Errorf("%s spawns entity %d (%s), want %d", item, got, entityNameOf(got), want)
		}
	}
}

// entityNameOf reverses the generated table for readable failures.
func entityNameOf(id int) string {
	for n, v := range entityByName {
		if v == id {
			return n
		}
	}
	return "?"
}

// A boat used while aiming at open water goes onto the water. The client
// reports that as a plain use — a fluid is not a clickable block — which is
// why it needs the look ray rather than a clicked cell.
func TestBoatPlacedByLookingAtWater(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, 0 // level, along +z
	boat := int32(itemByName["oak_boat"])
	pl.inv.slots[0] = invStack{item: boat, count: 1}
	pl.p.held = 0

	for z := 2; z <= 4; z++ { // a pond at eye level
		h.world.SetBlock(0, 181, z, worldgen.WaterBase)
	}
	h.placeVehicleFromLook(players, pl, boat, 0)

	if len(h.vehicles) != 1 {
		t.Fatalf("a boat should be floating there, got %d vehicles", len(h.vehicles))
	}
	if pl.inv.slots[0].count != 0 {
		t.Fatalf("the boat item should be spent, %d left", pl.inv.slots[0].count)
	}
}

// Aiming at nothing places nothing, and keeps the item.
func TestBoatNeedsSomethingToLandOn(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.yaw, pl.pitch = 0, -90 // straight up into empty sky
	boat := int32(itemByName["oak_boat"])
	pl.inv.slots[0] = invStack{item: boat, count: 1}
	pl.p.held = 0

	h.placeVehicleFromLook(players, pl, boat, 0)
	if len(h.vehicles) != 0 {
		t.Fatal("nothing in the ray: no boat should appear")
	}
	if pl.inv.slots[0].count != 1 {
		t.Fatal("the boat item must not be spent")
	}
}

// BoatItem.use: level.noCollision(boat, box) — no boat goes down where its
// box would overlap another boat, a player or a colliding block.
func TestBoatNeedsRoom(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.world.SetBlock(x+2, y, z, worldgen.Water)
	boat := itemByName["oak_boat"]
	place := func() { h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: boat, x: x + 2, y: y, z: z, slot: 0}) }
	place()
	place()
	if len(h.vehicles) != 1 {
		t.Fatalf("a boat went down inside another: %d boats", len(h.vehicles))
	}
	for eid := range h.vehicles {
		delete(h.vehicles, eid)
	}
	other := testTracked()
	other.p.eid = 2
	other.x, other.y, other.z = float64(x+2)+0.5, float64(y), float64(z)+0.5
	players[2] = other
	place()
	if len(h.vehicles) != 0 {
		t.Fatal("a boat went down inside a player")
	}
	delete(players, 2)
	h.world.SetBlock(x+3, y, z, worldgen.Stone)
	place()
	if len(h.vehicles) != 0 {
		t.Fatal("a boat went down overlapping a stone block")
	}
	h.world.SetBlock(x+3, y, z, worldgen.Air)
	place()
	if len(h.vehicles) != 1 {
		t.Fatal("with room the boat goes down")
	}
}
