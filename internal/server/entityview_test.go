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

// An entity's own frames follow its tracked set, not a radius: a viewer
// holding it hears about it, one who does not hear nothing. Kinds the
// tracker does not own yet still ride the positional broadcast.
func TestUpdatesFollowTheTrackedSet(t *testing.T) {
	h := newHub(world.New(1))
	near := survPlayer(h)
	far := survPlayer(h)
	far.p.eid = 2
	near.x, near.y, near.z = 0, 70, 0
	far.x, far.y, far.z = 300, 70, 300
	players := map[int32]*tracked{near.p.eid: near, far.p.eid: far}
	h.playersRef = players

	m := h.spawnMob(players, entityCow, 5, 70, 5)
	h.syncTracking(players)
	drainEvents(near)
	drainEvents(far)

	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	if !sawSwing(near, m.eid) {
		t.Error("the viewer holding the cow should get its frames")
	}
	if sawSwing(far, m.eid) {
		t.Error("a player who is not holding it should get nothing")
	}
	// An id the tracker does not own falls back to the positional broadcast,
	// so the kinds not yet moved over keep working.
	h.toTracking(players, 999999, 0, 1, 1, swingArm(999999))
	if !sawSwing(near, 999999) {
		t.Error("an untracked kind should still reach the players nearby")
	}
}

func sawSwing(tr *tracked, eid int32) bool {
	for {
		select {
		case pkt := <-tr.p.out:
			if ev, ok := pkt.ev.(attachproto.Swing); ok && ev.EID == eid {
				return true
			}
		default:
			return false
		}
	}
}

// Ranges come from the entity type, clamped by the viewer's render
// distance, and the check ignores height as vanilla's does.
func TestTrackRangesAreVanillas(t *testing.T) {
	cases := map[int]float64{
		entityCow: 10 * 16, entityZombie: 8 * 16, entityItem: 6 * 16,
		entityXPOrb: 6 * 16, entityArrow: 4 * 16, entityBat: 5 * 16,
	}
	for et, want := range cases {
		if got := trackRange(et); got != want {
			t.Errorf("%s track range %v, want %v", entityNameByID[et], got, want)
		}
	}
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0, 70, 0
	// The viewer's own render distance clamps it: six chunks here, so a cow
	// at seven is out even though its type reaches ten.
	if inRangeOf(pl, 0, 7*16, 0, entityCow) {
		t.Error("the viewer's render distance should clamp the type's range")
	}
	if !inRangeOf(pl, 0, 5*16, 0, entityCow) {
		t.Error("a cow five chunks away is inside both")
	}
	// Height is ignored, as vanilla's horizontal check is.
	pl.y = 300
	if !inRangeOf(pl, 0, 5*16, 0, entityCow) {
		t.Error("the check is horizontal: height must not matter")
	}
}
