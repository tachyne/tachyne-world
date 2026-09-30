package server

// securechat.go is the world's half of vanilla secure chat: what PlayerList
// does with it. The Java gateway validates each player's chat session and
// decodes their messages through the message chain (the per-connection half,
// ServerGamePacketListenerImpl's); the world keeps each player's session so
// every client can verify that player's messages (player_info_update
// INITIALIZE_CHAT), and relays a signed message to everyone with its
// signature intact (PlayerList.broadcastChatMessage, ChatType.CHAT bound to
// the sender).

import (
	"fmt"
	"log"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// evChatSession: a player's gateway validated a new chat session
// (ServerGamePacketListenerImpl.resetPlayerChatState).
type evChatSession struct {
	p *player
	s attachproto.ChatSession
}

func (evChatSession) isHubEvent() {}

// setChatSession is resetPlayerChatState's tail: the player's session is set
// and everyone is told (broadcastAll of INITIALIZE_CHAT).
func (h *hub) setChatSession(players map[int32]*tracked, e evChatSession) {
	s := e.s
	e.p.chatSession.Store(&s)
	upd := attachproto.PlayerInfoChat{UUID: e.p.uuid, Session: &s}
	for _, t := range players {
		if t.p.playerChat {
			t.p.trySendEv(upd)
		}
	}
}

// roomChatSigned is PlayerList.broadcastChatMessage for a signed message:
// every player whose gateway renders player_chat is sent the message with
// its signature, link index and signed body; content is what was signed and
// msg what the plugins made of it (withUnsignedContent: shown instead when
// it differs). Players on other gateways get the profileless relay, as for
// unsigned chat.
func (h *hub) roomChatSigned(players map[int32]*tracked, from *player, content, msg string, s *attachproto.SignedChat) {
	pc := attachproto.PlayerChat{
		Sender: from.uuid, SenderName: from.name,
		Index: s.Index, Signature: s.Signature,
		Content: content, Timestamp: s.Timestamp, Salt: s.Salt, LastSeen: s.LastSeen,
	}
	if msg != content {
		pc.Unsigned = msg
	}
	relay := attachproto.Chat{Text: msg, Sender: from.name}
	for _, t := range players {
		if t.p.playerChat {
			t.p.trySendEv(pc)
		} else {
			t.p.trySendEv(relay)
		}
	}
	attributed := fmt.Sprintf("<%s> %s", from.name, msg)
	log.Printf("chat: %s", attributed)
	h.npcsHear(attributed)
	h.bus.publish("chat", map[string]any{"text": attributed, "sender": from.name, "message": msg})
}

// SignedChat is the remote half of a signed message: the hub runs the plugin
// chat event, then relays it with the signature (roomChatSigned).
func (r *remotePlayer) SignedChat(ch attachproto.Chat) {
	r.p.touch() // tryHandleChat: typing counts as activity
	r.s.hub.post(evChat{from: r.p, text: ch.Text, signed: ch.Signed})
}

// ChatSession takes the session the player's gateway validated.
func (r *remotePlayer) ChatSession(s attachproto.ChatSession) {
	r.s.hub.post(evChatSession{p: r.p, s: s})
}
