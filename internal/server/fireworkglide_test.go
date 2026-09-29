package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// A rocket used in a glide names its glider in DATA_ATTACHED_TO_TARGET and
// leaves the boosting to the glider's client: the server never overwrites
// the glider's velocity, which is what used to throw away their momentum
// every tick.
func TestGlideRocketBoostIsTheClients(t *testing.T) {
	h := newTestHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.inv.slots[0] = invStack{item: itemFireworkRocket, count: 2}
	pl.p.held = 0
	pl.onGround = false
	pl.armor[1] = invStack{item: itemElytra, count: 1}
	pl.fallFlying = true
	drainEvs(pl.p)

	h.useItemEvent(players, evUseFirework{eid: pl.p.eid})
	if len(h.rockets) != 1 {
		t.Fatalf("gliding should launch a rocket, have %d", len(h.rockets))
	}
	var r *rocketEntity
	for _, x := range h.rockets {
		r = x
	}
	want := rocketAttachedMeta(r.eid, pl.p.eid)
	sawAttach := false
	for _, ev := range drainEvs(pl.p) {
		if m, ok := ev.(attachproto.EntityMeta); ok && m.EID == r.eid && bytes.Equal(m.Meta, metaEv(want).Meta) {
			sawAttach = true
		}
	}
	if !sawAttach {
		t.Fatal("the rocket's spawn should carry DATA_ATTACHED_TO_TARGET naming the glider")
	}
	// The entry itself: index 9, OPTIONAL_UNSIGNED_INT, the glider's id + 1.
	rd := bytes.NewReader(want)
	protocol.ReadVarInt(rd)
	idx, _ := rd.ReadByte()
	typ, _ := protocol.ReadVarInt(rd)
	val, _ := protocol.ReadVarInt(rd)
	if idx != 9 || typ != metaTypeOptUInt || val != pl.p.eid+1 {
		t.Fatalf("attached entry = index %d type %d value %d, want 9/%d/%d", idx, typ, val, metaTypeOptUInt, pl.p.eid+1)
	}

	pl.x, pl.y, pl.z = 10.5, 190, 3.5
	for i := 0; i < 5; i++ {
		h.updateRockets(players)
	}
	for _, ev := range drainEvs(pl.p) {
		if v, ok := ev.(attachproto.Velocity); ok && v.EID == pl.p.eid {
			t.Fatalf("the server set the glider's velocity: %+v", v)
		}
	}
	if r.x != pl.x || r.y != pl.y || r.z != pl.z {
		t.Fatalf("the rocket should ride with its glider: rocket %v,%v,%v player %v,%v,%v", r.x, r.y, r.z, pl.x, pl.y, pl.z)
	}
}

// A loose rocket launches with vanilla's slight sideways drift, and the 1.15
// accelerator grows it.
func TestLooseRocketDriftGrows(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	var r *rocketEntity
	for i := 0; i < 20; i++ { // triangle(0, …) can land on zero; find a drifting one
		r = h.spawnRocket(players, 0, 0.5, 250, 0.5, 0, invStack{item: itemFireworkRocket, count: 1, flight: 3})
		if r.vx != 0 {
			break
		}
	}
	if r.vx == 0 || r.vx > fireworkLaunchXZ || r.vx < -fireworkLaunchXZ {
		t.Fatalf("launch drift %v outside ±%v", r.vx, fireworkLaunchXZ)
	}
	vx0 := r.vx
	h.updateRockets(players)
	if got := r.vx / vx0; got < 1.149 || got > 1.151 {
		t.Fatalf("sideways speed should grow 1.15× a tick, got %v", got)
	}
}
