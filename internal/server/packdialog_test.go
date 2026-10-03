package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tachyne/tachyne-common/shard"
	"github.com/tachyne/tachyne-world/internal/attach"
	"github.com/tachyne/tachyne-world/internal/world"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// A data pack adds dialogs to the registry and puts them in the pause menu
// through the minecraft:pause_screen_additions tag. The reload that brings
// new registry entries sends each player who can be reconfigured back
// through configuration with the entries and the tags (bob, whose gateway
// cannot, stays in the level); a dialog that does not decode is left
// out; /dialog show finds the pack's dialog; a second reload with the same
// entries reconfigures nobody and only resends the tags.
func TestPackDialogsReachConfiguration(t *testing.T) {
	t.Cleanup(func() { installPackContent(nil) })
	s, h, ps, logs := reconfServer(t)
	ps["bob"].reconfigure = false
	s.DataPackDir = t.TempDir()
	writePack(t, s.DataPackDir, "fp", "", map[string]string{
		"data/test/dialog/hello.json": `{"type":"minecraft:notice","title":"Hello","body":{"type":"minecraft:plain_message","contents":"Welcome"},` +
			`"action":{"label":"Rules","action":{"type":"minecraft:show_dialog","dialog":"test:rules"}}}`,
		"data/test/dialog/rules.json":                            `{"type":"minecraft:notice","title":"Rules"}`,
		"data/test/dialog/bad.json":                              `{"type":"minecraft:notice"}`,
		"data/minecraft/tags/dialog/pause_screen_additions.json": `{"values":["test:hello"]}`,
		"data/minecraft/tags/dialog/quick_actions.json":          `{"values":["test:rules","test:bad"]}`,
	})
	onHub(t, h, func() { s.startFunctionRunner() })
	s.reloadDataPacks([]string{vanillaPackID, "file/fp"})
	start := waitAnyEv(t, logs["alice"], "alice's start_configuration", func(ev any) bool {
		_, ok := ev.(attachproto.StartConfiguration)
		return ok
	}).(attachproto.StartConfiguration)
	if len(start.Registries) != 1 || start.Registries[0].Registry != "minecraft:dialog" || len(start.Registries[0].Entries) != 2 {
		t.Fatalf("registries %+v", start.Registries)
	}
	if e := start.Registries[0].Entries[0]; e.Name != "test:hello" || !json.Valid(e.Data) {
		t.Errorf("first entry %s %s", e.Name, e.Data)
	}
	dialogTags := map[string][]string{}
	for _, set := range start.Tags {
		if set.Registry == "dialog" {
			for _, tg := range set.Tags {
				dialogTags[tg.Name] = tg.Entries
			}
		}
	}
	if p := dialogTags["minecraft:pause_screen_additions"]; len(p) != 1 || p[0] != "test:hello" {
		t.Errorf("pause_screen_additions %v (all %v)", p, dialogTags)
	}
	if q, ok := dialogTags["minecraft:quick_actions"]; !ok || len(q) != 0 {
		t.Errorf("quick_actions names a dialog that did not load, so it loads empty: %v %v", q, ok)
	}
	if inHub(t, h, ps["alice"]) {
		t.Error("alice is still in the level")
	}
	if !inHub(t, h, ps["bob"]) || !inHub(t, h, ps["carol"]) {
		t.Error("bob or carol left the level")
	}
	if d, msg := parseDialogArg("test:hello"); msg != "" || d.Ref != "test:hello" {
		t.Errorf("/dialog show test:hello: %+v %q", d, msg)
	}
	if _, msg := parseDialogArg("test:bad"); msg == "" {
		t.Error("a dialog that did not load was shown")
	}

	// alice finishes configuration; the same packs again change no entries.
	r := &remotePlayer{s: s, p: ps["alice"], emit: (&frameLog{}).emit, gm: -1}
	r.Configured(attachproto.Configured{}, joinState(r))
	waitJoined(t, h, "alice")
	logs["alice"].reset()
	s.reloadDataPacks([]string{vanillaPackID, "file/fp"})
	waitAnyEv(t, logs["alice"], "alice's update_tags", func(ev any) bool {
		_, ok := ev.(attachproto.UpdateTags)
		return ok
	})
	for _, ev := range logs["alice"].snapshot() {
		if _, ok := ev.(attachproto.StartConfiguration); ok {
			t.Error("an unchanged registry reconfigured alice")
		}
	}
	if cd := s.configData(); len(cd.Registries) != 1 {
		t.Errorf("a join's configuration data %+v", cd.Registries)
	}
}

