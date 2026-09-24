package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// The tab-list entry carries the player's own mode, and a /gamemode change
// reaches every client as UPDATE_GAME_MODE (it used to list everyone as
// creative, for good).
func TestTabListCarriesGameMode(t *testing.T) {
	if got := infoAdd(newPlayer(1, "a", [16]byte{}), gmSpectator); got.Gamemode != int32(gmSpectator) {
		t.Fatalf("infoAdd gamemode %d, want spectator", got.Gamemode)
	}
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false
	startHub(t, h)
	a := newPlayer(h.allocEID(), "alice", [16]byte{1})
	b := newPlayer(h.allocEID(), "bob", [16]byte{2})
	h.post(evJoin{p: a, x: 0.5, y: 100, z: 0.5, gamemode: gmSurvival})
	waitJoined(t, h, "alice")
	h.post(evJoin{p: b, x: 0.5, y: 100, z: 0.5, gamemode: gmSurvival})
	waitJoined(t, h, "bob")
	h.post(evSetGamemode{name: "alice", mode: gmSpectator, by: "alice", eid: a.eid})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case pkt := <-b.out:
			if m, ok := pkt.ev.(attachproto.PlayerInfoMode); ok && m.UUID == a.uuid && m.Gamemode == int32(gmSpectator) {
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("bob never heard alice became a spectator")
}
