package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// frames empties a player's queue and returns what was in it.
func frames(p *player) []any {
	var out []any
	for {
		select {
		case pk := <-p.out:
			out = append(out, pk.ev)
		default:
			return out
		}
	}
}

// sawAdd / sawRemove / sawMove report whether a frame list spawned, removed or
// moved entity eid.
func sawAdd(fs []any, eid int32) bool {
	for _, f := range fs {
		if a, ok := f.(attachproto.EntityAdd); ok && a.EID == eid {
			return true
		}
	}
	return false
}

func sawRemove(fs []any, eid int32) bool {
	for _, f := range fs {
		if r, ok := f.(attachproto.EntityRemove); ok {
			for _, id := range r.EIDs {
				if id == eid {
					return true
				}
			}
		}
	}
	return false
}

func sawMove(fs []any, eid int32) bool {
	for _, f := range fs {
		if m, ok := f.(attachproto.EntityMove); ok && m.EID == eid {
			return true
		}
	}
	return false
}

// Bug #41: Legion saw EdgeZA standing near her while he was 350 blocks away.
// His body had been spawned for her, his moves stopped reaching her once he
// was out of range, and nothing ever removed it. Vanilla's TrackedEntity
// removes a player's body the moment they leave a viewer's range and spawns
// it again when they return.
func TestPlayerBodyLeavesAndReturnsWithRange(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(100)
	h.rules.LocatorBar = false
	players := map[int32]*tracked{}
	legion := &tracked{p: newPlayer(1, "legion", [16]byte{1}), x: -17, y: 91, z: -72}
	edge := &tracked{p: newPlayer(2, "edge", [16]byte{2}), x: -10, y: 91, z: -70}
	players[1], players[2] = legion, edge

	h.syncTracking(players)
	if fs := frames(legion.p); !sawAdd(fs, 2) {
		t.Fatal("in range: legion should be sent edge's body")
	}
	if fs := frames(edge.p); !sawAdd(fs, 1) {
		t.Fatal("in range: edge should be sent legion's body")
	}

	// Edge teleports away through the /tp entry path (evMove, teleport).
	h.onMove(players, edge, evMove{eid: 2, x: -161, y: 75, z: -402, teleport: true})
	h.syncTracking(players)
	if fs := frames(legion.p); !sawRemove(fs, 2) {
		t.Fatal("edge left legion's range: his body must be removed, not left standing")
	}
	if fs := frames(edge.p); !sawRemove(fs, 1) {
		t.Fatal("…and legion's body removed from edge's view")
	}

	// Out of range, his moves no longer go to her at all.
	h.onMove(players, edge, evMove{eid: 2, x: -161.2, y: 75, z: -402, onGround: true})
	if fs := frames(legion.p); sawMove(fs, 2) {
		t.Fatal("a viewer not holding the body must not be sent its moves")
	}

	// He comes back: spawned again, in full.
	h.onMove(players, edge, evMove{eid: 2, x: -12, y: 91, z: -70, teleport: true})
	h.syncTracking(players)
	if fs := frames(legion.p); !sawAdd(fs, 2) {
		t.Fatal("back in range: edge's body must be spawned for legion again")
	}
}

// A player who leaves is removed for everyone holding their body, and no
// viewer keeps a stale entry that would remove it a second time.
func TestPlayerBodyRemovedOnLeave(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	a := &tracked{p: newPlayer(1, "a", [16]byte{1}), x: 0, y: 80, z: 0}
	b := &tracked{p: newPlayer(2, "b", [16]byte{2}), x: 5, y: 80, z: 5}
	players[1], players[2] = a, b
	h.syncTracking(players)
	frames(a.p)
	h.onLeave(players, b.p)
	if fs := frames(a.p); !sawRemove(fs, 2) {
		t.Fatal("a leaver's body must be removed for its viewers")
	}
	if a.tracked[2] {
		t.Fatal("the leaver must be out of the viewer's tracked set")
	}
}

// ServerPlayer.broadcastToPlayer: a spectator's body is sent only to other
// spectators.
func TestSpectatorBodyOnlyForSpectators(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	spec := &tracked{p: newPlayer(1, "spec", [16]byte{1}), x: 0, y: 80, z: 0, gamemode: gmSpectator}
	surv := &tracked{p: newPlayer(2, "surv", [16]byte{2}), x: 5, y: 80, z: 5}
	spec2 := &tracked{p: newPlayer(3, "spec2", [16]byte{3}), x: 3, y: 80, z: 3, gamemode: gmSpectator}
	players[1], players[2], players[3] = spec, surv, spec2
	h.syncTracking(players)
	if surv.tracked[1] || surv.tracked[3] {
		t.Fatal("a non-spectator must not be sent a spectator's body")
	}
	if !spec2.tracked[1] || !spec.tracked[2] {
		t.Fatal("a spectator sees other spectators and everyone else")
	}
}
