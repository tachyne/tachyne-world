package server

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /tellraw <targets> <message> (TellRawCommand): a JSON text component sent
// as a system message to each target, after ComponentUtils.updateForEntity
// has filled it in for them — a selector component becomes the names it
// selects (as the command's source sees them), a score component the value
// on the scoreboard ("*" is the player reading it). The gateway draws the
// component; the flattened text rides along for anything that cannot.

type evTellraw struct {
	by     *player
	target string
	raw    json.RawMessage
}

func (evTellraw) isHubEvent() {}

// cmdTellraw takes the raw line, so the JSON keeps its own spacing.
func (s *Server) cmdTellraw(p *player, line string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /tellraw <targets> <message>"
	rest := strings.TrimSpace(line)
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "tellraw"))
	target, msg := rest, ""
	if i := targetEnd(rest); i >= 0 {
		target, msg = rest[:i], strings.TrimSpace(rest[i:])
	}
	if target == "" || msg == "" {
		p.tell(usage)
		return
	}
	var probe any
	if json.Unmarshal([]byte(msg), &probe) != nil {
		p.tell("Invalid chat component: " + msg)
		return
	}
	s.hub.post(evTellraw{by: p, target: target, raw: json.RawMessage(msg)})
}

// targetEnd is where the target argument ends: a selector's brackets may
// hold spaces (@a[name=x, limit=1]), a name does not.
func targetEnd(s string) int {
	depth := 0
	for i, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case r == ' ' && depth <= 0:
			return i
		}
	}
	return -1
}

func (h *hub) onTellraw(players map[int32]*tracked, e evTellraw) {
	targets := h.commandTargets(players, e.by.eid, e.target)
	if len(targets) == 0 {
		e.by.tell("No player was found")
		return
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(e.raw))
	d.UseNumber()
	if d.Decode(&tree) != nil {
		return
	}
	for _, t := range targets {
		resolved := h.resolveComponent(players, e.by.eid, t, tree, 0)
		raw, err := json.Marshal(resolved)
		if err != nil {
			continue
		}
		t.p.sendEv(attachproto.Chat{Text: flattenComponent(resolved), Component: raw})
	}
}

// resolveComponent is updateForEntity: selector and score components are
// replaced by text, recursively through extra and with (depth-capped, as
// vanilla's 100).
func (h *hub) resolveComponent(players map[int32]*tracked, by int32, reader *tracked, v any, depth int) any {
	if depth > 100 {
		return v
	}
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = h.resolveComponent(players, by, reader, e, depth+1)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = val
		}
		if sel, ok := x["selector"].(string); ok {
			delete(out, "selector")
			delete(out, "separator")
			out["text"] = strings.Join(h.selectorNames(players, by, sel), ", ")
		}
		if sc, ok := x["score"].(map[string]any); ok {
			delete(out, "score")
			name, _ := sc["name"].(string)
			obj, _ := sc["objective"].(string)
			if name == "*" {
				name = reader.p.name
			}
			text := ""
			if v, ok := h.sb.Scores[name][obj]; ok {
				text = strconv.Itoa(int(v))
			}
			out["text"] = text
		}
		if _, ok := x["nbt"]; ok { // no NBT paths to read here: resolve to nothing
			delete(out, "nbt")
			delete(out, "block")
			delete(out, "entity")
			delete(out, "storage")
			delete(out, "interpret")
			out["text"] = ""
		}
		for _, k := range []string{"extra", "with"} {
			if l, ok := x[k].([]any); ok {
				out[k] = h.resolveComponent(players, by, reader, l, depth+1)
			}
		}
		return out
	}
	return v
}

// selectorNames are the names a selector picks, players then mobs.
func (h *hub) selectorNames(players map[int32]*tracked, by int32, sel string) []string {
	var names []string
	for _, t := range h.commandTargets(players, by, sel) {
		names = append(names, t.p.name)
	}
	for _, m := range h.commandMobs(players, by, sel) {
		names = append(names, mobDisplayName(m.etype))
	}
	return names
}

// flattenComponent is the component's plain text (getString): each text,
// translate key or keybind in reading order.
func flattenComponent(v any) string {
	var sb strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			sb.WriteString(x)
		case json.Number:
			sb.WriteString(x.String())
		case bool:
			sb.WriteString(strconv.FormatBool(x))
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			switch {
			case x["text"] != nil:
				walk(x["text"])
			case x["translate"] != nil:
				if fb, ok := x["fallback"].(string); ok {
					sb.WriteString(fb)
				} else {
					walk(x["translate"])
				}
			case x["keybind"] != nil:
				walk(x["keybind"])
			}
			if l, ok := x["extra"].([]any); ok {
				walk(l)
			}
		}
	}
	walk(v)
	return sb.String()
}
