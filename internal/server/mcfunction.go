package server

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Functions: a data pack's .mcfunction files, as CommandFunction.fromLines
// reads them. Each line is trimmed; a line ending in a backslash runs on
// into the next; blank lines and # comments are skipped; a line starting
// with $ is a macro line whose $(name) variables are filled from the
// compound the function is called with. A function is loaded or refused
// whole: one bad line and it is not there.
//
// Vanilla parses every command at load, at the function permission level
// (2). The dispatcher here parses as it runs, so the load checks what
// holds for every command — no leading slash, and no command whose root
// needs more than level 2 — and the rest surfaces when the line runs.

// maxCommandLine is CommandFunction.checkCommandLineLength's limit.
const maxCommandLine = 2000000

// functionPermission is function-permission-level: what functions are
// compiled and run at.
const functionPermission = permGamemasters

// mcFunction is one loaded function.
type mcFunction struct {
	id      string
	entries []fnEntry
	macro   bool
	params  []string // macro variables, first appearance first
}

// fnEntry is one command line: plain text, or a macro template with the
// indices of the parameters it uses.
type fnEntry struct {
	text string
	tmpl *fnTemplate
	idx  []int
}

// fnTemplate is StringTemplate: the text between variables and the
// variables' names.
type fnTemplate struct {
	segments []string
	vars     []string
}

// javaTrim is String.trim: every character at or below a space goes from
// both ends.
func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

// compileFunction reads a function's text (CommandFunction.fromLines +
// FunctionBuilder).
func compileFunction(id, text string) (*mcFunction, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // the newline ending the file ends its last line
	}
	fn := &mcFunction{id: id}
	paramIdx := map[string]int{}
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := javaTrim(lines[i])
		if strings.HasSuffix(line, "\\") {
			var b strings.Builder
			b.WriteString(line)
			for {
				i++
				if i == len(lines) {
					return nil, errors.New("Line continuation at end of file")
				}
				cur := b.String()
				b.Reset()
				b.WriteString(cur[:len(cur)-1])
				b.WriteString(javaTrim(lines[i]))
				if b.Len() > maxCommandLine {
					return nil, fmt.Errorf("Command too long: %d characters", b.Len())
				}
				if !strings.HasSuffix(b.String(), "\\") {
					break
				}
			}
			line = b.String()
		}
		if len(line) > maxCommandLine {
			return nil, fmt.Errorf("Command too long: %d characters", len(line))
		}
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '/' {
			if len(line) > 1 && line[1] == '/' {
				return nil, fmt.Errorf("Unknown or invalid command '%s' on line %d (if you intended to make a comment, use '#' not '//')", line, lineNo)
			}
			return nil, fmt.Errorf("Unknown or invalid command '%s' on line %d (did you mean '%s'? Do not use a preceding forwards slash.)", line, lineNo, unquotedWord(line[1:]))
		}
		if line[0] == '$' {
			tmpl, err := parseTemplate(line[1:])
			if err != nil {
				return nil, fmt.Errorf("Can't parse function line %d: '%s': %v", lineNo, line[1:], err)
			}
			e := fnEntry{tmpl: tmpl}
			for _, v := range tmpl.vars {
				k, ok := paramIdx[v]
				if !ok {
					k = len(fn.params)
					paramIdx[v] = k
					fn.params = append(fn.params, v)
				}
				e.idx = append(e.idx, k)
			}
			fn.macro = true
			fn.entries = append(fn.entries, e)
			continue
		}
		if err := functionLineAllowed(line); err != nil {
			return nil, fmt.Errorf("Whilst parsing command on line %d: %v", lineNo, err)
		}
		fn.entries = append(fn.entries, fnEntry{text: line})
	}
	return fn, nil
}

// unquotedWord is StringReader.readUnquotedString at the start of s.
func unquotedWord(s string) string {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' || s[i] == '_' || s[i] == '-' || s[i] == '.' || s[i] == '+') {
		i++
	}
	return s[:i]
}

// functionLineAllowed is the part of the load-time parse the engine can
// make: a command whose root requires more than the function permission
// level is not there for a function, and the line does not parse.
func functionLineAllowed(line string) error {
	root := line
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		root = line[:i]
	}
	if need, ok := cmdPermission[root]; ok && need > functionPermission {
		return errors.New("Unknown or incomplete command. See below for error at position 0: <--[HERE]")
	}
	return nil
}

