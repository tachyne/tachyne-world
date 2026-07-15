package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// Test-local family ids: waitFor names event families by their old canonical
// packet id (the wire consts were deleted with the raw path).
const (
	evBlockSetID            = 0x08
	playClientSpawnEntity   = 0x01
	playClientEntityMoveRot = 0x2f
	playClientPlayerInfo    = 0x3f
	playClientPlayerRemove  = 0x3e
	playClientEntityDestroy = 0x46
	playClientEntityHead    = 0x4c
)

// startHub runs a hub's tick loop for the duration of a test and stops it at
// cleanup. Leaked run() goroutines used to pile up across the suite and starve
// later tests' hubs past onHub's deadline — a load-timing flake (TestTripwire
// et al.). Every test that starts the hub should go through here.
func startHub(t *testing.T, h *hub) {
	t.Helper()
	go h.run()
	t.Cleanup(func() { close(h.stop) })
}

// waitFor drains a player's outbound queue until it sees packet id, or fails.
// Domain events match under their family's canonical packet id (an EntityMove
// counts as playClientEntityMoveRot whether a renderer would emit it relative
// or as an absolute resync).
func waitFor(t *testing.T, p *player, id int32, what string) {
	t.Helper()
	// Generous deadline: these tests leak their hub goroutines (run() has no
	// stop), so under -race with -count>1 the accumulated tick load made a
	// 2-second deadline flaky long before it means anything is wrong.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case pkt := <-p.out:
			var got int32
			switch pkt.ev.(type) {
			case attachproto.PlayerInfo:
				got = playClientPlayerInfo
			case attachproto.PlayerGone:
				got = playClientPlayerRemove
			case attachproto.EntityAdd:
				got = playClientSpawnEntity
			case attachproto.EntityMove:
				got = playClientEntityMoveRot
			case attachproto.EntityHead:
				got = playClientEntityHead
			case attachproto.EntityRemove:
				got = playClientEntityDestroy
			case attachproto.BlockSet:
				got = evBlockSetID
			}
			if got == id {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s (packet 0x%02x)", what, id)
		}
	}
}

// TestHubMultiplayer drives the hub directly: two players join, one moves, one
// leaves, and we assert the other player receives the right entity packets.
func TestHubMultiplayer(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	// Player-relay assertions only: without this, natural spawning fills the
	// seed-1 ocean with mobs whose movement events flood the undrained test
	// queues until trySendEv drops the packets this test waits for.
	h.rules.DoMobSpawning = false
	startHub(t, h)

	p1 := newPlayer(h.allocEID(), "alice", [16]byte{1})
	p2 := newPlayer(h.allocEID(), "bob", [16]byte{2})

	sy := w.SurfaceY(0, 0) // on the terrain so noclip/float checks stay quiet
	h.post(evJoin{p: p1, x: 0.5, y: sy, z: 0.5})
	// When bob joins, alice must be told about him (info + spawn), and bob about alice.
	h.post(evJoin{p: p2, x: 10.5, y: sy, z: 10.5})

	waitFor(t, p1, playClientPlayerInfo, "alice learns bob in tab list")
	waitFor(t, p1, playClientSpawnEntity, "alice sees bob's entity")
	waitFor(t, p2, playClientSpawnEntity, "bob sees alice's entity")

	// Alice makes a LEGAL move: small (the survival speed budget rejects and
	// never relays impossible moves) and after a few ticks so the budget bank
	// isn't empty. This test used to "pass" on mob-wander packet noise, which
	// masked that its 7-block instant move was being rejected all along.
	time.Sleep(300 * time.Millisecond)
	h.post(evMove{eid: p1.eid, x: 0.8, y: sy, z: 0.8, yaw: 90})
	waitFor(t, p2, playClientEntityMoveRot, "bob sees alice move")

	// Alice leaves; bob should get a despawn.
	h.post(evLeave{p: p1})
	waitFor(t, p2, playClientEntityDestroy, "bob sees alice despawn")
}

// TestHubBlockBroadcast: an edit by one player reaches a nearby player but not
// the editor (who already predicted it).
func TestHubBlockBroadcast(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false // see TestHubMultiplayer: keep mob noise out
	startHub(t, h)

	editor := newPlayer(h.allocEID(), "editor", [16]byte{1})
	viewer := newPlayer(h.allocEID(), "viewer", [16]byte{2})
	h.post(evJoin{p: editor, x: 0, y: 64, z: 0})
	h.post(evJoin{p: viewer, x: 0, y: 64, z: 0})
	// Drain the join packets so we don't confuse them with the block update.
	waitFor(t, viewer, playClientSpawnEntity, "viewer sees editor")

	h.post(evBlock{x: 1, y: 64, z: 1, state: 1, by: editor.eid})
	waitFor(t, viewer, evBlockSetID, "viewer sees the edit")

	// The editor must NOT receive a broadcast for its own edit.
	select {
	case pkt := <-editor.out:
		if _, isBlock := pkt.ev.(attachproto.BlockSet); isBlock {
			t.Fatal("editor received a broadcast of its own edit")
		}
	case <-time.After(200 * time.Millisecond):
		// good: nothing echoed back
	}
}
