package server

import (
	"sync"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// anyEvLog records every domain event a player is sent.
type anyEvLog struct {
	mu  sync.Mutex
	evs []any
}

func (l *anyEvLog) snapshot() []any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]any(nil), l.evs...)
}

func recordAllEvents(t *testing.T, p *player) *anyEvLog {
	l := &anyEvLog{}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case pkt := <-p.out:
				l.mu.Lock()
				l.evs = append(l.evs, pkt.ev)
				l.mu.Unlock()
			case <-p.critWake:
				for _, pkt := range p.takeCrit() {
					l.mu.Lock()
					l.evs = append(l.evs, pkt.ev)
					l.mu.Unlock()
				}
			case <-stop:
				return
			}
		}
	}()
	return l
}

// waitAnyEv polls a log until match finds an event.
func waitAnyEv(t *testing.T, l *anyEvLog, what string, match func(any) bool) any {
	t.Helper()
	deadline := time.Now().Add(hubTestWait)
	for {
		for _, ev := range l.snapshot() {
			if match(ev) {
				return ev
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("never saw %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Secure chat through the remote entry points: the session a gateway
// validated reaches every player-chat session as INITIALIZE_CHAT and rides
// later tab-list adds; a signed message goes to every such session as
// player_chat with its signature and signed body, and to a session whose
// gateway does not render player_chat (Bedrock) as the Sender relay.
func TestSignedChatRelay(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative)}
	w.ForceLoad(0, 0, 2)
	startHub(t, h)

	ps, logs := map[string]*player{}, map[string]*anyEvLog{}
	for i, name := range []string{"alice", "bob", "carol"} {
		p := newPlayer(h.allocEID(), name, [16]byte{byte(i + 1)})
		p.playerChat = name != "carol" // carol came in through the Bedrock gateway
		sy := w.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		logs[name] = recordAllEvents(t, p)
		h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
		waitJoined(t, h, name)
		ps[name] = p
	}
	alice := &remotePlayer{s: s, p: ps["alice"]}

	sess := attachproto.ChatSession{SessionID: [16]byte{0x5e}, ExpiresAt: 123, Key: []byte{1, 2}, KeySig: []byte{3}}
	alice.ChatSession(sess)
	got := waitAnyEv(t, logs["bob"], "alice's INITIALIZE_CHAT", func(ev any) bool {
		e, ok := ev.(attachproto.PlayerInfoChat)
		return ok && e.UUID == ps["alice"].uuid && e.Session != nil
	}).(attachproto.PlayerInfoChat)
	if got.Session.SessionID != sess.SessionID || got.Session.ExpiresAt != 123 {
		t.Fatalf("session %+v", got.Session)
	}
	if pi := infoAdd(ps["alice"], gmCreative); pi.Chat == nil || pi.Chat.SessionID != sess.SessionID {
		t.Fatalf("a tab-list add of alice lacks her chat session: %+v", pi.Chat)
	}

	sig := make([]byte, 256)
	sig[5] = 9
	seen := [][]byte{make([]byte, 256)}
	alice.SignedChat(attachproto.Chat{Text: "hello", Signed: &attachproto.SignedChat{Index: 2, Signature: sig, Timestamp: 1000, Salt: 77, LastSeen: seen}})
	for _, name := range []string{"alice", "bob"} {
		pc := waitAnyEv(t, logs[name], name+"'s player_chat", func(ev any) bool {
			_, ok := ev.(attachproto.PlayerChat)
			return ok
		}).(attachproto.PlayerChat)
		if pc.Sender != ps["alice"].uuid || pc.SenderName != "alice" || pc.Content != "hello" || pc.Index != 2 ||
			pc.Timestamp != 1000 || pc.Salt != 77 || len(pc.Signature) != 256 || pc.Signature[5] != 9 || len(pc.LastSeen) != 1 || pc.Unsigned != "" {
			t.Fatalf("%s got %+v", name, pc)
		}
	}
	waitAnyEv(t, logs["carol"], "carol's relay", func(ev any) bool {
		c, ok := ev.(attachproto.Chat)
		return ok && c.Text == "hello" && c.Sender == "alice"
	})
	for _, ev := range logs["carol"].snapshot() {
		switch ev.(type) {
		case attachproto.PlayerChat, attachproto.PlayerInfoChat:
			t.Fatalf("carol's gateway was sent %T", ev)
		}
	}
}

// Signed chat is a chain: its frames must never drop under back-pressure.
func TestSignedChatFramesReliable(t *testing.T) {
	for _, ev := range []any{attachproto.PlayerChat{}, attachproto.PlayerInfoChat{}, attachproto.DeleteChat{}} {
		if !isLifecycleFrame(ev) {
			t.Errorf("%T is droppable", ev)
		}
	}
}
