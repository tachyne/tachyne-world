package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// signedCmdServer is three joined players for the message commands: alice
// (an operator) and bob on a Java gateway that renders player chat, carol
// on one that does not (Bedrock). Every event each is sent is recorded.
func signedCmdServer(t *testing.T) (*Server, *hub, map[string]*player, map[string]*anyEvLog) {
	t.Helper()
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative), Ops: map[string]bool{"alice": true}}
	h.isOp = s.isOp
	w.ForceLoad(0, 0, 2)
	startHub(t, h)
	ps, logs := map[string]*player{}, map[string]*anyEvLog{}
	for i, name := range []string{"alice", "bob", "carol"} {
		p := newPlayer(h.allocEID(), name, [16]byte{byte(i + 1)})
		p.playerChat = name != "carol"
		sy := w.SurfaceY(0, 0)
		p.x, p.y, p.z = 0.5, sy, 0.5
		logs[name] = recordAllEvents(t, p)
		h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
		waitJoined(t, h, name)
		ps[name] = p
	}
	return s, h, ps, logs
}

// signedArg is a signed message argument as the gateway forwards it.
func signedArg(name, content string, index int32) attachproto.SignedArgument {
	sig := make([]byte, 256)
	sig[0] = byte(index)
	return attachproto.SignedArgument{Name: name, Content: content,
		Chat: attachproto.SignedChat{Index: index, Signature: sig, Timestamp: 1000 + int64(index), Salt: 7}}
}

// mark posts a marker line and waits for every log to have it, so what was
// posted before it has been delivered.
func markAll(t *testing.T, h *hub, logs map[string]*anyEvLog, mark string) {
	t.Helper()
	h.post(evChat{text: mark})
	for name, l := range logs {
		waitAnyEv(t, l, name+"'s marker "+mark, func(ev any) bool {
			c, ok := ev.(attachproto.Chat)
			return ok && c.Text == mark
		})
	}
}

// playerChats is every PlayerChat a log holds.
func playerChats(l *anyEvLog) []attachproto.PlayerChat {
	var out []attachproto.PlayerChat
	for _, ev := range l.snapshot() {
		if pc, ok := ev.(attachproto.PlayerChat); ok {
			out = append(out, pc)
		}
	}
	return out
}

// chatLines is every system chat line a log holds.
func chatLines(l *anyEvLog) []string {
	var out []string
	for _, ev := range l.snapshot() {
		if c, ok := ev.(attachproto.Chat); ok && !c.ActionBar {
			out = append(out, c.Text)
		}
	}
	return out
}

// A signed /say and /me arrive through the remote entry point and go to
// every player-chat session as player chat bound to say_command and
// emote_command, signature intact; a session that does not render player
// chat gets the decorated line.
func TestSignedSayAndMeRelay(t *testing.T) {
	s, h, ps, logs := signedCmdServer(t)
	alice := &remotePlayer{s: s, p: ps["alice"]}
	alice.SignedCommand(attachproto.Command{Cmd: "say hello  world", Signed: []attachproto.SignedArgument{signedArg("message", "hello  world", 3)}})
	alice.SignedCommand(attachproto.Command{Cmd: "me waves", Signed: []attachproto.SignedArgument{signedArg("action", "waves", 4)}})
	markAll(t, h, logs, "M1")
	for _, name := range []string{"alice", "bob"} {
		pcs := playerChats(logs[name])
		if len(pcs) != 2 {
			t.Fatalf("%s got %d player chats: %+v", name, len(pcs), pcs)
		}
		say, me := pcs[0], pcs[1]
		if say.ChatType != chatTypeSay || say.Content != "hello  world" || say.Index != 3 || say.Sender != ps["alice"].uuid ||
			say.SenderName != "alice" || len(say.Signature) != 256 || say.Signature[0] != 3 || say.Timestamp != 1003 || say.Unsigned != "" || say.Target != "" {
			t.Errorf("%s's /say: %+v", name, say)
		}
		if me.ChatType != chatTypeEmote || me.Content != "waves" || me.Index != 4 {
			t.Errorf("%s's /me: %+v", name, me)
		}
		for _, l := range chatLines(logs[name]) {
			if l == "[alice] hello  world" || l == "[alice] hello world" || l == "* alice waves" {
				t.Errorf("%s was sent the system line too: %q", name, l)
			}
		}
	}
	c := chatLines(logs["carol"])
	if !hasLine(c, "[alice] hello  world") || !hasLine(c, "* alice waves") {
		t.Errorf("carol's lines: %q", c)
	}
	if pcs := playerChats(logs["carol"]); len(pcs) != 0 {
		t.Errorf("carol's gateway was sent player chat: %+v", pcs)
	}
}

