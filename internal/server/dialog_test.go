package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// dialogServer: alice and bob are operators on dialog-rendering gateways;
// carol is not an operator and came in through a gateway without dialogs.
func dialogServer(t *testing.T) (*Server, *hub, map[string]*player, map[string]*anyEvLog) {
	t.Helper()
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative), Ops: map[string]bool{"alice": true, "bob": true}}
	h.isOp = s.isOp
	w.ForceLoad(0, 0, 2)
	startHub(t, h)
	ps, logs := map[string]*player{}, map[string]*anyEvLog{}
	for i, name := range []string{"alice", "bob", "carol"} {
		p := newPlayer(h.allocEID(), name, [16]byte{byte(i + 1)})
		p.dialogs = name != "carol"
		sy := w.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		logs[name] = recordAllEvents(t, p)
		h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
		waitJoined(t, h, name)
		ps[name] = p
	}
	return s, h, ps, logs
}

func sawLine(_ *anyEvLog, text string) func(any) bool {
	return func(ev any) bool {
		c, ok := ev.(attachproto.Chat)
		return ok && c.Text == text
	}
}

// isShow matches a ShowDialog by its reference ("" = an inline dialog).
func isShow(ref string) func(any) bool {
	return func(ev any) bool {
		e, ok := ev.(attachproto.ShowDialog)
		return ok && e.Ref == ref
	}
}

func countDialogs(l *anyEvLog) (shows []attachproto.ShowDialog, clears int) {
	for _, ev := range l.snapshot() {
		switch e := ev.(type) {
		case attachproto.ShowDialog:
			shows = append(shows, e)
		case attachproto.ClearDialog:
			clears++
		}
	}
	return
}

// /dialog through the dispatcher: an inline dialog to one player, a registry
// dialog to everyone, clear, and the refusals.
func TestDialogCommand(t *testing.T) {
	s, _, ps, logs := dialogServer(t)
	alice := ps["alice"]

	s.handleCommand(alice, `dialog show bob {type: "minecraft:notice", title: "Rules", body: {type: "minecraft:plain_message", contents: "Be kind"}}`)
	waitAnyEv(t, logs["alice"], "the single feedback", sawLine(logs["alice"], "Displayed dialog to bob"))
	waitAnyEv(t, logs["bob"], "bob's dialog", isShow(""))
	shows, _ := countDialogs(logs["bob"])
	if len(shows) != 1 || shows[0].Ref != "" {
		t.Fatalf("bob's dialogs: %+v", shows)
	}
	var d map[string]any
	if err := json.Unmarshal(shows[0].Dialog, &d); err != nil || d["type"] != "minecraft:notice" || d["title"] != "Rules" {
		t.Fatalf("inline dialog %s (%v)", shows[0].Dialog, err)
	}

	s.handleCommand(alice, "dialog show @a server_links")
	waitAnyEv(t, logs["alice"], "the multiple feedback", sawLine(logs["alice"], "Displayed dialog to 3 players"))
	for _, name := range []string{"alice", "bob"} {
		waitAnyEv(t, logs[name], name+"'s server_links", isShow("minecraft:server_links"))
		shows, _ := countDialogs(logs[name])
		if len(shows) == 0 || shows[len(shows)-1].Ref != "minecraft:server_links" {
			t.Fatalf("%s did not get server_links: %+v", name, shows)
		}
	}
	if shows, clears := countDialogs(logs["carol"]); len(shows) != 0 || clears != 0 {
		t.Fatal("a gateway without dialogs was sent one")
	}

	s.handleCommand(alice, "dialog clear bob")
	waitAnyEv(t, logs["alice"], "the clear feedback", sawLine(logs["alice"], "Cleared dialog for bob"))
	waitAnyEv(t, logs["bob"], "bob's clear", func(ev any) bool {
		_, ok := ev.(attachproto.ClearDialog)
		return ok
	})
	if _, clears := countDialogs(logs["bob"]); clears != 1 {
		t.Fatalf("bob's clears = %d", clears)
	}

	// Refusals: an unknown registry dialog, a malformed one, no permission.
	s.handleCommand(alice, "dialog show bob minecraft:nope")
	waitAnyEv(t, logs["alice"], "the unknown-dialog refusal", sawLine(logs["alice"], "Can't find element 'minecraft:nope' in registry 'minecraft:dialog'"))
	s.handleCommand(alice, `dialog show bob {type: "minecraft:confirmation", title: "Sure?", yes: {label: "Yes"}}`)
	waitAnyEv(t, logs["alice"], "the malformed-dialog refusal", func(ev any) bool {
		c, ok := ev.(attachproto.Chat)
		return ok && strings.HasPrefix(c.Text, "Failed to parse structure: No key no")
	})
	s.handleCommand(ps["carol"], "dialog clear alice")
	waitAnyEv(t, logs["carol"], "carol's refusal", sawLine(logs["carol"], "You don't have permission."))
	if shows, _ := countDialogs(logs["bob"]); len(shows) != 2 {
		t.Fatalf("a refused /dialog reached bob: %d dialogs", len(shows))
	}
}

