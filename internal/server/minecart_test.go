package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const cartY = 200 // build high, clear of terrain

// cartTrack lays a straight east-west track on stone from x0 to x1 at z=10,
// out of one rail base state, and clears the air above it.
func cartTrack(h *hub, x0, x1 int, base uint32, powered bool) {
	for x := x0 - 2; x <= x1+2; x++ {
		h.world.SetBlock(x, cartY-1, 10, worldgen.BlockBase("stone"))
		for y := cartY; y <= cartY+3; y++ {
			h.world.SetBlock(x, y, 10, worldgen.Air)
		}
		h.world.SetBlock(x, cartY-1, 9, worldgen.BlockBase("stone"))
		h.world.SetBlock(x, cartY-1, 11, worldgen.BlockBase("stone"))
	}
	for x := x0; x <= x1; x++ {
		h.world.SetBlock(x, cartY, 10, railWith(base, shapeEW, powered))
	}
}

func cartOnTrack(t *testing.T, h *hub, players map[int32]*tracked, x int) *vehicle {
	t.Helper()
	if !h.spawnVehicleAt(players, 0, entityMinecart, x, cartY, 10) {
		t.Fatal("a cart should place on the rail")
	}
	for _, v := range h.vehicles {
		return v
	}
	return nil
}

func cartHub() (*hub, map[int32]*tracked) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	return h, players
}

// An empty cart given a shove rolls along the rail and bleeds 4% of its
// speed a tick until it stops; it never leaves the rail line.
func TestCartRollsAndSlows(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 60, railMin, false)
	v := cartOnTrack(t, h, players, 5)
	v.vx = 0.3
	for i := 0; i < 20; i++ {
		h.tickMinecart(players, v)
	}
	if v.x < 9 || v.z != 10.5 {
		t.Fatalf("after 20 ticks at x=%.2f z=%.2f, want well east on the rail line", v.x, v.z)
	}
	if math.Abs(v.y-(cartY+0.0625)) > 1e-9 {
		t.Errorf("a cart on a flat rail sits 1/16 above it, got y=%.4f", v.y)
	}
	for i := 0; i < 300; i++ {
		h.tickMinecart(players, v)
	}
	if math.Hypot(v.vx, v.vz) > 1e-6 {
		t.Errorf("an empty cart should coast to a stop, speed=%.6f", math.Hypot(v.vx, v.vz))
	}
	if v.x > 60 {
		t.Errorf("rolled off the far end: x=%.2f", v.x)
	}
}

// Live powered rails push a moving cart to the 0.4 block/tick cap;
// dead ones brake it to a standstill.
func TestPoweredRailBoostsAndBrakes(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 200, poweredRailMin, true)
	v := cartOnTrack(t, h, players, 5)
	v.vx = 0.05
	for i := 0; i < 30; i++ {
		h.tickMinecart(players, v)
	}
	before := v.x
	h.tickMinecart(players, v)
	if step := v.x - before; math.Abs(step-cartTopSpeed) > 1e-6 {
		t.Errorf("a boosted cart moves at the cap: step %.4f, want %.4f", step, cartTopSpeed)
	}
	// Cut the power ahead of it.
	for x := floorInt(v.x) + 1; x <= 200; x++ {
		h.world.SetBlock(x, cartY, 10, railWith(poweredRailMin, shapeEW, false))
	}
	for i := 0; i < 40; i++ {
		h.tickMinecart(players, v)
	}
	if math.Hypot(v.vx, v.vz) != 0 {
		t.Errorf("dead powered rails should stop the cart, speed=%.4f", math.Hypot(v.vx, v.vz))
	}
}

// A resting cart on a live powered rail with a solid block at its west end
// sets off east: the classic station.
func TestPoweredRailStationStart(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 5, 60, poweredRailMin, true)
	h.world.SetBlock(4, cartY, 10, worldgen.BlockBase("stone"))
	v := cartOnTrack(t, h, players, 5)
	h.tickMinecart(players, v)
	if v.vx != cartStationPush {
		t.Fatalf("station push: vx=%.4f, want %.4f", v.vx, cartStationPush)
	}
	for i := 0; i < 40; i++ {
		h.tickMinecart(players, v)
	}
	if v.x < 8 {
		t.Errorf("the cart should have left the station eastward, x=%.2f", v.x)
	}
}

