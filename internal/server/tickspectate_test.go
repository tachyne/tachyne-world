package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

func tickCmdHub(t *testing.T) (*hub, *Server, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.p.eid = 500 // clear of the mob ids the hub hands out
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	s := &Server{hub: h, Ops: map[string]bool{pl.p.name: true}}
	return h, s, players, pl
}

// /tick freeze stops the world's clock; step lets a frozen game run a few
// ticks; the clients are told each state.
func TestTickFreezeAndStep(t *testing.T) {
	h, s, players, pl := tickCmdHub(t)
	drainOut(pl.p)
	s.handleCommand(pl.p, "tick freeze")
	runHubCmds(h, players)
	if !h.ticks.frozen {
		t.Fatal("/tick freeze should freeze the game")
	}
	var frozenSeen bool
	for _, ev := range drainEvs(pl.p) {
		if st, ok := ev.(attachproto.TickingState); ok && st.Frozen && st.Rate == 20 {
			frozenSeen = true
		}
	}
	if !frozenSeen {
		t.Fatal("clients must be told the game is frozen")
	}
	if h.tickGate(players) {
		t.Fatal("a frozen game must not tick")
	}
	s.handleCommand(pl.p, "tick step 2")
	runHubCmds(h, players)
	if !h.tickGate(players) || !h.tickGate(players) || h.tickGate(players) {
		t.Fatal("step 2 runs exactly two ticks")
	}
	s.handleCommand(pl.p, "tick rate 40")
	s.handleCommand(pl.p, "tick unfreeze")
	runHubCmds(h, players)
	if h.ticks.frozen || h.ticks.rate != 40 || h.ticks.interval().Milliseconds() != 25 {
		t.Fatalf("after unfreeze at rate 40: frozen=%v rate=%v interval=%v", h.ticks.frozen, h.ticks.rate, h.ticks.interval())
	}
}

// A spectator's click on a mob puts the camera there; shift brings it home.
func TestSpectatorCamera(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	pl.gamemode = gmSpectator
	m := h.spawnMobIn(players, entityCow, 0, pl.x+3, pl.y, pl.z)
	drainOut(pl.p)
	h.onAttack(players, evAttack{attacker: pl.p.eid, target: m.eid})
	if pl.camera != m.eid || m.health < mobHealth(entityCow) {
		t.Fatalf("camera %d (want %d), cow health %v: a spectator's click only moves the camera", pl.camera, m.eid, m.health)
	}
	var cam []int32
	for _, ev := range drainEvs(pl.p) {
		if c, ok := ev.(attachproto.Camera); ok {
			cam = append(cam, c.EID)
		}
	}
	if len(cam) != 1 || cam[0] != m.eid {
		t.Fatalf("set_camera frames %v, want [%d]", cam, m.eid)
	}
	h.setCamera(pl, 0)
	if pl.camera != 0 {
		t.Fatal("the camera should come home")
	}
}

// /transfer sends the named player a transfer to the host and port.
func TestTransferCommand(t *testing.T) {
	h, s, players, pl := tickCmdHub(t)
	drainOut(pl.p)
	s.handleCommand(pl.p, "transfer play.example.org 25570")
	runHubCmds(h, players)
	for _, ev := range drainEvs(pl.p) {
		if tr, ok := ev.(attachproto.Transfer); ok {
			if tr.Host != "play.example.org" || tr.Port != 25570 {
				t.Fatalf("transfer %+v", tr)
			}
			return
		}
	}
	t.Fatal("no transfer sent")
}

// /posteffect add, then list, then clear — each change sent to the player.
func TestPostEffectCommand(t *testing.T) {
	h, s, players, pl := tickCmdHub(t)
	h.postFX = newPostEffectStore("")
	drainOut(pl.p)
	s.handleCommand(pl.p, "posteffect add @s creeper")
	runHubCmds(h, players)
	var got []string
	for _, ev := range drainEvs(pl.p) {
		if pe, ok := ev.(attachproto.PostEffects); ok {
			got = pe.Effects
		}
	}
	if len(got) != 1 || got[0] != "minecraft:creeper" {
		t.Fatalf("post effects %v, want [minecraft:creeper]", got)
	}
	s.handleCommand(pl.p, "posteffect clear @s")
	runHubCmds(h, players)
	if len(h.postFX.get(pl.p.key())) != 0 {
		t.Fatal("clear should empty the list")
	}
}

// ZombieNautilus.finalizeSpawn: a warm-ocean zombie nautilus is the coral one,
// and its viewers are told.
func TestZombieNautilusWarmVariant(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	m := &mob{etype: entityZombieNautilus, eid: 9}
	m.variant = zombieNautilusWarm
	drainOut(pl.p)
	h.showMobTo(pl, m)
	for _, ev := range drainEvs(pl.p) {
		if nv, ok := ev.(attachproto.NautilusVariant); ok && nv.EID == 9 && nv.Variant == 1 {
			return
		}
	}
	_ = players
	t.Fatal("no variant frame for a warm zombie nautilus")
}
