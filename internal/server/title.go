package server

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// `/title <targets> <title|subtitle|actionbar|times|clear|reset> [args]` —
// vanilla's own shape. The big words across the middle of the screen were the
// one piece of the player-facing HUD the engine could not drive at all.

// cmdTitle is the command. Operators only, like the rest of the tools that
// write on somebody else's screen.
func (s *Server) cmdTitle(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	ev, complaint := parseTitle(p, args)
	if complaint != "" {
		p.tell(complaint)
		return
	}
	s.hub.post(ev)
}

// parseTitle is the whole of /title's argument handling, kept apart from the
// command so it can be exercised directly. A non-empty complaint is what the
// player is told and means nothing is posted.
func parseTitle(p *player, args []string) (evTitle, string) {
	const usage = "Usage: /title <player|@selector> <title|subtitle|actionbar|times|clear|reset> [text|ticks]"
	if len(args) < 2 {
		return evTitle{}, usage
	}
	target, verb := args[0], strings.ToLower(args[1])
	rest := strings.TrimSpace(strings.Join(args[2:], " "))
	ev := evTitle{by: p, target: target}
	switch verb {
	case "title", "subtitle", "actionbar":
		if rest == "" {
			return evTitle{}, "Say what to show: /title " + target + " " + verb + " <text>"
		}
		// TitleCommand takes a ComponentArgument: JSON (or its SNBT-ish
		// form), resolved for each viewer. Plain text stays for the
		// renderers that draw strings.
		raw, isComp := componentJSON(rest)
		text := rest
		if isComp {
			if t, _, ok := parseTextComponent(rest); ok {
				text = t
			}
		} else {
			return evTitle{}, "Invalid chat component: " + rest
		}
		switch verb {
		case "title":
			ev.t.Title, ev.t.TitleJSON = text, raw
		case "subtitle":
			ev.t.Subtitle, ev.t.SubtitleJSON = text, raw
		default:
			ev.actionBar, ev.actionJSON = text, raw
		}
	case "clear":
		ev.t.Clear = true
	case "reset":
		ev.t.Clear, ev.t.Reset = true, true
	case "times":
		f := strings.Fields(rest)
		if len(f) != 3 {
			return evTitle{}, "Usage: /title <targets> times <fade in> <stay> <fade out>  (in ticks)"
		}
		n := make([]int32, 3)
		for i, v := range f {
			x, err := strconv.Atoi(v)
			if err != nil || x < 0 {
				return evTitle{}, "The three times are whole numbers of ticks."
			}
			n[i] = int32(x)
		}
		ev.t.FadeIn, ev.t.Stay, ev.t.FadeOut = n[0], n[1], n[2]
	default:
		return evTitle{}, usage
	}
	return ev, ""
}

type evTitle struct {
	by     *player
	target string
	t      attachproto.Title
	// actionBar is the line above the hotbar, which is a chat message with a
	// flag rather than a title packet — vanilla puts it under /title all the
	// same, so this does too.
	actionBar  string
	actionJSON json.RawMessage
}

func (evTitle) isHubEvent() {}

func (h *hub) onTitle(players map[int32]*tracked, e evTitle) {
	hit := 0
	for _, t := range h.commandTargets(players, e.by.eid, e.target) {
		// ComponentUtils.updateForEntity: selectors and scores resolve for
		// each viewer.
		resolve := func(raw json.RawMessage) json.RawMessage {
			var tree any
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			if len(raw) == 0 || d.Decode(&tree) != nil {
				return raw
			}
			out, err := json.Marshal(h.resolveComponent(players, e.by.eid, t, tree, 0))
			if err != nil {
				return raw
			}
			return out
		}
		if e.actionBar != "" {
			t.p.trySendEv(attachproto.Chat{Text: e.actionBar, Component: resolve(e.actionJSON), ActionBar: true})
		} else {
			tt := e.t
			tt.TitleJSON, tt.SubtitleJSON = resolve(e.t.TitleJSON), resolve(e.t.SubtitleJSON)
			t.p.trySendEv(tt)
		}
		hit++
	}
	if hit == 0 {
		e.by.trySendEv(chatEv("No player matched " + e.target + "."))
	}
}
