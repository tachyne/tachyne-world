package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Three small commands: /random (RandomCommand's value and roll — anyone may
// use them; the named random sequences are not kept), /swing (SwingCommand)
// and /teammsg, alias /tm (TeamMsgCommand).

// ---- /random ------------------------------------------------------------------

type evRandomCmd struct {
	by       int32
	name     string
	min, max int
	announce bool // roll: everyone hears it
}

func (evRandomCmd) isHubEvent() {}

// parseIntRange is MinMaxBounds.Ints: "a..b", "a..", "..b" or "a". A missing
// end is open (the int range's own limit).
func parseIntRange(s string) (lo, hi int, msg string) {
	lo, hi = math.MinInt32, math.MaxInt32
	a, b, ranged := strings.Cut(s, "..")
	if !ranged {
		b = a
	}
	if a == "" && b == "" {
		return 0, 0, "Expected value or range of values"
	}
	parse := func(v string, into *int) string {
		if v == "" {
			return ""
		}
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			if _, ferr := strconv.ParseFloat(v, 64); ferr == nil {
				return "Only whole numbers are allowed; not decimals"
			}
			return fmt.Sprintf("Invalid integer '%s'", v)
		}
		*into = int(n)
		return ""
	}
	if m := parse(a, &lo); m != "" {
		return 0, 0, m
	}
	if m := parse(b, &hi); m != "" {
		return 0, 0, m
	}
	if lo > hi {
		return 0, 0, "Min cannot be bigger than max"
	}
	return lo, hi, ""
}

func (s *Server) cmdRandom(p *player, args []string) {
	if len(args) < 2 || (args[0] != "value" && args[0] != "roll") {
		p.tell("Usage: /random value|roll <range>")
		return
	}
	if len(args) > 2 { // <sequence>: a gamemaster's, and not kept here
		p.tell("Named random sequences are not supported: /random value|roll <range>")
		return
	}
	lo, hi, msg := parseIntRange(args[1])
	if msg != "" {
		p.tell(msg)
		return
	}
	switch span := int64(hi) - int64(lo); {
	case span == 0:
		p.tell("The range of the random value must be at least 1")
		return
	case span >= math.MaxInt32:
		p.tell("The range of the random value must be at most 2147483647")
		return
	}
	s.hub.post(evRandomCmd{by: p.eid, name: p.name, min: lo, max: hi, announce: args[0] == "roll"})
}

// applyRandomCommand draws on the hub's random source (randomBetweenInclusive).
func (h *hub) applyRandomCommand(players map[int32]*tracked, e evRandomCmd) int {
	v := e.min + h.rng.Intn(e.max-e.min+1)
	if e.announce {
		h.broadcastChat(players, fmt.Sprintf("%s rolled %d (from %d to %d)", e.name, v, e.min, e.max))
	} else {
		h.cmdInfo(players, e.by)(fmt.Sprintf("Randomized value: %d", v))
	}
	return v
}

// ---- /swing -------------------------------------------------------------------

type evSwingCmd struct {
	by     int32
	target string // "@s" when none was named
	hand   int32  // 0 main, 1 off
}

func (evSwingCmd) isHubEvent() {}

func (s *Server) cmdSwing(p *player, args []string) {
	if !s.isOp(p.name) { // SwingCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	e := evSwingCmd{by: p.eid, target: "@s"}
	switch len(args) {
	case 0:
	case 1, 2:
		e.target = args[0]
		if len(args) == 2 {
			switch args[1] {
			case "mainhand":
			case "offhand":
				e.hand = 1
			default:
				p.tell("Usage: /swing [<targets> [mainhand|offhand]]")
				return
			}
		}
	default:
		p.tell("Usage: /swing [<targets> [mainhand|offhand]]")
		return
	}
	s.hub.post(e)
}

// applySwingCommand is LivingEntity.swing(hand, updateSelf=true) for each
// target: everyone watching sees the arm go, a player's own client included.
func (h *hub) applySwingCommand(players map[int32]*tracked, e evSwingCmd) {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // sendSuccess(…, true)
	ens := h.commandEntities(players, e.by, e.target)
	if len(ens) == 0 {
		tell("No living entities were found to swing")
		return
	}
	for _, en := range ens {
		sw := attachproto.Swing{EID: en.eid(), Hand: e.hand}
		if t := en.t; t != nil {
			t.p.trySendEv(sw)
			h.toOthersNear(players, t.p.eid, t.dim, t.x, t.z, sw)
		} else {
			h.toTracking(players, en.m.eid, en.m.dim, en.m.x, en.m.z, sw)
		}
	}
	if len(ens) == 1 {
		okTell("Made " + ens[0].name() + " swing an arm")
	} else {
		okTell(fmt.Sprintf("Made %d entities swing their arms", len(ens)))
	}
}

// ---- /teammsg -----------------------------------------------------------------

type evTeamMsg struct {
	from int32
	text string
}

func (evTeamMsg) isHubEvent() {}

func (s *Server) cmdTeamMsg(p *player, args []string) {
	if len(args) == 0 {
		p.tell("Usage: /teammsg <message>")
		return
	}
	s.hub.post(evTeamMsg{from: p.eid, text: strings.Join(args, " ")})
}

// applyTeamMsg sends the line to the sender's team: "[Team] [name] text" to
// the others, "-> [Team] [name] text" back to the sender (chat.type.team.text and
// .sent). The name is bracketed as the room's chat lines are, not in vanilla's
// <name>: a system line shaped "<name> …" is what the client's secure-chat
// check hides from everyone but the sender on an offline-mode server.
func (h *hub) applyTeamMsg(players map[int32]*tracked, e evTeamMsg) {
	from := players[e.from]
	if from == nil {
		return
	}
	team := h.teamOf(from.p.name)
	if team == "" {
		from.p.trySendEv(chatEv("You must be on a team to message your team"))
		return
	}
	title := team
	if t := h.sb.Teams[team]; t != nil && t.Title != "" {
		title = t.Title
	}
	for _, t := range players {
		switch {
		case t == from:
			t.p.trySendEv(chatEv(fmt.Sprintf("-> [%s] [%s] %s", title, from.p.name, e.text)))
		case h.teamOf(t.p.name) == team:
			t.p.trySendEv(chatEv(fmt.Sprintf("[%s] [%s] %s", title, from.p.name, e.text)))
		}
	}
}
