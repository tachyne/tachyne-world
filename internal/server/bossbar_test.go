package server

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// evRecorder keeps a player's chat lines (as chatLog, for settle) and the
// boss-bar frames they were sent.
type evRecorder struct {
	chat *chatLog
	mu   sync.Mutex
	bars []attachproto.BossBar
}

func (r *evRecorder) barFrames() []attachproto.BossBar {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]attachproto.BossBar(nil), r.bars...)
}

func recordEvents(t *testing.T, p *player) *evRecorder {
	r := &evRecorder{chat: &chatLog{}}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case pkt := <-p.out:
				switch ev := pkt.ev.(type) {
				case attachproto.Chat:
					r.chat.mu.Lock()
					r.chat.lines = append(r.chat.lines, ev.Text)
					r.chat.mu.Unlock()
				case attachproto.BossBar:
					r.mu.Lock()
					r.bars = append(r.bars, ev)
					r.mu.Unlock()
				}
			case <-stop:
				return
			}
		}
	}()
	return r
}

// eventServer is feedbackServer with full event recording: alice and bob
// are operators, carol is not.
func eventServer(t *testing.T, rulesPath string) (*Server, *hub, map[string]*player, map[string]*chatLog, map[string]*evRecorder) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	h.rules.DoMobSpawning = false
	h.rulesPath = rulesPath
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative), Ops: map[string]bool{"alice": true, "bob": true}}
	h.isOp = s.isOp
	w.ForceLoad(0, 0, 2)
	startHub(t, h)
	ps, logs, recs := map[string]*player{}, map[string]*chatLog{}, map[string]*evRecorder{}
	for _, name := range []string{"alice", "bob", "carol"} {
		p := newPlayer(h.allocEID(), name, [16]byte{byte(len(ps) + 1)})
		sy := w.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		recs[name] = recordEvents(t, p)
		logs[name] = recs[name].chat
		h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
		waitJoined(t, h, name)
		ps[name] = p
	}
	return s, h, ps, logs, recs
}

// /bossbar through the dispatcher: a bar is made, shown to players,
// filled, restyled and queried with vanilla's lines; it is saved with the
// settings; and the unchanged / duplicate / unknown cases fail.
func TestCommandBossbar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, h, ps, logs, recs := eventServer(t, path)
	alice := ps["alice"]
	uuid := bossbarUUID("minecraft:test")

	s.handleCommand(alice, `bossbar add test "Hello"`)
	s.handleCommand(alice, "bossbar set test players @a")
	s.handleCommand(alice, "bossbar set test value 50")
	s.handleCommand(alice, "bossbar get test players")
	settle(t, h, logs, "B1")
	a := linesBetween(logs["alice"], "", "B1")
	for _, want := range []string{
		"Created custom bossbar §f[Hello§r§f]§r",
		"Custom bossbar §f[Hello§r§f]§r now has 3 player(s): alice, bob, carol",
		"Custom bossbar §f[Hello§r§f]§r has changed value to 50",
		"Custom bossbar §f[Hello§r§f]§r has 3 player(s) currently online: alice, bob, carol",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	bars := recs["carol"].barFrames()
	if len(bars) != 2 || bars[0].Op != attachproto.BossBarAdd || bars[0].UUID != uuid || bars[0].Title != "Hello" ||
		bars[0].Color != attachproto.BossWhite || bars[1].Op != attachproto.BossBarHealth || bars[1].Health != 0.5 {
		t.Fatalf("carol's bar frames: %+v", bars)
	}

	// Failures: a duplicate id, an unchanged value, an unknown bar, and a
	// component the title cannot show.
	s.handleCommand(alice, "bossbar add test x")
	s.handleCommand(alice, "bossbar set test value 50")
	s.handleCommand(alice, "bossbar get nope value")
	s.handleCommand(alice, `bossbar set test name {"translate":"x"}`)
	// A non-operator is refused.
	s.handleCommand(ps["carol"], "bossbar list")
	settle(t, h, logs, "B2")
	a = linesBetween(logs["alice"], "B1", "B2")
	for _, want := range []string{
		"A bossbar already exists with the ID 'minecraft:test'",
		"Nothing changed. That's already the value of this bossbar",
		"No bossbar exists with the ID 'minecraft:nope'",
		"The translate component is not supported here",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "B1", "B2"); permissionRefusals(c) != 1 {
		t.Errorf("non-op: %q", c)
	}

	// A styled name and a new colour update the bar in place (UPDATE_NAME,
	// UPDATE_STYLE); hiding removes it.
	s.handleCommand(alice, `bossbar set test name {text:"Boss",color:red,bold:1b}`)
	s.handleCommand(alice, "bossbar set test color red")
	s.handleCommand(alice, "bossbar set test visible false")
	s.handleCommand(alice, "bossbar list")
	settle(t, h, logs, "B3")
	a = linesBetween(logs["alice"], "B2", "B3")
	if !hasLine(a, "Custom bossbar §c[§r§c§lBoss§r§c]§r has changed color") ||
		!hasLine(a, "There are 1 custom bossbar(s) active: §c[§r§c§lBoss§r§c]§r") {
		t.Errorf("restyle lines: %q", a)
	}
	bars = recs["bob"].barFrames()
	last := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	name := bars[len(bars)-3]
	if last.Op != attachproto.BossBarRemove || prev.Op != attachproto.BossBarStyle || prev.Color != attachproto.BossRed ||
		name.Op != attachproto.BossBarTitle || name.Title != "§r§c§lBoss" {
		t.Errorf("bob's last frames: %+v %+v %+v", name, prev, last)
	}

	// Saved with the settings.
	var raw []byte
	onHub(t, h, func() { raw, _ = os.ReadFile(path) })
	if !strings.Contains(string(raw), `"customBossEvents"`) || !strings.Contains(string(raw), `"minecraft:test"`) {
		t.Errorf("the bar was not saved: %s", raw)
	}

	s.handleCommand(alice, "bossbar remove test")
	s.handleCommand(alice, "bossbar list")
	settle(t, h, logs, "B4")
	if a := linesBetween(logs["alice"], "B3", "B4"); !hasLine(a, "Removed custom bossbar §c[§r§c§lBoss§r§c]§r") ||
		!hasLine(a, "There are no custom bossbars active") {
		t.Errorf("remove: %q", a)
	}
}

func TestTextComponentSubset(t *testing.T) {
	for _, tc := range []struct{ in, want, why string }{
		{`"plain"`, "plain", ""},
		{`word`, "word", ""},
		{`{"text":"a","extra":[{"text":"b","color":"gold"},"c"]}`, "a§r§6b§rc", ""},
		{`["x",{"text":"y","italic":true}]`, "x§r§oy", ""},
		{`{text:"hi",color:"#ff0000"}`, "", "The colour #ff0000 is not supported here"},
		{`{"score":{"name":"a","objective":"b"}}`, "", "The score component is not supported here"},
	} {
		got, why, ok := parseTextComponent(tc.in)
		if !ok || got != tc.want || why != tc.why {
			t.Errorf("%s → %q %q %v", tc.in, got, why, ok)
		}
	}
	if _, _, ok := parseTextComponent(`two words`); ok {
		t.Error("two bare words are not a component")
	}
}
