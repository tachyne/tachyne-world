package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// AbstractBoat's two seats hold two players: the first aboard steers from
// the front, the second rides behind and cannot steer, and when the front
// one gets out the other moves up and takes the paddles.
func TestTwoPlayersShareABoat(t *testing.T) {
	h, players, v := boatFixture(t)
	a, b := survPlayer(h), survPlayer(h)
	a.p.eid, b.p.eid = 201, 202
	players[a.p.eid], players[b.p.eid] = a, b
	a.x, a.y, a.z = 1.5, 100, 0.5
	b.x, b.y, b.z = 0.5, 100, 1.5
	h.mountVehicle(players, a, v)
	h.mountVehicle(players, b, v)
	if p := v.passengers(); len(p) != 2 || p[0] != a.p.eid || p[1] != b.p.eid || b.ridingEID != v.eid {
		t.Fatalf("both players aboard, the first in front: %v", p)
	}
	if h.boatController(v) != a.p.eid {
		t.Fatal("the front player steers")
	}
	h.applyVehicleMove(players, b, evVehicleMove{eid: b.p.eid, x: v.x + 1, y: v.y, z: v.z})
	if v.x != 0.5 {
		t.Fatal("the back-seat player cannot steer the boat")
	}
	c := survPlayer(h)
	c.p.eid = 203
	players[c.p.eid] = c
	c.x, c.y, c.z = 0.5, 100, 0.5
	h.mountVehicle(players, c, v)
	if c.ridingEID != 0 {
		t.Fatal("a third player finds no seat")
	}
	h.dismount(players, a)
	if v.rider != b.p.eid || v.rider2 != 0 || h.boatController(v) != b.p.eid {
		t.Fatalf("the back seat moves up: rider %d rider2 %d", v.rider, v.rider2)
	}
	h.applyVehicleMove(players, b, evVehicleMove{eid: b.p.eid, x: v.x + 1, y: v.y, z: v.z})
	if v.x != 1.5 || a.ridingEID != 0 {
		t.Fatalf("now in front, it steers: x %v", v.x)
	}
}

// An unsteered boat is not still: in a current it drifts downstream, and
// dropped in the air it falls to the ground; in still water it stays put.
func TestUnmannedBoatDrifts(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	// A channel along +x at y=180: a source at x=0 flowing east.
	for x := -2; x <= 12; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
		w.SetBlock(x, 180, -2, worldgen.Stone)
		w.SetBlock(x, 180, 2, worldgen.Stone)
	}
	for x := 0; x <= 7; x++ {
		for z := -1; z <= 1; z++ {
			w.SetBlock(x, 180, z, worldgen.WaterBase+uint32(x)) // level x: flowing east
		}
	}
	boat := h.addVehicle(players, 0, entityByName["oak_boat"], 1.5, 180.2, 0.5, 0)
	for i := 0; i < 100; i++ {
		h.updateVehicles(players)
	}
	if boat.x < 2.5 {
		t.Fatalf("an empty boat in a current drifts with it: x %.3f", boat.x)
	}
	// Dropped in the air over the stone at x=10, it falls to the ground.
	fall := h.addVehicle(players, 0, entityByName["oak_boat"], 10.5, 182.5, 0.5, 0)
	for i := 0; i < 60; i++ {
		h.updateVehicles(players)
	}
	if fall.y != 180 {
		t.Fatalf("a boat in the air falls onto the ground: y %.3f", fall.y)
	}
	// Still water: a pool.
	for x := 20; x <= 24; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.WaterBase)
			w.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	still := h.addVehicle(players, 0, entityByName["oak_boat"], 22.5, 180.5, 0.5, 0)
	for i := 0; i < 200; i++ {
		h.updateVehicles(players)
	}
	if still.x != 22.5 || still.z != 0.5 || still.y < 180 || still.y > 181 {
		t.Fatalf("a boat in still water floats where it is: (%.3f, %.3f, %.3f)", still.x, still.y, still.z)
	}
}

// Camel.getMaxPassengers is 2: a second player climbs on behind the first,
// only the front one steers, and the back one moves up when it gets off.
func TestCamelSeatsTwoPlayers(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	camel := h.spawnSpecies(players, entityCamel, 0, 0.5, 180, 0.5)
	camel.tamed, camel.saddled = true, true
	a, b, c := survPlayer(h), survPlayer(h), survPlayer(h)
	for i, p := range []*tracked{a, b, c} {
		p.p.eid = int32(301 + i)
		p.x, p.y, p.z = 1.5, 180, 0.5
		players[p.p.eid] = p
	}
	h.tryMount(players, a, camel)
	h.tryMount(players, b, camel)
	h.tryMount(players, c, camel)
	if p := camel.playerPassengers(); len(p) != 2 || p[0] != a.p.eid || p[1] != b.p.eid || c.ridingEID != 0 {
		t.Fatalf("two riders, the first in front, no third: %v (third on %d)", p, c.ridingEID)
	}
	if h.applyMountMove(players, b, evVehicleMove{eid: b.p.eid, x: camel.x + 1, y: camel.y, z: camel.z}) || camel.x != 0.5 {
		t.Fatal("the back-seat rider cannot steer the camel")
	}
	h.applyMountMove(players, a, evVehicleMove{eid: a.p.eid, x: camel.x + 1, y: camel.y, z: camel.z})
	if camel.x != 1.5 || b.x != 1.5 {
		t.Fatalf("the front rider steers and the back one rides along: camel %v back %v", camel.x, b.x)
	}
	h.dismountMob(players, a)
	if camel.rider != b.p.eid || camel.rider2 != 0 {
		t.Fatalf("the back rider moves up: rider %d rider2 %d", camel.rider, camel.rider2)
	}
}
