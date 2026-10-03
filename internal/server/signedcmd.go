package server

// signedcmd.go is the world's half of signed command arguments: /say, /me,
// /msg (/tell, /w) and /teammsg (/tm) take a minecraft:message argument,
// which a secure-chat client signs as a chat message. The gateway checks
// those signatures against the player's message chain (vanilla
// ServerGamePacketListenerImpl.collectSignedArguments) and forwards them
// with the command (attach Command.Signed); here a message argument they
// name resolves to that signed message (MessageArgument.resolveChatMessage)
// and goes out as player chat bound to the command's chat type
// (PlayerList.broadcastChatMessage, MsgCommand.sendMessage,
// TeamMsgCommand.sendMessage). A recipient whose gateway does not render
// player chat, and every unsigned argument, keep the system lines these
// commands always sent — vanilla's unsigned argument is
// PlayerChatMessage.system, which goes out profileless rather than as
// player chat.

import (
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The chat types the message commands bind (ChatType's keys).
const (
	chatTypeSay         = "minecraft:say_command"
	chatTypeEmote       = "minecraft:emote_command"
	chatTypeMsgIn       = "minecraft:msg_command_incoming"
	chatTypeMsgOut      = "minecraft:msg_command_outgoing"
	chatTypeTeamMsgIn   = "minecraft:team_msg_command_incoming"
	chatTypeTeamMsgOut  = "minecraft:team_msg_command_outgoing"
	maxMessageArgLength = 256 // MessageArgument.Message.parseText's limit
)

// signedCmd is the command line a player's gateway forwarded with signed
// message arguments, held on the player while the dispatcher runs it
// (CommandSourceStack.getSigningContext).
type signedCmd struct {
	line string
	args []attachproto.SignedArgument
}

// SignedCommand is the remote half of a command carrying signed message
// arguments.
func (r *remotePlayer) SignedCommand(cm attachproto.Command) {
	r.p.touch()
	r.s.handleCommandSigned(r.p, cm.Cmd, cm.Signed)
}

// handleCommandSigned runs a command with its signing context: the message
// commands read their signed argument from it while the line dispatches.
func (s *Server) handleCommandSigned(p *player, cmd string, signed []attachproto.SignedArgument) {
	if len(signed) > 0 {
		p.cmdSigned.Store(&signedCmd{line: cmd, args: signed})
		defer p.cmdSigned.Store(nil)
	}
	s.handleCommand(p, cmd)
}

// signedArgument is CommandSigningContext.getArgument: the signed message
// for the named argument, when the text the dispatcher read for it is the
// content that was signed (a plugin that rewrote the line leaves the
// argument unsigned). nil = unsigned.
func (p *player) signedArgument(name, text string) *attachproto.SignedArgument {
	sc := p.cmdSigned.Load()
	if sc == nil || p.exec != nil {
		return nil
	}
	for i := range sc.args {
		a := &sc.args[i]
		if a.Name != name || a.Content == "" || !strings.HasSuffix(sc.line, a.Content) {
			continue
		}
		if strings.Join(commandFields(a.Content), " ") == text {
			return a
		}
	}
	return nil
}

// messageSource reports whether a command's source is a player in its own
// right — not the console, not an /execute stand-in — the only source a
// signed message can come from.
func messageSource(p *player) bool {
	return p != nil && p.exec == nil && p.name != consoleName
}

// signedPlayerChat is the signed argument as the PlayerChat a recipient is
// sent: the signature, link index and signed body as the gateway checked
// them, the signed content, the resolved text when it differs
// (withUnsignedContent) and the bound chat type.
func signedPlayerChat(from *player, a *attachproto.SignedArgument, resolved, chatType, target string) attachproto.PlayerChat {
	pc := attachproto.PlayerChat{
		Sender: from.uuid, SenderName: from.name,
		Index: a.Chat.Index, Signature: a.Chat.Signature,
		Content: a.Content, Timestamp: a.Chat.Timestamp, Salt: a.Chat.Salt, LastSeen: a.Chat.LastSeen,
		ChatType: chatType, Target: target,
	}
	if resolved != a.Content {
		pc.Unsigned = resolved
	}
	return pc
}

// messageContent is the argument's text: the signed content as the client
// typed it, or the text the dispatcher read.
func messageContent(text string, a *attachproto.SignedArgument) string {
	if a != nil {
		return a.Content
	}
	return text
}

// resolveMessageSelectors is MessageArgument.Message.toComponent: each
// selector in the text (@p, @a, @r, @s, @e, @n, with or without
// [arguments]) becomes the names it picks, joined by ", "
// (EntitySelector.joinNames), when the source may use selectors
// (commands/entity_selectors: gamemasters). An '@' that starts no selector
// stays as it is.
func (h *hub) resolveMessageSelectors(players map[int32]*tracked, by int32, text string, allowed bool) string {
	if !allowed || !strings.Contains(text, "@") {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '@' || i+1 >= len(text) || !strings.ContainsRune("parsen", rune(text[i+1])) {
			b.WriteByte(text[i])
			i++
			continue
		}
		end := i + 2
		if end < len(text) && text[end] == '[' {
			c := selectorClose(text, end)
			if c < 0 { // an unclosed selector: read the rest as written
				b.WriteString(text[i:])
				break
			}
			end = c + 1
		}
		b.WriteString(strings.Join(h.selectorNames(players, by, text[i:end]), ", "))
		i = end
	}
	return b.String()
}

// selectorClose is the index of the ']' closing the selector arguments that
// open at s[open], skipping quoted strings and nested brackets; -1 if none.
func selectorClose(s string, open int) int {
	depth := 0
	var quote byte
	for j := open; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == '\\' {
				j++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 && c == ']' {
				return j
			}
		}
	}
	return -1
}

// ---- /say and /me ---------------------------------------------------------------

// evCmdChat is a /say or /me: the source, its display name, the message
// argument as the dispatcher read it, and its signature if it was signed.
type evCmdChat struct {
	from      *player
	emote     bool // /me (EMOTE_COMMAND); /say otherwise (SAY_COMMAND)
	name      string
	text      string
	signed    *attachproto.SignedArgument
	selectors bool
}

func (evCmdChat) isHubEvent() {}

// postCmdChat queues a /say or /me with its signing context.
func (s *Server) postCmdChat(p *player, emote bool, arg, text string) {
	if n := utf8.RuneCountInString(text); n > maxMessageArgLength {
		p.tell(fmt.Sprintf("Chat message was too long (%d > maximum %d characters)", n, maxMessageArgLength))
		return
	}
	s.hub.post(evCmdChat{from: p, emote: emote, name: sourceName(p), text: text,
		signed: p.signedArgument(arg, text), selectors: s.isOp(p.name)})
	setCmdResult(p, 1)
}

// onCmdChat is SayCommand / EmoteCommands: the message resolved, then
// PlayerList.broadcastChatMessage with the bound chat type. A signed
// message reaches every player-chat session as player chat; everyone else
// (and every unsigned message) gets the decorated line.
func (h *hub) onCmdChat(players map[int32]*tracked, e evCmdChat) {
	var by int32
	if e.from != nil {
		by = e.from.eid
	}
	content := messageContent(e.text, e.signed)
	resolved := h.resolveMessageSelectors(players, by, content, e.selectors)
	line, chatType := fmt.Sprintf("[%s] %s", e.name, resolved), chatTypeSay
	if e.emote {
		line, chatType = fmt.Sprintf("* %s %s", e.name, resolved), chatTypeEmote
	}
	if e.signed == nil || !messageSource(e.from) {
		h.roomChat(players, line)
		return
	}
	pc := signedPlayerChat(e.from, e.signed, resolved, chatType, "")
	sys := chatEv(line)
	for _, t := range players {
		if t.p.exec != nil {
			continue
		}
		if t.p.playerChat {
			t.p.trySendEv(pc)
		} else {
			t.p.trySendEv(sys)
		}
	}
	log.Printf("chat: %s", line)
	h.npcsHear(line)
	h.bus.publish("chat", map[string]any{"text": line})
}