// A custom click action from a player in a configuration phase (out of the
// level) still reaches plugins and the bus; one from a player who left
// does not.
func TestCustomClickActionInConfiguration(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.DoMobSpawning = false
	rec := &recordingBus{}
	h.bus = rec
	h.registerBusBridge()
	startHub(t, h)
	p := newPlayer(h.allocEID(), "alice", [16]byte{1})
	p.reconfigure = true
	h.post(evJoin{p: p, x: 0.5, y: 80, z: 0.5})
	waitJoined(t, h, "alice")
	var sent bool
	onHub(t, h, func() { sent = h.reconfigure(h.playersRef, h.playersRef[p.eid]) })
	if !sent || inHub(t, h, p) {
		t.Fatal("alice was not sent to configuration")
	}
	r := &remotePlayer{s: &Server{hub: h}, p: p}
	r.Action(attachproto.CustomClickAction{ID: "myplugin:accept", Payload: json.RawMessage(`{"ok":1}`)})
	deadline := time.Now().Add(hubTestWait)
	for {
		if raw, ok := rec.get("player_custom_click"); ok {
			var ev struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil || ev.Name != "alice" || ev.ID != "myplugin:accept" {
				t.Fatalf("player_custom_click payload %s (err %v)", raw, err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a configuration-phase custom click never reached the bus")
		}
		time.Sleep(10 * time.Millisecond)
	}
	gone := newPlayer(h.allocEID(), "bob", [16]byte{2})
	rec.mu.Lock()
	delete(rec.topics, "player_custom_click")
	rec.mu.Unlock()
	(&remotePlayer{s: &Server{hub: h}, p: gone}).Action(attachproto.CustomClickAction{ID: "x:y"})
	onHub(t, h, func() {})
	if _, ok := rec.get("player_custom_click"); ok {
		t.Error("a player in no phase was heard")
	}
}

// A player's secure-chat session crosses a shard seam with them: the
// destination lists them with it once their gateway resumes there.
func TestHandoverCarriesChatSession(t *testing.T) {
	topo := shard.Map{Version: 1, Regions: []shard.Region{
		{SID: 0, MinCX: -16, MinCZ: -8, W: 16, H: 16},
		{SID: 1, MinCX: 0, MinCZ: -8, W: 16, H: 16},
	}}
	shardOf := func(cx, cz int32) int32 { return topo.ShardOf(0, cx, cz) }
	hubA := newTestHub(world.New(1))
	hubA.sid, hubA.shardOf = 0, shardOf
	hubB := newTestHub(world.New(1))
	hubB.sid, hubB.shardOf = 1, shardOf
	hubB.rules.DoMobSpawning = false
	startHub(t, hubB)
	playersA := map[int32]*tracked{}
	hubA.peers = &fakeMesh{self: 0, deliver: map[int32]func(int32, byte, []byte){
		1: func(from int32, typ byte, payload []byte) {
			onHub(t, hubB, func() { hubB.handlePeerFrame(hubB.playersRef, from, typ, payload) })
		},
	}}
	hubB.peers = &fakeMesh{self: 1, deliver: map[int32]func(int32, byte, []byte){
		0: func(from int32, typ byte, payload []byte) { hubA.handlePeerFrame(playersA, from, typ, payload) },
	}}
	eid := shard.MintEID(1, shard.PlayerSID)
	p := newPlayer(eid, "wesley", [16]byte{1, 2, 3})
	sess := attachproto.ChatSession{SessionID: [16]byte{9}, ExpiresAt: 1 << 50, Key: []byte{1, 2}, KeySig: []byte{3}}
	p.chatSession.Store(&sess)
	src := &tracked{p: p, x: 8, y: 71, gamemode: gmSurvival, health: 20, food: 20, inv: &inventory{},
		living: living{effects: map[int32]*activeEffect{}}}
	playersA[eid] = src
	hubA.checkSeamCrossing(playersA, src)
	if _, ok := playersA[eid]; ok {
		t.Fatal("no handover")
	}
	s := &Server{hub: hubB, modes: newModeStore("", gmSurvival)}
	r, err := s.ResumeRemote(attach.Identity{Name: "wesley", UUID: p.uuid, Edition: "java",
		Features: []string{attachproto.FeaturePlayerChat}}, "0.1", func(byte, []byte) {})
	if err != nil {
		t.Fatal(err)
	}
	waitJoined(t, hubB, "wesley")
	got := r.(*remotePlayer).p.chatSession.Load()
	if got == nil || got.SessionID != sess.SessionID || string(got.Key) != string(sess.Key) {
		t.Errorf("chat session after the crossing: %+v", got)
	}
}

// reset forgets what the log has recorded.
func (l *anyEvLog) reset() {
	l.mu.Lock()
	l.evs = nil
	l.mu.Unlock()
}
