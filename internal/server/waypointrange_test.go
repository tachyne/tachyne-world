package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The waypoint range attributes: a crouching player drops off the locator
// bar (the crouch modifier zeroes WAYPOINT_TRANSMIT_RANGE), and a receiver
// sees a transmitter only nearer than the lesser of the two ranges.
func TestWaypointRanges(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.LocatorBar = true
	a, b := survPlayer(h), survPlayer(h)
	b.p.eid = a.p.eid + 1
	b.x = a.x + 10
	players := map[int32]*tracked{a.p.eid: a, b.p.eid: b}
	h.waypointOnJoin(players, b)
	drainWaypoints(a)
	drainWaypoints(b)

	b.sneaking = true
	b.updatePlayerAttributes()
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("a crouching b should leave a's bar: %v", ops)
	}
	b.sneaking = false
	b.updatePlayerAttributes()
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("b standing up should come back: %v", ops)
	}

	a.playerAttrs().SetBase(attr.WaypointReceiveRange, 5) // b is 10 away
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("b beyond a's receive range should leave a's bar: %v", ops)
	}
	a.playerAttrs().SetBase(attr.WaypointReceiveRange, 6e7)
	b.playerAttrs().SetBase(attr.WaypointTransmitRange, 9)
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 0 {
		t.Fatalf("b beyond its own transmit range must stay off: %v", ops)
	}
	b.playerAttrs().SetBase(attr.WaypointTransmitRange, 11)
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("b within both ranges should show: %v", ops)
	}
}

// A mob transmits once something gives it a WAYPOINT_TRANSMIT_RANGE (its
// default is zero): a receiver in range tracks it, re-sent when it moves a
// block, and untracks it out of range or when it dies.
func TestMobWaypointTransmits(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.LocatorBar = true
	a := survPlayer(h)
	a.p.eid = 1 << 30 // clear of the test hub's mob eids (a live player mints in its own lane)
	players := map[int32]*tracked{a.p.eid: a}
	m := h.spawnMob(players, entityCow, a.x+10, a.y, a.z)
	drainWaypoints(a)

	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 0 {
		t.Fatalf("a mob with no transmit range must not show: %v", ops)
	}
	m.mobAttrs().SetBase(attr.WaypointTransmitRange, 20)
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("a mob given a range should show: %v", ops)
	}
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 0 {
		t.Fatalf("a still mob is not re-sent: %v", ops)
	}
	m.x += 2
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("a moved mob is re-tracked: %v", ops)
	}
	m.mobAttrs().SetBase(attr.WaypointTransmitRange, 5) // a is 12 away
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("a mob out of its range leaves the bar: %v", ops)
	}
	m.mobAttrs().SetBase(attr.WaypointTransmitRange, 20)
	h.waypointTick(players)
	drainWaypoints(a)
	delete(h.mobs, m.eid)
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("a mob gone leaves the bar: %v", ops)
	}
}
