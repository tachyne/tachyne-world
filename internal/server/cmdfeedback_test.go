package server

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// chatLog records every chat line a player is sent, draining its queue as a
// session writer would so nothing is dropped on a full channel.
type chatLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *chatLog) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

func recordChat(t *testing.T, p *player) *chatLog {
	c := &chatLog{}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case pkt := <-p.out:
				if ch, ok := pkt.ev.(attachproto.Chat); ok {
					c.mu.Lock()
					c.lines = append(c.lines, ch.Text)
					c.mu.Unlock()
				}
			case <-stop:
				return
			}
		}
	}()
	return c
}

// feedbackServer is a live hub with two operators and one ordinary player.
func feedbackServer(t *testing.T) (*Server, *hub, map[string]*player, map[string]*chatLog) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative), Ops: map[string]bool{"alice": true, "bob": true}}
	h.isOp = s.isOp
	w.ForceLoad(0, 0, 2)
	startHub(t, h)
	ps, logs := map[string]*player{}, map[string]*chatLog{}
	for _, name := range []string{"alice", "bob", "carol"} {
		p := newPlayer(h.allocEID(), name, [16]byte{byte(len(ps) + 1)})
		sy := w.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		logs[name] = recordChat(t, p)
		h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
		waitJoined(t, h, name)
		ps[name] = p
	}
	return s, h, ps, logs
}

// settle waits until everything posted so far has reached every player: a
// marker line goes out after it, FIFO, and each log must show the marker.
func settle(t *testing.T, h *hub, logs map[string]*chatLog, mark string) {
	t.Helper()
	h.post(evChat{text: mark})
	deadline := time.Now().Add(hubTestWait)
	for name, c := range logs {
		for {
			seen := false
			for _, l := range c.all() {
				seen = seen || l == mark
			}
			if seen {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never received the marker %q", name, mark)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// linesBetween is what a player was sent after marker from and before to.
func linesBetween(c *chatLog, from, to string) []string {
	var out []string
	in := from == ""
	for _, l := range c.all() {
		switch {
		case l == from:
			in = true
		case l == to:
			return out
		case in:
			out = append(out, l)
		}
	}
	return out
}

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// send_command_feedback and log_admin_commands, as CommandSourceStack uses
// them: a success line reaches its caller and — gray, italic, bracketed —
// the other operators while feedback is on; the log keeps it while
// log_admin_commands is on; and a failure always gets through.
func TestCommandFeedbackRules(t *testing.T) {
	var logBuf bytes.Buffer
	var logMu sync.Mutex
	log.SetOutput(writerFunc(func(b []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return logBuf.Write(b)
	}))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	logged := func(s string) bool {
		logMu.Lock()
		defer logMu.Unlock()
		return strings.Contains(logBuf.String(), s)
	}

	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]

	// Defaults: both rules on. A session-side command and a hub-side one.
	s.handleCommand(alice, "weather clear")
	s.handleCommand(alice, "effect give @s speed 10 0")
	settle(t, h, logs, "M1")
	a, b, c := linesBetween(logs["alice"], "", "M1"), linesBetween(logs["bob"], "", "M1"), linesBetween(logs["carol"], "", "M1")
	if !hasLine(a, "Weather set to clear") || !hasPrefixLine(a, "Applied effect ") {
		t.Fatalf("the caller should see its success lines, got %q", a)
	}
	if !hasLine(b, "§7§o[alice: Weather set to clear]") || !hasPrefixLine(b, "§7§o[alice: Applied effect ") {
		t.Fatalf("another operator should be told, got %q", b)
	}
	for _, l := range append(c, a...) {
		if strings.HasPrefix(l, "§7§o[alice:") {
			t.Fatalf("the admin line went to the caller or a non-operator: %q", l)
		}
	}
	if !logged("[alice: Weather set to clear]") {
		t.Error("log_admin_commands on: the server log should record the command")
	}
	// A query is sendSuccess(false): its caller only.
	s.handleCommand(alice, "time query daytime")
	settle(t, h, logs, "M2")
	if a := linesBetween(logs["alice"], "M1", "M2"); len(a) != 1 || !strings.HasPrefix(a[0], "The time is ") {
		t.Fatalf("a query answers its caller: %q", a)
	}
	if b := linesBetween(logs["bob"], "M1", "M2"); len(b) != 0 {
		t.Fatalf("a query is not broadcast to operators: %q", b)
	}

	// send_command_feedback off: silence for success, failures still speak,
	// and the log still records it.
	s.handleCommand(alice, "gamerule send_command_feedback false")
	s.handleCommand(alice, "weather rain")
	s.handleCommand(alice, "weather hail")
	settle(t, h, logs, "M3")
	a = linesBetween(logs["alice"], "M2", "M3")
	if len(a) != 1 || !strings.HasPrefix(a[0], "Usage: /weather") {
		t.Fatalf("with feedback off only the failure should reach the caller, got %q", a)
	}
	if b := linesBetween(logs["bob"], "M2", "M3"); len(b) != 0 {
		t.Fatalf("with feedback off other operators hear nothing, got %q", b)
	}
	if !logged("[alice: Weather set to rain]") {
		t.Error("log_admin_commands is independent of feedback: the log should still record it")
	}

	// log_admin_commands off: the log is quiet too.
	s.handleCommand(alice, "gamerule log_admin_commands false")
	s.handleCommand(alice, "weather thunder")
	settle(t, h, logs, "M4")
	if logged("[alice: Weather set to thunder]") {
		t.Error("log_admin_commands off: the command should not be logged")
	}
	var v string
	onHub(t, h, func() { v, _ = h.ruleValueText("send_command_feedback") })
	if v != "false" {
		t.Errorf("send_command_feedback reads %q", v)
	}
}

func hasPrefixLine(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
