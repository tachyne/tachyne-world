package attach

import (
	"net"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"

	proto "github.com/tachyne/tachyne-common/attach"
)

// signedRemote records what secure chat reaches the engine with.
type signedRemote struct {
	mockRemote
	chats    chan string
	signed   chan proto.Chat
	sessions chan proto.ChatSession
}

func (r *signedRemote) Chat(text string)                { r.chats <- text }
func (r *signedRemote) SignedChat(ch proto.Chat)        { r.signed <- ch }
func (r *signedRemote) ChatSession(s proto.ChatSession) { r.sessions <- s }

// A signed chat frame reaches the engine with its signature, an unsigned one
// as plain text, and the chat session as its own frame; the Hello's
// features ride into the Identity.
func TestSignedChatFramesReachTheEngine(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rec := &signedRemote{chats: make(chan string, 2), signed: make(chan proto.Chat, 2), sessions: make(chan proto.ChatSession, 2)}
	ids := make(chan Identity, 1)
	w := world.New(1)
	go Serve(ln, Config{
		World: w, Time: func() int64 { return 0 }, Token: "secret",
		Join: func(id Identity, emit func(byte, []byte)) (Remote, error) {
			ids <- id
			return rec, nil
		},
	})
	t.Cleanup(func() { ln.Close() })
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(30 * time.Second))
	proto.WriteJSON(c, proto.MsgHello, proto.Hello{
		Token: "secret", Gateway: "gw-java-776/0", Name: "EdgeZA", Edition: "java",
		UUID: "430803e4-3068-442f-9f47-e0fd6ee57e3c", Features: []string{proto.FeaturePlayerChat},
	})
	select {
	case id := <-ids:
		if !id.HasFeature(proto.FeaturePlayerChat) {
			t.Fatalf("features lost: %+v", id.Features)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Join never ran")
	}

	sig := make([]byte, 256)
	sig[0] = 7
	proto.WriteJSON(c, proto.MsgChatSession, proto.ChatSession{SessionID: [16]byte{1}, ExpiresAt: 99, Key: []byte{1}, KeySig: []byte{2}})
	proto.WriteJSON(c, proto.MsgChat, proto.Chat{Text: "signed hi", Signed: &proto.SignedChat{Index: 4, Signature: sig, Timestamp: 5, Salt: 6}})
	proto.WriteJSON(c, proto.MsgChat, proto.Chat{Text: "plain hi"})

	select {
	case s := <-rec.sessions:
		if s.SessionID != [16]byte{1} || s.ExpiresAt != 99 {
			t.Fatalf("session %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the chat session never reached the engine")
	}
	select {
	case ch := <-rec.signed:
		if ch.Text != "signed hi" || ch.Signed == nil || ch.Signed.Index != 4 || len(ch.Signed.Signature) != 256 || ch.Signed.Signature[0] != 7 {
			t.Fatalf("signed chat %+v", ch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the signed chat never reached the engine")
	}
	select {
	case text := <-rec.chats:
		if text != "plain hi" {
			t.Fatalf("plain chat %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plain chat never reached the engine")
	}
}

// commandRemote records the commands that reach the engine.
type commandRemote struct {
	mockRemote
	plain  chan string
	signed chan proto.Command
}

func (r *commandRemote) Command(cmd string)             { r.plain <- cmd }
func (r *commandRemote) SignedCommand(cm proto.Command) { r.signed <- cm }

// A command with signed message arguments reaches the engine with them;
// one without goes the plain way.
func TestSignedCommandReachesTheEngine(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rec := &commandRemote{plain: make(chan string, 2), signed: make(chan proto.Command, 2)}
	w := world.New(1)
	go Serve(ln, Config{
		World: w, Time: func() int64 { return 0 }, Token: "secret",
		Join: func(id Identity, emit func(byte, []byte)) (Remote, error) { return rec, nil },
	})
	t.Cleanup(func() { ln.Close() })
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(30 * time.Second))
	proto.WriteJSON(c, proto.MsgHello, proto.Hello{
		Token: "secret", Gateway: "gw-java-776/0", Name: "EdgeZA", Edition: "java",
		UUID: "430803e4-3068-442f-9f47-e0fd6ee57e3c", Features: []string{proto.FeaturePlayerChat},
	})
	sig := make([]byte, 256)
	sig[1] = 9
	proto.WriteJSON(c, proto.MsgCommand, proto.Command{Cmd: "say hi there", Signed: []proto.SignedArgument{
		{Name: "message", Content: "hi there", Chat: proto.SignedChat{Index: 2, Signature: sig, Timestamp: 3, Salt: 4}}}})
	proto.WriteJSON(c, proto.MsgCommand, proto.Command{Cmd: "list"})
	select {
	case cm := <-rec.signed:
		if cm.Cmd != "say hi there" || len(cm.Signed) != 1 || cm.Signed[0].Name != "message" || cm.Signed[0].Content != "hi there" ||
			cm.Signed[0].Chat.Index != 2 || len(cm.Signed[0].Chat.Signature) != 256 || cm.Signed[0].Chat.Signature[1] != 9 {
			t.Fatalf("signed command %+v", cm)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the signed command never reached the engine")
	}
	select {
	case cmd := <-rec.plain:
		if cmd != "list" {
			t.Fatalf("plain command %q", cmd)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the plain command never reached the engine")
	}
}
