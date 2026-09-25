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
	h := newHub(world.New(1))
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