// parseTemplate is StringTemplate.fromString.
func parseTemplate(s string) (*fnTemplate, error) {
	t := &fnTemplate{}
	start := 0
	idx := strings.IndexByte(s, '$')
	for idx != -1 {
		if idx != len(s)-1 && s[idx+1] == '(' {
			t.segments = append(t.segments, s[start:idx])
			end := strings.IndexByte(s[idx+1:], ')')
			if end == -1 {
				return nil, errors.New("Unterminated macro variable")
			}
			end += idx + 1
			v := s[idx+2 : end]
			if !validMacroVar(v) {
				return nil, fmt.Errorf("Invalid macro variable name '%s'", v)
			}
			t.vars = append(t.vars, v)
			start = end + 1
			if n := strings.IndexByte(s[start:], '$'); n >= 0 {
				idx = start + n
			} else {
				idx = -1
			}
		} else if n := strings.IndexByte(s[idx+1:], '$'); n >= 0 {
			idx = idx + 1 + n
		} else {
			idx = -1
		}
	}
	if start == 0 {
		return nil, errors.New("No variables in macro")
	}
	if start != len(s) {
		t.segments = append(t.segments, s[start:])
	}
	return t, nil
}

// validMacroVar is StringTemplate.isValidVariableName: letters, digits and
// underscores.
func validMacroVar(v string) bool {
	for _, r := range v {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// substitute fills the template's variables in order.
func (t *fnTemplate) substitute(args []string) string {
	var b strings.Builder
	for i := range t.vars {
		b.WriteString(t.segments[i])
		b.WriteString(args[i])
	}
	if len(t.segments) > len(t.vars) {
		b.WriteString(t.segments[len(t.segments)-1])
	}
	return b.String()
}

// instantiate is CommandFunction.instantiate: a plain function's lines
// as they are (arguments are ignored), a macro function's with its
// variables filled from args. The error is the instantiation failure's
// text.
func (f *mcFunction) instantiate(args map[string]any) ([]string, error) {
	if !f.macro {
		out := make([]string, len(f.entries))
		for i, e := range f.entries {
			out[i] = e.text
		}
		return out, nil
	}
	if args == nil {
		return nil, fmt.Errorf("Missing arguments to function %s", f.id)
	}
	values := make([]string, len(f.params))
	for i, name := range f.params {
		v, ok := args[name]
		if !ok {
			return nil, fmt.Errorf("Missing argument %s to function %s", name, f.id)
		}
		values[i] = macroValue(v)
	}
	out := make([]string, 0, len(f.entries))
	for _, e := range f.entries {
		if e.tmpl == nil {
			out = append(out, e.text)
			continue
		}
		sub := make([]string, len(e.idx))
		for i, k := range e.idx {
			sub[i] = values[k]
		}
		line := e.tmpl.substitute(sub)
		if len(line) > maxCommandLine {
			return nil, fmt.Errorf("Command too long: %d characters", len(line))
		}
		if err := functionLineAllowed(line); err != nil {
			return nil, fmt.Errorf("While instantiating macro %s: Command '%s' caused error: %v", f.id, line, err)
		}
		out = append(out, line)
	}
	return out, nil
}

// macroValue is MacroFunction.stringify: a number plainly (a float to at
// most fifteen decimals, no exponent), a string as it is, a boolean as
// the byte it is stored as, anything else as SNBT.
func macroValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case float64:
		return decimalText(x)
	case float32:
		return decimalText(float64(x))
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	return snbtText(v)
}

// decimalText is DecimalFormat("#") with fifteen fraction digits at most.
func decimalText(f float64) string {
	s := strconv.FormatFloat(f, 'f', 15, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// snbtText writes a parsed SNBT value back as text: compounds with their
// keys sorted, strings quoted where a bare word would not read back.
func snbtText(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(snbtKey(k))
			b.WriteByte(':')
			b.WriteString(snbtText(x[k]))
		}
		b.WriteByte('}')
		return b.String()
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = snbtText(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case string:
		return strconv.Quote(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64) + "d"
	case bool:
		if x {
			return "1b"
		}
		return "0b"
	case nil:
		return "{}"
	}
	return fmt.Sprint(v)
}

func snbtKey(k string) string {
	if k == "" {
		return `""`
	}
	for i := 0; i < len(k); i++ {
		if !snbtBareChar(k[i]) {
			return strconv.Quote(k)
		}
	}
	return k
}