// A cart with nothing under it falls and lands; on the ground off the rails
// it skids to a halt.
func TestCartFallsAndLands(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 10, railMin, false)
	v := cartOnTrack(t, h, players, 5)
	v.x, v.y, v.z = 5.5, cartY+6, 10.5 // lifted into the air above the track
	h.world.SetBlock(5, cartY, 10, worldgen.Air)
	v.vx = 0.3
	for i := 0; i < 60; i++ {
		h.tickMinecart(players, v)
	}
	if !v.onGround || v.vy != 0 {
		t.Errorf("should have landed: y=%.3f onGround=%v vy=%.3f", v.y, v.onGround, v.vy)
	}
	if v.y < cartY-0.001 || v.y > cartY+1.1 {
		t.Errorf("landed at an odd height y=%.3f", v.y)
	}
	if math.Hypot(v.vx, v.vz) > 1e-6 {
		t.Errorf("an off-rail cart on the ground should stop, speed=%.4f", math.Hypot(v.vx, v.vz))
	}
}

// A rider's forward key nudges a resting cart in the direction they face
// (yaw -90 = east); the server, not the client, moves the cart.
func TestRiderNudgesCart(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 60, railMin, false)
	v := cartOnTrack(t, h, players, 5)
	r := testTracked()
	players[r.p.eid] = r
	r.x, r.y, r.z = v.x, v.y, v.z
	h.mountVehicle(players, r, v)
	if v.rider != r.p.eid || r.ridingEID != v.eid {
		t.Fatal("mount failed")
	}
	r.yaw = -90
	r.inForward = 1
	for i := 0; i < 60; i++ {
		h.tickMinecart(players, v)
	}
	if v.x < 6 {
		t.Errorf("the rider should have got the cart rolling east, x=%.2f vx=%.4f", v.x, v.vx)
	}
	if r.x != v.x || r.z != v.z {
		t.Errorf("the rider rides along: rider (%.2f,%.2f) cart (%.2f,%.2f)", r.x, r.z, v.x, v.z)
	}
	// The rider's client only reports its camera: a stale position in its
	// move packet must not drag it off the cart.
	h.onMove(players, r, evMove{eid: r.p.eid, x: 1, y: 1, z: 1, yaw: 45})
	if r.x != v.x || r.yaw != 45 {
		t.Errorf("a passenger's move packet is camera-only: rider x=%.2f yaw=%.0f", r.x, r.yaw)
	}
}

// A live activator rail throws the rider off.
func TestActivatorRailEjectsRider(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, activatorMin, true)
	v := cartOnTrack(t, h, players, 5)
	r := testTracked()
	players[r.p.eid] = r
	r.x, r.y, r.z = v.x, v.y, v.z
	h.mountVehicle(players, r, v)
	h.tickMinecart(players, v)
	if v.rider != 0 || r.ridingEID != 0 {
		t.Errorf("activator rail should eject: rider=%d ridingEID=%d", v.rider, r.ridingEID)
	}
}

// On a sloped rail a resting cart rolls downhill.
func TestCartSlidesDownSlope(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 4, railMin, false)
	h.world.SetBlock(5, cartY-1, 10, worldgen.BlockBase("stone"))
	h.world.SetBlock(5, cartY, 10, railWith(railMin, shapeAscE, false)) // rises toward x=6
	h.world.SetBlock(6, cartY, 10, worldgen.BlockBase("stone"))
	h.world.SetBlock(6, cartY+1, 10, railWith(railMin, shapeEW, false))
	v := cartOnTrack(t, h, players, 5)
	for i := 0; i < 40; i++ {
		h.tickMinecart(players, v)
	}
	if v.x >= 5 {
		t.Errorf("should have rolled west down the slope, x=%.2f vx=%.4f", v.x, v.vx)
	}
}

// A rolling empty cart scoops up a mob in its path and carries it; breaking
// the cart sets the mob down.
func TestCartScoopsUpMob(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 60, railMin, false)
	v := cartOnTrack(t, h, players, 5)
	cow := h.spawnMob(players, entityCow, 10.5, cartY, 10.5)
	if cow == nil {
		t.Fatal("no cow")
	}
	v.vx = 0.3
	for i := 0; i < 40; i++ {
		h.tickMinecart(players, v)
	}
	if v.mobRider != cow.eid || cow.cart != v.eid {
		t.Fatalf("the cart should have picked the cow up: mobRider=%d cow.cart=%d x=%.2f", v.mobRider, cow.cart, v.x)
	}
	r := testTracked()
	players[r.p.eid] = r
	r.x, r.y, r.z = v.x, v.y, v.z
	h.mountVehicle(players, r, v)
	if v.rider != 0 {
		t.Error("a cart with a mob aboard has no seat for a player")
	}
	h.breakVehicle(players, v)
	if cow.cart != 0 {
		t.Error("breaking the cart frees the cow")
	}
}
