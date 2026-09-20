package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// addedFor reports whether a spawn frame for eid reached this player.
func addedFor(tr *tracked, eid int32) bool {
	for {
		select {
		case pkt := <-tr.p.out:
			if ev, ok := pkt.ev.(attachproto.EntityAdd); ok && ev.EID == eid {
				return true
			}
		default:
			return false
		}
	}
}

// An entity that leaves the viewer's range is removed from their client,
// and spawned again when it comes back — vanilla's tracked-entity model.
// Before this a creature that wandered off simply froze where it stood.
func TestTrackingFollowsTheViewer(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0, 70, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players

	m := h.spawnMob(players, entityCow, 8, 70, 8) // well inside the interest radius
	if m == nil {
		t.Fatal("no cow")
	}
	drainEvents(pl)
	h.syncTracking(players)
	if !pl.tracked[m.eid] || !addedFor(pl, m.eid) {
		t.Fatal("a cow beside the player should be spawned for them")
	}
	// It wanders off: the viewer is told to drop it.
	m.x, m.z = 400, 400
	h.syncTracking(players)
	if pl.tracked[m.eid] {
		t.Fatal("a cow out of range should leave the viewer's set")
	}
	if !goneFor(pl, m.eid) {
		t.Fatal("the viewer was never told to drop the cow that walked away")
	}
	// And back again: spawned afresh, not left to the client's memory.
	m.x, m.z = 8, 8
	h.syncTracking(players)
	if !pl.tracked[m.eid] || !addedFor(pl, m.eid) {
		t.Fatal("a cow that comes back should be spawned again")
	}
	// The same for a dropped item.
	it := h.spawnItemAt(players, 0, itemByName["stick"], 1, 6, 70, 6, 0, 0, 0)
	h.syncTracking(players)
	if !pl.tracked[it.eid] || !addedFor(pl, it.eid) {
		t.Fatal("a dropped item in range should be spawned for the viewer")
	}
	delete(h.items, it.eid) // picked up
	h.syncTracking(players)
	if pl.tracked[it.eid] || !goneFor(pl, it.eid) {
		t.Fatal("an item that stops existing should be dropped from the view")
	}
}

// A viewer only ever hears about what is in range of them: a second player
// far away is told nothing about the first one's neighbours.
func TestTrackingIsPerViewer(t *testing.T) {
	h := newHub(world.New(1))
	near := survPlayer(h)
	far := survPlayer(h)
	far.p.eid = 2
	near.x, near.y, near.z = 0, 70, 0
	far.x, far.y, far.z = 500, 70, 500
	players := map[int32]*tracked{near.p.eid: near, far.p.eid: far}
	h.playersRef = players

	m := h.spawnMob(players, entityCow, 4, 70, 4)
	drainEvents(near)
	drainEvents(far)
	h.syncTracking(players)
	if !near.tracked[m.eid] {
		t.Error("the player beside the cow should track it")
	}
	if far.tracked[m.eid] || addedFor(far, m.eid) {
		t.Error("a player 500 blocks away should hear nothing about it")
	}
}

// A dimension change replaces the whole view: everything the client held
// is removed, and the new dimension's entities arrive with the next pass.
func TestDimensionChangeDropsTheView(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnMob(players, entityCow, 4, 70, 4)
	h.syncTracking(players)
	drainEvents(pl)

	h.dropTracked(pl)
	if len(pl.tracked) != 0 {
		t.Fatal("the view should be empty after a dimension change")
	}
	if !goneFor(pl, m.eid) {
		t.Fatal("the entities the client held should be removed")
	}
}
