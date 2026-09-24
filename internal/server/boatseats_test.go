package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func boatFixture(t *testing.T) (*hub, map[int32]*tracked, *vehicle) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	v := &vehicle{eid: h.allocEID(), etype: entityByName["oak_boat"], x: 0.5, y: 100, z: 0.5}
	h.vehicles[v.eid] = v
	return h, players, v
}

// An empty boat takes aboard a villager it touches; a horse is too wide.
func TestBoatPicksUpAMob(t *testing.T) {
	h, players, v := boatFixture(t)
	horse := h.spawnMob(players, entityHorse, 0.9, 100, 0.5)
	h.boatPickup(players, v)
	if v.mobRider != 0 {
		t.Fatal("a horse boarded a boat")
	}
	h.despawnMob(players, horse)
	vil := h.spawnMob(players, entityVillager, 0.9, 100, 0.5)
	h.boatPickup(players, v)
	if v.mobRider != vil.eid || vil.cart != v.eid || !v.mobFirst {
		t.Fatalf("the villager did not board: rider %d cart %d", v.mobRider, vil.cart)
	}
}

// A player takes the second seat beside a goat, and that earns the goat-boat
// criterion; a player steering an empty boat picks nothing up.
func TestPlayerRidesBesideAGoat(t *testing.T) {
	h, players, v := boatFixture(t)
	goat := h.spawnMob(players, entityGoat, 0.9, 100, 0.5)
	h.boatPickup(players, v)
	if v.mobRider != goat.eid {
		t.Fatal("the goat did not board")
	}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = 1.5, 100, 0.5
	h.mountVehicle(players, pl, v)
	if v.rider != pl.p.eid || len(v.passengers()) != 2 || v.passengers()[0] != goat.eid {
		t.Fatalf("seats %v, want the goat in front and the player behind", v.passengers())
	}
	if !(advMatch{vehicle: "oak_boat", passenger: "goat"}).criterion(critOf(t, "minecraft:husbandry/ride_a_boat_with_a_goat", "ride_a_boat_with_a_goat")) {
		t.Error("the goat-boat criterion does not match")
	}

	h2, players2, v2 := boatFixture(t)
	driver := survPlayer(h2)
	players2[driver.p.eid] = driver
	driver.x, driver.y, driver.z = 1.5, 100, 0.5
	h2.mountVehicle(players2, driver, v2)
	h2.spawnMob(players2, entityVillager, 0.9, 100, 0.5)
	h2.boatPickup(players2, v2)
	if v2.mobRider != 0 {
		t.Error("a boat a player is steering picked up a villager")
	}
}
