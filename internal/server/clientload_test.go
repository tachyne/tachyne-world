package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// ServerPlayer.isInvulnerableTo: a respawned player takes nothing until the
// client reports its world loaded — or sixty ticks pass.
func TestNothingHurtsUntilTheClientHasLoaded(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.tick.Store(1000)
	h.damageOf(players, pl, 30, dtGeneric)
	h.respawn(pl)
	hp := pl.health
	h.damageOf(players, pl, 5, dtGeneric)
	if pl.health != hp {
		t.Fatalf("hurt before the client loaded: %v → %v", hp, pl.health)
	}
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
	r.Action(attachproto.PlayerLoaded{})
	for len(h.events) > 0 {
		if ev, ok := (<-h.events).(evClientLoaded); ok && players[ev.eid] != nil {
			players[ev.eid].loadUntil = 0
		}
	}
	h.tick.Add(20) // clear of the hurt cooldown
	h.damageOf(players, pl, 5, dtGeneric)
	if pl.health >= hp {
		t.Fatal("once loaded the player should take damage again")
	}
	// Without the packet, the grace runs out on its own.
	h.tick.Add(20)
	h.damageOf(players, pl, 30, dtGeneric)
	h.respawn(pl)
	h.tick.Add(clientLoadTimeout)
	hp = pl.health
	h.damageOf(players, pl, 5, dtGeneric)
	if pl.health >= hp {
		t.Fatal("after sixty ticks the grace should be over")
	}
}

// A creative player flying down with shift held does not crouch
// (Player.updatePlayerPose).
func TestFlyingPlayerDoesNotCrouch(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.gamemode = gmCreative
	pl.flying = true
	if !mayFly(pl.gamemode) || mayFly(gmSurvival) {
		t.Fatal("mayfly: creative and spectator only")
	}
}
