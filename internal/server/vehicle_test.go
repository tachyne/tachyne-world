package server

import (
	"testing"

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

func TestRideValidateAndSnapBack(t *testing.T) {
	h, pl, players, x, y, z := vehSetup(t)
	h.placeVehicle(players, pl, evPlaceVehicle{eid: 1, item: itemByName["minecart"], x: x, y: y, z: z, slot: 0})
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
	stepTicks(h, players, 20)
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
		"dark_oak", "cherry", "mangrove", "pale_oak", "bamboo"} {
		item := wood + "_boat"
		got, ok := boatEntities[item]
		if !ok {
			t.Errorf("%s has no boat entity", item)
			continue
		}
		// Bamboo floats a raft; every other wood has a boat of its own name.
		want := entityByName[wood+"_boat"]
		if wood == "bamboo" {
			want = entityByName["bamboo_raft"]
		}
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
