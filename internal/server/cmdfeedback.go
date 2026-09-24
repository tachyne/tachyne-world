package server

import "log"

// Command feedback, as vanilla's CommandSourceStack.sendSuccess and
// broadcastToAdmins give it.
//
// A command's SUCCESS line reaches the player who ran it only while the
// send_command_feedback rule is on (a player source accepts success only
// then). When the command changed something (sendSuccess with broadcast),
// the other online operators are told too — "[Name: message]" in gray
// italics — again only while send_command_feedback is on, and the server
// log records the same line while log_admin_commands is on. A FAILURE is
// never suppressed: those stay plain tells.

// evCmdSuccess carries a success line from a session-side command to the
// hub, which owns the rules and knows who else is an operator.
type evCmdSuccess struct {
	p         *player
	text      string
	broadcast bool
}

func (evCmdSuccess) isHubEvent() {}

// adminLine is chat.type.admin, "[%s: %s]", styled gray and italic.
func adminLine(name, text string) string { return "§7§o[" + name + ": " + text + "]" }

// cmdSuccess is sendSuccess for a command the player caller ran.
func (h *hub) cmdSuccess(players map[int32]*tracked, caller *player, text string, broadcast bool) {
	if caller == nil {
		return
	}
	if h.rules.SendCommandFeedback {
		caller.trySendEv(chatEv(text))
	}
	if !broadcast {
		return
	}
	line := adminLine(caller.name, text)
	if h.rules.SendCommandFeedback && h.isOp != nil {
		for _, t := range players {
			if t.p != caller && h.isOp(t.p.name) {
				t.p.trySendEv(chatEv(line))
			}
		}
	}
	if h.rules.LogAdminCommands {
		log.Printf("[%s: %s]", caller.name, text)
	}
}

// cmdOK returns the success teller for a hub-side command: each line is
// sendSuccess(…, true), the form for a command that changed something.
func (h *hub) cmdOK(players map[int32]*tracked, by int32) func(string) {
	return func(msg string) {
		if t := players[by]; t != nil {
			h.cmdSuccess(players, t.p, msg, true)
		}
	}
}

// cmdInfo is cmdOK for an answer that changes nothing (a query, a list):
// sendSuccess(…, false), which only its caller sees.
func (h *hub) cmdInfo(players map[int32]*tracked, by int32) func(string) {
	return func(msg string) {
		if t := players[by]; t != nil {
			h.cmdSuccess(players, t.p, msg, false)
		}
	}
}

// ok is a session-side command's success line (broadcast to operators).
func (s *Server) ok(p *player, text string) {
	s.hub.post(evCmdSuccess{p: p, text: text, broadcast: true})
}

// info is a session-side command's answer that changes nothing.
func (s *Server) info(p *player, text string) {
	s.hub.post(evCmdSuccess{p: p, text: text})
}