// The codec checks, directly: each dialog type's required fields.
func TestCheckDialog(t *testing.T) {
	for in, want := range map[string]string{
		`{type: notice, title: "x"}`: "",
		`{type: "minecraft:multi_action", title: "x", actions: [{label: "a", action: {type: "dynamic/custom", id: "p:go"}}], inputs: [{type: text, key: "name", label: "Name"}]}`: "",
		`{type: dialog_list, title: "x", dialogs: "#minecraft:quick_actions"}`:                                                                                                    "",
		`{type: dialog_list, title: "x", dialogs: ["server_links", {type: notice, title: "y"}]}`:                                                                                  "",
		`{type: notice}`: "No key title in MapLike",
		`{title: "x"}`:   "No key type in MapLike",
		`{type: "minecraft:multi_action", title: "x", actions: []}`:                               "actions must be a non-empty list",
		`{type: notice, title: "x", inputs: [{type: text, key: "a b", label: "A"}]}`:              "a b is not a valid input name",
		`{type: notice, title: "x", action: {label: "go", action: {type: open_file, path: "/"}}}`: "Unknown action type minecraft:open_file",
		`{type: dialog_list, title: "x", dialogs: ["nope"]}`:                                      "Unknown dialog nope",
	} {
		v, err := parseSNBT(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := checkDialog(v.(map[string]any)); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

// A custom click action reaches plugins and the bus as
// player_custom_click, through the remote entry point.
func TestCustomClickActionReachesTheBus(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.DoMobSpawning = false
	rec := &recordingBus{}
	h.bus = rec
	h.registerBusBridge()
	startHub(t, h)
	p := newPlayer(h.allocEID(), "alice", [16]byte{1})
	h.post(evJoin{p: p, x: 0.5, y: 80, z: 0.5})
	waitJoined(t, h, "alice")
	s := &Server{hub: h}
	r := &remotePlayer{s: s, p: p}
	r.Action(attachproto.CustomClickAction{ID: "myplugin:submit", Payload: json.RawMessage(`{"name":"Bob"}`)})
	deadline := time.Now().Add(hubTestWait)
	for {
		if raw, ok := rec.get("player_custom_click"); ok {
			var ev struct {
				EID     int32           `json:"eid"`
				Name    string          `json:"name"`
				ID      string          `json:"id"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil || ev.EID != p.eid || ev.Name != "alice" || ev.ID != "myplugin:submit" || string(ev.Payload) != `{"name":"Bob"}` {
				t.Fatalf("player_custom_click payload %s (err %v)", raw, err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("player_custom_click never published")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Dialog frames are one-shot: they must not drop.
func TestDialogFramesReliable(t *testing.T) {
	if !isLifecycleFrame(attachproto.ShowDialog{}) || !isLifecycleFrame(attachproto.ClearDialog{}) {
		t.Fatal("dialog frames are droppable")
	}
}
