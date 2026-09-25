package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// runHubCmds runs the closures a remote action queued for the hub.
func runHubCmds(h *hub, players map[int32]*tracked) {
	for len(h.events) > 0 {
		if ev, ok := (<-h.events).(evHubCmd); ok {
			ev.fn(players)
		}
	}
}

// The gateway's keep-alive latency reaches the tab list: in the join entry
// and in the 600-tick UPDATE_LATENCY for everyone.
func TestLatencyReachesTabList(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
	r.Action(attachproto.Latency{MS: 87})
	if got := infoAdd(pl.p, 0).Latency; got != 87 {
		t.Fatalf("join entry latency %d, want 87", got)
	}
	drainOut(pl.p)
	h.broadcastLatency(players)
	found := false
	for _, ev := range drainEvs(pl.p) {
		if e, ok := ev.(attachproto.PlayerInfoLatency); ok && len(e.Entries) == 1 && e.Entries[0].Latency == 87 {
			found = true
		}
	}
	if !found {
		t.Fatal("no UPDATE_LATENCY with the player's 87 ms")
	}
}

// The 26.x gamerule editor asks for values; an operator gets them all by
// their canonical names, anyone else nothing.
func TestGameRuleEditorValues(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.KeepInventory = true
	s := &Server{hub: h, Ops: map[string]bool{}}
	r := &remotePlayer{s: s, p: pl.p, gm: -1}
	drainOut(pl.p)
	r.Action(attachproto.GameRuleReq{})
	runHubCmds(h, players)
	for _, ev := range drainEvs(pl.p) {
		if _, ok := ev.(attachproto.GameRuleValues); ok {
			t.Fatal("a non-operator must not be sent the rules")
		}
	}
	s.Ops[pl.p.name] = true
	r.Action(attachproto.GameRuleReq{})
	runHubCmds(h, players)
	var vals map[string]string
	for _, ev := range drainEvs(pl.p) {
		if e, ok := ev.(attachproto.GameRuleValues); ok {
			vals = e.Values
		}
	}
	if vals["keep_inventory"] != "true" || vals["random_tick_speed"] == "" {
		t.Fatalf("operator's values: keep_inventory=%q random_tick_speed=%q (%d rules)", vals["keep_inventory"], vals["random_tick_speed"], len(vals))
	}
}

// ServerPlayer.onEffectAdded sends the blend bit; onEffectUpdated does not.
func TestNewEffectBlendsIn(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	drainOut(pl.p)
	h.addEffect(players, pl, effDarkness, activeEffect{left: 200})
	h.addEffect(players, pl, effDarkness, activeEffect{left: 400})
	var blends []bool
	for _, ev := range drainEvs(pl.p) {
		if e, ok := ev.(attachproto.Effect); ok && e.ID == effDarkness && !e.Remove {
			blends = append(blends, e.Blend)
		}
	}
	if len(blends) != 2 || !blends[0] || blends[1] {
		t.Fatalf("blend flags %v, want [true false]", blends)
	}
}
