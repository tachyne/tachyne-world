package server

import (
	"encoding/json"
	"regexp"
	"strings"
)

// A text-component argument (ComponentArgument), for the commands whose
// component the engine can show faithfully as legacy-formatted text: plain
// text with a named colour and the five formats, nested through "extra" or
// a list. That is everything a boss bar's title can show. Components that
// need the client to resolve something — translate, score, selector,
// keybind, nbt, object — and hex colours or fonts are refused rather than
// shown wrong. Click, hover and insertion are accepted and have nothing to
// show on a title.
//
// The argument is SNBT as vanilla parses it: a quoted string, a bare word,
// a compound ({text:"hi",color:red}) or a list; strict JSON is a subset.

var legacyColour = map[string]string{
	"black": "0", "dark_blue": "1", "dark_green": "2", "dark_aqua": "3",
	"dark_red": "4", "dark_purple": "5", "gold": "6", "gray": "7",
	"dark_gray": "8", "blue": "9", "green": "a", "aqua": "b",
	"red": "c", "light_purple": "d", "yellow": "e", "white": "f",
}

var legacyFormat = []struct{ key, code string }{
	{"obfuscated", "k"}, {"bold", "l"}, {"strikethrough", "m"}, {"underlined", "n"}, {"italic", "o"},
}

// compStyle is the inherited style while flattening.
type compStyle struct {
	colour string
	fmt    map[string]bool
}

func (s compStyle) prefix() string {
	var b strings.Builder
	if s.colour != "" {
		b.WriteString("§" + s.colour)
	}
	for _, f := range legacyFormat {
		if s.fmt[f.key] {
			b.WriteString("§" + f.code)
		}
	}
	return b.String()
}

var (
	snbtBareKey   = regexp.MustCompile(`([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)\s*:`)
	snbtBareValue = regexp.MustCompile(`(:\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*[,}\]])`)
	snbtBareBool  = regexp.MustCompile(`^(true|false)$`)
	snbtByteBool  = regexp.MustCompile(`(:\s*)([01])b(\s*[,}\]])`)
)

// parseTextComponent reads a component argument into legacy-formatted
// text. ok is false for input that is not a component; a non-empty why is a
// component this subset cannot show.
func parseTextComponent(arg string) (text string, why string, ok bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", false
	}
	v, ok := componentValue(arg)
	if !ok {
		return "", "", false
	}
	var b strings.Builder
	styled := false
	var walk func(n any, st compStyle) string
	walk = func(n any, st compStyle) string {
		switch c := n.(type) {
		case string:
			if st.colour != "" || len(st.fmt) > 0 {
				styled = true
				b.WriteString("§r" + st.prefix())
			} else if styled {
				b.WriteString("§r")
			}
			b.WriteString(c)
		case float64, bool:
			raw, _ := json.Marshal(c)
			return walk(string(raw), st)
		case []any:
			if len(c) == 0 {
				return "an empty list is not a component"
			}
			// A list is its first element with the rest as its extra: the
			// rest inherit the first one's style.
			first := st
			if m, ok := c[0].(map[string]any); ok {
				first = mergeStyle(st, m)
			}
			for i, e := range c {
				s := st
				if i > 0 {
					s = first
				}
				if why := walk(e, s); why != "" {
					return why
				}
			}
		case map[string]any:
			for k := range c {
				switch k {
				case "text", "type", "color", "extra", "bold", "italic", "underlined",
					"strikethrough", "obfuscated", "click_event", "hover_event", "clickEvent",
					"hoverEvent", "insertion", "shadow_color":
				default:
					return "The " + k + " component is not supported here"
				}
			}
			if ty, ok := c["type"].(string); ok && ty != "text" {
				return "The " + ty + " component is not supported here"
			}
			if col, ok := c["color"].(string); ok {
				if _, named := legacyColour[col]; !named {
					return "The colour " + col + " is not supported here"
				}
			}
			own := mergeStyle(st, c)
			t, _ := c["text"].(string)
			if why := walk(t, own); why != "" {
				return why
			}
			if ex, ok := c["extra"].([]any); ok {
				for _, e := range ex {
					if why := walk(e, own); why != "" {
						return why
					}
				}
			}
		default:
			return "not a component"
		}
		return ""
	}
	if why := walk(v, compStyle{}); why != "" {
		return "", why, true
	}
	return b.String(), "", true
}

func mergeStyle(st compStyle, c map[string]any) compStyle {
	out := compStyle{colour: st.colour, fmt: map[string]bool{}}
	for k, v := range st.fmt {
		out.fmt[k] = v
	}
	if col, ok := c["color"].(string); ok {
		out.colour = legacyColour[col]
	}
	for _, f := range legacyFormat {
		if v, ok := c[f.key].(bool); ok {
			if v {
				out.fmt[f.key] = true
			} else {
				delete(out.fmt, f.key)
			}
		}
	}
	return out
}

// componentValue reads a ComponentArgument — JSON, or the SNBT-ish form
// commands also accept, or a bare word — into its JSON value.
func componentValue(arg string) (any, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(arg), &v); err != nil {
		// SNBT: quote bare keys and bare-word values, 1b/0b booleans.
		norm := snbtBareKey.ReplaceAllString(arg, `$1"$2":`)
		for i := 0; i < 2; i++ { // overlapping matches need a second pass
			norm = snbtByteBool.ReplaceAllStringFunc(norm, func(m string) string {
				sub := snbtByteBool.FindStringSubmatch(m)
				b := "false"
				if sub[2] == "1" {
					b = "true"
				}
				return sub[1] + b + sub[3]
			})
			norm = snbtBareValue.ReplaceAllStringFunc(norm, func(m string) string {
				sub := snbtBareValue.FindStringSubmatch(m)
				if snbtBareBool.MatchString(sub[2]) {
					return m
				}
				return sub[1] + `"` + sub[2] + `"` + sub[3]
			})
		}
		norm = strings.ReplaceAll(norm, `'`, `"`)
		if err := json.Unmarshal([]byte(norm), &v); err != nil {
			if strings.ContainsAny(arg, " {}[]\"',:") {
				return nil, false
			}
			v = arg // a bare word is a string
		}
	}
	return v, true
}

// componentJSON is componentValue as JSON text.
func componentJSON(arg string) (json.RawMessage, bool) {
	v, ok := componentValue(arg)
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(v)
	return raw, err == nil
}
