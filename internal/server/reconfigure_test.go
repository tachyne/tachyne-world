package server

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/tachyne/tachyne-world/internal/attach"
	"github.com/tachyne/tachyne-world/internal/world"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// attachIdentity is a Java session's identity with the gateway features
// its Hello listed.
func attachIdentity(name string, features []string) attach.Identity {
	return attach.Identity{Name: name, Edition: "java", Features: features}
}

// reconfServer is signedCmdServer with alice and bob on a gateway that can
// reconfigure (FeatureReconfigure, as adoptIdentity reads it) and carol on
// Bedrock.
func reconfServer(t *testing.T) (*Server, *hub, map[string]*player, map[string]*anyEvLog) {
	t.Helper()
	s, h, ps, logs := signedCmdServer(t)
	for _, name := range []string{"alice", "bob", "carol"} {
		features := []string{attachproto.FeaturePlayerChat, attachproto.FeatureReconfigure}
		if name == "carol" {
			features = nil
		}
		s.adoptIdentity(ps[name], attachIdentity(name, features))
	}
	ps["carol"].bedrock = true
	return s, h, ps, logs
}

// frameLog records what a remote emits synchronously (r.emit), by frame.
type frameLog struct {
	mu     sync.Mutex
	types  []byte
	frames [][]byte
}

func (l *frameLog) emit(typ byte, payload []byte) {
	l.mu.Lock()
	l.types = append(l.types, typ)
	l.frames = append(l.frames, payload)
	l.mu.Unlock()
}

// joinState is what the attach session's mkWelcome reads off a remote.
func joinState(r *remotePlayer) func() attachproto.Welcome {
	return func() attachproto.Welcome {
		x, y, z := r.Spawn()
		return attachproto.Welcome{EID: r.EID(), Spawn: attachproto.Pos{X: x, Y: y, Z: z},
			Gamemode: r.Gamemode(), Death: r.Death(), Dim: r.Dim()}
	}
}

// inHub reports whether a player is in the hub's player map.
func inHub(t *testing.T, h *hub, p *player) bool {
	var in bool
	onHub(t, h, func() { in = h.playersRef[p.eid] != nil })
	return in
}

// /debugconfig config sends a player back to configuration: out of the
// level (everyone else is told they left), sent MsgStartConfiguration with
// the dimension table, and nothing else. When their gateway answers that
// the client finished, the player is placed again where they stood: the
// rejoin (the join state, read after the move), the command tree, and the
// hub's join, which puts them back in everyone's tab list. A player whose
// gateway cannot reconfigure is refused.
func TestReconfigureRoundTrip(t *testing.T) {
	s, h, ps, logs := reconfServer(t)
	bob := ps["bob"]
	var bx, by, bz float64
	onHub(t, h, func() {
		tb := h.playersRef[bob.eid]
		tb.x, tb.z, tb.yaw = 3.5, -2.5, 90
		bx, by, bz = tb.x, tb.y, tb.z
	})
	s.handleCommand(ps["alice"], "debugconfig config bob")
	start := waitAnyEv(t, logs["bob"], "bob's start_configuration", func(ev any) bool {
		_, ok := ev.(attachproto.StartConfiguration)
		return ok
	}).(attachproto.StartConfiguration)
	if len(start.Dimensions) != len(world.Dimensions) || start.Dimensions[0].Key != "minecraft:overworld" {
		t.Errorf("start configuration data %+v", start.ConfigData)
	}
	waitAnyEv(t, logs["carol"], "bob leaving carol's tab list", func(ev any) bool {
		g, ok := ev.(attachproto.PlayerGone)
		return ok && g.UUID == bob.uuid
	})
	if inHub(t, h, bob) {
		t.Fatal("bob is still in the level while configuring")
	}
	markAll(t, h, map[string]*anyEvLog{"alice": logs["alice"], "carol": logs["carol"]}, "M1")
	if !hasLine(chatLines(logs["alice"]), "Switched player bob("+uuidString(bob.uuid)+") to config mode") {
		t.Errorf("alice's lines: %q", chatLines(logs["alice"]))
	}
	for _, ev := range logs["bob"].snapshot() {
		if c, ok := ev.(attachproto.Chat); ok && c.Text == "M1" {
			t.Error("bob was sent room chat while configuring")
		}
	}

	// The gateway: the client finished configuration.
	frames := &frameLog{}
	r := &remotePlayer{s: s, p: bob, emit: frames.emit, gm: -1}
	r.Configured(attachproto.Configured{View: 5}, joinState(r))
	waitJoined(t, h, "bob")
	frames.mu.Lock()
	types := append([]byte(nil), frames.types...)
	var first []byte
	if len(frames.frames) > 0 {
		first = frames.frames[0]
	}
	frames.mu.Unlock()
	if len(types) < 2 || types[0] != attachproto.MsgRejoin || types[1] != attachproto.MsgCommandTree {
		t.Fatalf("frames after configuration %#x", types)
	}
	var rj attachproto.Rejoin
	if err := json.Unmarshal(first, &rj); err != nil {
		t.Fatal(err)
	}
	if w := rj.Welcome; w.EID != bob.eid || w.Spawn.X != bx || w.Spawn.Y != by || w.Spawn.Z != bz || w.Dim != 0 || w.Gamemode != gmCreative {
		t.Errorf("rejoin welcome %+v, want bob at %v,%v,%v", w, bx, by, bz)
	}
	if bob.viewDist.Load() != 5 {
		t.Errorf("view distance %d", bob.viewDist.Load())
	}
	waitAnyEv(t, logs["carol"], "bob back in carol's tab list", func(ev any) bool {
		pi, ok := ev.(attachproto.PlayerInfo)
		return ok && pi.UUID == bob.uuid
	})
	// A stray second answer places nobody twice.
	r.Configured(attachproto.Configured{}, joinState(r))
	frames.mu.Lock()
	if len(frames.types) != len(types) {
		t.Errorf("a stray configured frame emitted %d more frames", len(frames.types)-len(types))
	}
	frames.mu.Unlock()

	// carol's gateway (Bedrock) cannot take her there.
	s.handleCommand(ps["alice"], "debugconfig config carol")
	markAll(t, h, map[string]*anyEvLog{"alice": logs["alice"]}, "M2")
	if !hasLine(chatLines(logs["alice"]), "Can't switch player carol("+uuidString(ps["carol"].uuid)+") to config mode") {
		t.Errorf("alice's lines: %q", chatLines(logs["alice"]))
	}
	if !inHub(t, h, ps["carol"]) {
		t.Error("carol left the level")
	}
	// Not an admin: no such command for bob.
	s.handleCommand(bob, "debugconfig config alice")
	if !inHub(t, h, ps["alice"]) {
		t.Error("a non-admin reconfigured alice")
	}
}

