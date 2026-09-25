package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

func drainWaypoints(pl *tracked) (ops []int8) {
	for {
		select {
		case pkt := <-pl.p.out:
			if w, ok := pkt.ev.(attachproto.Waypoint); ok {
				ops = append(ops, w.Op)
			}
		default:
			return
		}
	}
}

// Invisibility, a carved pumpkin or mob head worn, and spectator mode take a
// player off the others' locator bars; taking them off puts the player back
// (isTransmittingWaypoint). A spectator still sees everyone.
func TestLocatorBarHidesTheHidden(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.LocatorBar = true
	a, b := survPlayer(h), survPlayer(h)
	b.p.eid = a.p.eid + 1
	players := map[int32]*tracked{a.p.eid: a, b.p.eid: b}
	h.waypointOnJoin(players, b)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("a should track b on join: %v", ops)
	}
	drainWaypoints(b)

	h.applyEffect(players, b, effInvisibility, 0, 30)
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("an invisible b should leave a's bar: %v", ops)
	}
	h.waypointOnMove(players, b, true)
	if ops := drainWaypoints(a); len(ops) != 0 {
		t.Fatalf("an invisible b moving must not show: %v", ops)
	}
	h.removeEffect(b, effInvisibility)
	b.armor[0] = invStack{item: int32(itemByName["carved_pumpkin"]), count: 1}
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 0 {
		t.Fatalf("b in a carved pumpkin must stay hidden: %v", ops)
	}
	b.armor[0] = invStack{}
	h.waypointTick(players)
	if ops := drainWaypoints(a); len(ops) != 1 || ops[0] != waypointTrack {
		t.Fatalf("b should come back on a's bar: %v", ops)
	}

	a.gamemode = gmSpectator
	drainWaypoints(b)
	h.waypointTick(players)
	if ops := drainWaypoints(b); len(ops) != 1 || ops[0] != waypointUntrack {
		t.Fatalf("a spectator a should leave b's bar: %v", ops)
	}
}