// An unsigned argument — no signature, or one for other text than the
// dispatcher read (a rewritten line) — keeps the system line for everyone.
func TestUnsignedMessageCommandsStaySystemLines(t *testing.T) {
	s, h, ps, logs := signedCmdServer(t)
	s.handleCommand(ps["alice"], "say plain")
	s.handleCommandSigned(ps["alice"], "say other", []attachproto.SignedArgument{signedArg("message", "something else", 1)})
	s.handleCommandSigned(ps["bob"], "me shrugs", []attachproto.SignedArgument{signedArg("message", "shrugs", 2)}) // the wrong argument name
	markAll(t, h, logs, "M1")
	for name, l := range logs {
		if pcs := playerChats(l); len(pcs) != 0 {
			t.Errorf("%s was sent player chat for an unsigned argument: %+v", name, pcs)
		}
		lines := chatLines(l)
		if !hasLine(lines, "[alice] plain") || !hasLine(lines, "[alice] other") || !hasLine(lines, "* bob shrugs") {
			t.Errorf("%s's lines: %q", name, lines)
		}
	}
	if ps["alice"].cmdSigned.Load() != nil {
		t.Error("the signing context outlived its command")
	}
}

// Selectors in a message resolve for a source that may use them, and the
// resolved text rides as the unsigned (decorated) content beside the signed
// one; for a source that may not, the text stays as typed.
func TestMessageSelectorsResolve(t *testing.T) {
	s, h, ps, logs := signedCmdServer(t)
	s.handleCommandSigned(ps["alice"], "say hi @s", []attachproto.SignedArgument{signedArg("message", "hi @s", 5)})
	s.handleCommand(ps["bob"], "me greets @s and mail@x")
	markAll(t, h, logs, "M1")
	pcs := playerChats(logs["bob"])
	if len(pcs) != 1 || pcs[0].Content != "hi @s" || pcs[0].Unsigned != "hi alice" {
		t.Fatalf("bob got %+v", pcs)
	}
	if c := chatLines(logs["carol"]); !hasLine(c, "[alice] hi alice") || !hasLine(c, "* bob greets @s and mail@x") {
		t.Errorf("carol's lines: %q", c)
	}
	// The resolver alone: an '@' that starts no selector stays.
	onHub(t, h, func() {
		got := h.resolveMessageSelectors(h.playersRef, ps["alice"].eid, "a@b @s[name=alice] @z @", true)
		if got != "a@b alice @z @" {
			t.Errorf("resolved %q", got)
		}
	})
}