// A /reload's tags reach every session whose gateway takes them as one
// whole tag set (PlayerList.reloadResources' update_tags) — not a Bedrock
// session, nor one whose gateway cannot — and the same set rides in the
// configuration data a later join or reconfiguration is given.
func TestReloadSendsUpdateTags(t *testing.T) {
	t.Cleanup(func() { installPackContent(nil) })
	s, h, ps, logs := reconfServer(t)
	ps["bob"].reconfigure = false // an older Java gateway
	s.DataPackDir = t.TempDir()
	writePack(t, s.DataPackDir, "fp", "", map[string]string{
		"data/test/tags/item/gravelish.json":     `{"values":["minecraft:gravel"]}`,
		"data/minecraft/tags/item/planks.json":   `{"replace":true,"values":["minecraft:oak_planks"]}`,
		"data/test/tags/block/nothing_much.json": `{"values":["minecraft:stone"]}`,
	})
	onHub(t, h, func() { s.startFunctionRunner() })
	s.reloadDataPacks([]string{vanillaPackID, "file/fp"})
	upd := waitAnyEv(t, logs["alice"], "alice's update_tags", func(ev any) bool {
		_, ok := ev.(attachproto.UpdateTags)
		return ok
	}).(attachproto.UpdateTags)
	if len(upd.Tags) != 2 || upd.Tags[0].Registry != "block" || upd.Tags[1].Registry != "item" {
		t.Fatalf("tag sets %+v", upd.Tags)
	}
	items := map[string][]string{}
	for _, tg := range upd.Tags[1].Tags {
		items[tg.Name] = tg.Entries
	}
	if g := items["test:gravelish"]; len(g) != 1 || g[0] != "minecraft:gravel" {
		t.Errorf("test:gravelish %v", g)
	}
	if p := items["minecraft:planks"]; len(p) != 1 || p[0] != "minecraft:oak_planks" {
		t.Errorf("minecraft:planks %v", p)
	}
	if b := upd.Tags[0].Tags; len(b) != 1 || b[0].Name != "test:nothing_much" {
		t.Errorf("block tags %+v", b)
	}
	markAll(t, h, logs, "M1")
	for _, name := range []string{"bob", "carol"} {
		for _, ev := range logs[name].snapshot() {
			if _, ok := ev.(attachproto.UpdateTags); ok {
				t.Errorf("%s's gateway was sent update_tags", name)
			}
		}
	}
	if cd := s.configData(); len(cd.Tags) != 2 || cd.Tags[1].Registry != "item" || len(cd.Dimensions) != 3 {
		t.Errorf("join configuration data %+v", cd)
	}
	for _, ev := range []any{attachproto.UpdateTags{}, attachproto.StartConfiguration{}, attachproto.Rejoin{}} {
		if !isLifecycleFrame(ev) {
			t.Errorf("%T may be dropped", ev)
		}
	}
}

// A vanilla tag no pack loads any more goes out as an empty member list,
// so the client's built-in members do not stand.
func TestPackTagSetsEmptyForUnloadedTag(t *testing.T) {
	pc := &packContent{tags: &tagRegistry{
		tags:    packTagSet{"item": {}},
		changed: map[string]map[string]bool{"item": {"minecraft:planks": true}},
	}}
	sets := packTagSets(pc)
	if len(sets) != 1 || len(sets[0].Tags) != 1 || sets[0].Tags[0].Entries == nil || len(sets[0].Tags[0].Entries) != 0 {
		t.Fatalf("%+v", sets)
	}
	b, _ := json.Marshal(sets[0].Tags[0])
	if string(b) != `{"name":"minecraft:planks","entries":[]}` {
		t.Errorf("wire form %s", b)
	}
	if packTagSets(nil) != nil {
		t.Error("no load: no tags")
	}
}
