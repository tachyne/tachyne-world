package server

import (
	"fmt"
	"strconv"
	"strings"
)

// SNBT, the text form of NBT that command arguments take (TagParser): a
// compound {key: value, …}, a list [a, b], a typed array [B; …], [I; …] or
// [L; …], a quoted string ("…" or '…' with backslash escapes), a number with
// an optional type suffix (b, s, l, f, d; a decimal point makes a double),
// true and false, or a bare word, which is a string.
//
// Values come back as Go values: map[string]any, []any, string, bool, int64
// (every integer type) and float64 (float and double).

// parseSNBT parses one SNBT value that must fill the whole of s.
func parseSNBT(s string) (any, error) {
	p := &snbtParser{s: s}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, p.errf("Trailing data found")
	}
	return v, nil
}

// parseSNBTPrefix parses one SNBT value at the start of s and reports how
// much of s it took.
func parseSNBTPrefix(s string) (any, int, error) {
	p := &snbtParser{s: s}
	v, err := p.value()
	return v, p.i, err
}

type snbtParser struct {
	s string
	i int
}

func (p *snbtParser) errf(msg string) error {
	return fmt.Errorf("%s at position %d: %s<--[HERE]", msg, p.i, p.s[:min(p.i, len(p.s))])
}

func (p *snbtParser) ws() {
	for p.i < len(p.s) && strings.IndexByte(" \t\n\r", p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *snbtParser) peek() byte {
	p.ws()
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *snbtParser) expect(c byte) error {
	if p.peek() != c {
		return p.errf(fmt.Sprintf("Expected '%c'", c))
	}
	p.i++
	return nil
}

func snbtBareChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '-' || c == '.' || c == '+'
}

func (p *snbtParser) value() (any, error) {
	switch c := p.peek(); c {
	case '{':
		return p.compound()
	case '[':
		return p.list()
	case '"', '\'':
		return p.quoted()
	case 0:
		return nil, p.errf("Expected value")
	}
	start := p.i
	for p.i < len(p.s) && snbtBareChar(p.s[p.i]) {
		p.i++
	}
	word := p.s[start:p.i]
	if word == "" {
		return nil, p.errf("Expected value")
	}
	return snbtScalar(word), nil
}

// snbtScalar types a bare word: a boolean, a number or a string.
func snbtScalar(w string) any {
	switch strings.ToLower(w) {
	case "true":
		return true
	case "false":
		return false
	}
	body, suffix := w, byte(0)
	if last := w[len(w)-1]; strings.IndexByte("bBsSlLfFdD", last) >= 0 && len(w) > 1 {
		body, suffix = w[:len(w)-1], last|0x20
	}
	switch suffix {
	case 'f', 'd':
		if v, err := strconv.ParseFloat(body, 64); err == nil {
			return v
		}
	case 'b', 's', 'l':
		if v, err := strconv.ParseInt(body, 10, 64); err == nil {
			return v
		}
	default:
		if v, err := strconv.ParseInt(w, 10, 64); err == nil {
			return v
		}
		if strings.ContainsAny(w, ".eE") {
			if v, err := strconv.ParseFloat(w, 64); err == nil {
				return v
			}
		}
	}
	return w
}

func (p *snbtParser) quoted() (string, error) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == '\\' && p.i < len(p.s):
			n := p.s[p.i]
			p.i++
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(n)
			}
		case c == q:
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
	}
	return "", p.errf("Unclosed quoted string")
}

func (p *snbtParser) compound() (map[string]any, error) {
	p.i++ // {
	out := map[string]any{}
	if p.peek() == '}' {
		p.i++
		return out, nil
	}
	for {
		var key string
		switch c := p.peek(); {
		case c == '"' || c == '\'':
			k, err := p.quoted()
			if err != nil {
				return nil, err
			}
			key = k
		default:
			start := p.i
			for p.i < len(p.s) && (snbtBareChar(p.s[p.i]) || p.s[p.i] == ':' && p.i+1 < len(p.s) && snbtBareChar(p.s[p.i+1]) && p.nsKey(start)) {
				p.i++
			}
			key = p.s[start:p.i]
			if key == "" {
				return nil, p.errf("Expected key")
			}
		}
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out[key] = v
		switch p.peek() {
		case ',':
			p.i++
		case '}':
			p.i++
			return out, nil
		default:
			return nil, p.errf("Expected '}'")
		}
	}
}

// nsKey reports whether the key starting at start is a namespaced id
// (minecraft:sharpness) rather than a key followed by its value's colon: a
// colon inside a bare key is taken when another colon follows it before the
// next separator.
func (p *snbtParser) nsKey(start int) bool {
	for j := p.i + 1; j < len(p.s); j++ {
		switch c := p.s[j]; {
		case c == ':':
			return true
		case !snbtBareChar(c):
			return false
		}
	}
	return false
}

func (p *snbtParser) list() (any, error) {
	p.i++ // [
	// A typed array: [B; …], [I; …], [L; …].
	if p.i+1 < len(p.s) && strings.IndexByte("BIL", p.s[p.i]) >= 0 && p.s[p.i+1] == ';' {
		p.i += 2
	}
	out := []any{}
	if p.peek() == ']' {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		switch p.peek() {
		case ',':
			p.i++
		case ']':
			p.i++
			return out, nil
		default:
			return nil, p.errf("Expected ']'")
		}
	}
}

// snbtInt reads a whole number from a parsed value.
func snbtInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		if n == float64(int64(n)) {
			return int64(n), true
		}
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}