// A signed /msg: the sender sees msg_command_outgoing naming each target,
// each target hears msg_command_incoming; a target on a gateway without
// player chat gets the whisper line, and the sender still gets player chat.
func TestSignedMsgRelay(t *testing.T) {
	s, h, ps, logs := signedCmdServer(t)
	alice := &remotePlayer{s: s, p: ps["alice"]}
	alice.SignedCommand(attachproto.Command{Cmd: "msg @a[name=!alice] psst", Signed: []attachproto.SignedArgument{signedArg("message", "psst", 6)}})
	markAll(t, h, logs, "M1")
	out := playerChats(logs["alice"])
	if len(out) != 2 {
		t.Fatalf("alice got %+v", out)
	}
	targets := map[string]bool{}
	for _, pc := range out {
		if pc.ChatType != chatTypeMsgOut || pc.Content != "psst" || pc.Index != 6 || pc.Sender != ps["alice"].uuid {
			t.Errorf("alice's outgoing: %+v", pc)
		}
		targets[pc.Target] = true
	}
	if !targets["bob"] || !targets["carol"] {
		t.Errorf("outgoing targets %v", targets)
	}
	in := playerChats(logs["bob"])
	if len(in) != 1 || in[0].ChatType != chatTypeMsgIn || in[0].Target != "" || in[0].Content != "psst" || in[0].SenderName != "alice" {
		t.Errorf("bob's incoming: %+v", in)
	}
	if c := chatLines(logs["carol"]); !hasLine(c, "alice whispers to you: psst") {
		t.Errorf("carol's lines: %q", c)
	}
	if pcs := playerChats(logs["carol"]); len(pcs) != 0 {
		t.Errorf("carol's gateway was sent player chat: %+v", pcs)
	}
}

// A signed /teammsg: team_msg_command_outgoing to the sender and
// team_msg_command_incoming to the team, both naming the team; nobody off
// the team hears it.
func TestSignedTeamMsgRelay(t *testing.T) {
	s, h, ps, logs := signedCmdServer(t)
	onHub(t, h, func() {
		h.sb.Teams["red"] = &sbTeam{Title: "Reds", Members: map[string]bool{"alice": true, "bob": true}}
	})
	alice := &remotePlayer{s: s, p: ps["alice"]}
	alice.SignedCommand(attachproto.Command{Cmd: "tm go left", Signed: []attachproto.SignedArgument{signedArg("message", "go left", 8)}})
	markAll(t, h, logs, "M1")
	a, b := playerChats(logs["alice"]), playerChats(logs["bob"])
	if len(a) != 1 || a[0].ChatType != chatTypeTeamMsgOut || a[0].Target != "Reds" || a[0].Content != "go left" || a[0].Index != 8 {
		t.Errorf("alice: %+v", a)
	}
	if len(b) != 1 || b[0].ChatType != chatTypeTeamMsgIn || b[0].Target != "Reds" || b[0].Content != "go left" {
		t.Errorf("bob: %+v", b)
	}
	if pcs := playerChats(logs["carol"]); len(pcs) != 0 {
		t.Errorf("carol is off the team: %+v", pcs)
	}
	for _, l := range chatLines(logs["carol"]) {
		if l == "[Reds] [alice] go left" {
			t.Errorf("carol is off the team and heard it")
		}
	}
}

// The tree gives every message command a minecraft:message argument, the
// one the client signs, and /msg's targets take any number of players.
func TestCommandTreeMessageArguments(t *testing.T) {
	nodes, root := decodeCommandTree(t, buildCommandTree())
	child := func(of int32, name string) int32 {
		for _, k := range nodes[of].kids {
			if nodes[k].name == name {
				return k
			}
		}
		t.Fatalf("no %q under %q", name, nodes[of].name)
		return -1
	}
	for _, path := range [][]string{
		{"say", "message"}, {"me", "action"}, {"teammsg", "message"}, {"tm", "message"},
		{"msg", "targets", "message"}, {"tell", "targets", "message"}, {"w", "targets", "message"},
	} {
		cur := root
		for _, name := range path {
			cur = child(cur, name)
		}
		if n := nodes[cur]; n.parser != parserMessage || n.flags&0x04 == 0 || n.flags&0x10 != 0 {
			t.Errorf("%v: parser %d flags %#x", path, n.parser, n.flags)
		}
	}
	for _, n := range modelledCommands() {
		if n.lit != "msg" {
			continue
		}
		if tg := n.children[0]; tg.parser != parserEntity || len(tg.props) != 1 || tg.props[0] != entityPlayers {
			t.Errorf("msg targets: parser %d props %v", tg.parser, tg.props)
		}
	}
}
