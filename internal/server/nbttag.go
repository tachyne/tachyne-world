package server

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Typed NBT, for /data (DataCommands) and its NBT paths: unlike the untyped
// SNBT values the other commands read (snbt.go), a tag here keeps its type,
// because /data prints it ("20.0f", "1b"), compares it type-strictly (an
// IntTag 1 is not a ByteTag 1) and stores it back as it came.
//
// A compound is a map[string]any and a list or typed array is a *nbtList —
// both references, so an NBT path can change them in place the way vanilla's
// Node.setTag does. Scalars are the named number types below and string.

type (
	nbtByte   int8
	nbtShort  int16
	nbtInt    int32
	nbtLong   int64
	nbtFloat  float32
	nbtDouble float64
)

// nbtList is a ListTag, or with arr set ('B', 'I' or 'L') a byte, int or
// long array. Since 1.21.5 a ListTag may hold elements of mixed types.
type nbtList struct {
	arr   byte
	elems []any
}

// typedListOf builds the parsed [...] of a typed SNBT value: a typed array's
// elements take the array's element type, and anything not a number in one
// is refused.
func typedListOf(arr byte, elems []any) (any, error) {
	l := &nbtList{arr: arr, elems: elems}
	if arr == 0 {
		return l, nil
	}
	for i, e := range elems {
		v, ok := arrayElem(arr, e)
		if !ok {
			return nil, fmt.Errorf("Invalid array element type")
		}
		l.elems[i] = v
	}
	return l, nil
}

// arrayElem is a numeric tag cast to a typed array's element type
// (NumericTag.byteValue / intValue / longValue), false for a non-number.
func arrayElem(arr byte, v any) (any, bool) {
	n, ok := tagInt64(v)
	if !ok {
		return nil, false
	}
	switch arr {
	case 'B':
		return nbtByte(int8(n)), true
	case 'I':
		return nbtInt(int32(n)), true
	}
	return nbtLong(n), true
}

// snbtTypedScalar types a bare word as vanilla's SNBT grammar does: true and
// false are bytes, a number takes its suffix's type (b, s, l, f, d), a plain
// whole number is an int and one with a point or an exponent a double;
// anything else is a string.
func snbtTypedScalar(w string) any {
	switch strings.ToLower(w) {
	case "true":
		return nbtByte(1)
	case "false":
		return nbtByte(0)
	}
	body, suffix := w, byte(0)
	if last := w[len(w)-1]; strings.IndexByte("bBsSlLfFdD", last) >= 0 && len(w) > 1 {
		body, suffix = w[:len(w)-1], last|0x20
	}
	intIn := func(bits int) (int64, bool) {
		v, err := strconv.ParseInt(body, 10, bits)
		return v, err == nil
	}
	switch suffix {
	case 'b':
		if v, ok := intIn(8); ok {
			return nbtByte(v)
		}
	case 's':
		if v, ok := intIn(16); ok {
			return nbtShort(v)
		}
	case 'l':
		if v, ok := intIn(64); ok {
			return nbtLong(v)
		}
	case 'f':
		if snbtNumeric(body) {
			if v, err := strconv.ParseFloat(body, 32); err == nil {
				return nbtFloat(v)
			}
		}
	case 'd':
		if snbtNumeric(body) {
			if v, err := strconv.ParseFloat(body, 64); err == nil {
				return nbtDouble(v)
			}
		}
	default:
		if v, err := strconv.ParseInt(w, 10, 32); err == nil {
			return nbtInt(v)
		}
		if snbtNumeric(w) && strings.ContainsAny(w, ".eE") {
			if v, err := strconv.ParseFloat(w, 64); err == nil {
				return nbtDouble(v)
			}
		}
	}
	return w
}

// snbtNumeric is whether a word is spelt like a number (so "inf" and "NaN",
// which Go would read, stay strings as they do in vanilla).
func snbtNumeric(w string) bool {
	if w == "" {
		return false
	}
	for i := 0; i < len(w); i++ {
		c := w[i]
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E') {
			return false
		}
	}
	return true
}

// tagInt64 reads any numeric tag as a whole number (NumericTag.longValue).
func tagInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case nbtByte:
		return int64(n), true
	case nbtShort:
		return int64(n), true
	case nbtInt:
		return int64(n), true
	case nbtLong:
		return int64(n), true
	case nbtFloat:
		return int64(n), true
	case nbtDouble:
		return int64(n), true
	}
	return 0, false
}

// tagFloat64 reads any numeric tag as a double (NumericTag.doubleValue).
func tagFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case nbtByte:
		return float64(n), true
	case nbtShort:
		return float64(n), true
	case nbtInt:
		return float64(n), true
	case nbtLong:
		return float64(n), true
	case nbtFloat:
		return float64(n), true
	case nbtDouble:
		return float64(n), true
	}
	return 0, false
}

// tagIsNumeric is instanceof NumericTag.
func tagIsNumeric(v any) bool {
	_, ok := tagFloat64(v)
	return ok
}

// tagCopy is Tag.copy: a deep copy of compounds and lists.
func tagCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = tagCopy(e)
		}
		return out
	case *nbtList:
		out := &nbtList{arr: t.arr, elems: make([]any, len(t.elems))}
		for i, e := range t.elems {
			out.elems[i] = tagCopy(e)
		}
		return out
	}
	return v
}

// tagEqual is Tag.equals: the same type and the same value, all the way down.
func tagEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !tagEqual(v, w) {
				return false
			}
		}
		return true
	case *nbtList:
		y, ok := b.(*nbtList)
		if !ok || x.arr != y.arr || len(x.elems) != len(y.elems) {
			return false
		}
		for i := range x.elems {
			if !tagEqual(x.elems[i], y.elems[i]) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	}
	return a == b // same dynamic type and value
}

// tagCompare is NbtUtils.compareNbt: a pattern matches a tag of the same type
// whose compound keys cover the pattern's, recursively; with partial set a
// list pattern matches a list holding a match for each of its elements.
func tagCompare(pattern, have any, partial bool) bool {
	if pattern == nil {
		return true
	}
	if have == nil {
		return false
	}
	switch p := pattern.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok || len(p) > len(h) {
			return false
		}
		for k, v := range p {
			if !tagCompare(v, h[k], partial) {
				return false
			}
		}
		return true
	case *nbtList:
		h, ok := have.(*nbtList)
		if !ok || h.arr != p.arr {
			return false
		}
		if p.arr == 0 && partial {
			if len(p.elems) == 0 {
				return len(h.elems) == 0
			}
			for _, e := range p.elems {
				found := false
				for _, g := range h.elems {
					if tagCompare(e, g, partial) {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
			return true
		}
	}
	return tagEqual(pattern, have)
}

// tagMerge is CompoundTag.merge: each key of src goes into dst, a compound
// merging into a compound already there and anything else replacing it.
func tagMerge(dst, src map[string]any) {
	for k, v := range src {
		if sub, ok := v.(map[string]any); ok {
			if into, ok := dst[k].(map[string]any); ok {
				tagMerge(into, sub)
				continue
			}
		}
		dst[k] = tagCopy(v)
	}
}

// tagTooDeep is NbtPath.isTooDeep: nesting past 512 levels.
func tagTooDeep(v any, depth int) bool {
	if depth >= 512 {
		return true
	}
	switch t := v.(type) {
	case map[string]any:
		for _, e := range t {
			if tagTooDeep(e, depth+1) {
				return true
			}
		}
	case *nbtList:
		if t.arr == 0 {
			for _, e := range t.elems {
				if tagTooDeep(e, depth+1) {
					return true
				}
			}
		}
	}
	return false
}

// tagSize is what /data get returns for a tag: a number's floor, a
// collection's or a compound's size, a string's length.
func tagSize(v any) int {
	switch t := v.(type) {
	case map[string]any:
		return len(t)
	case *nbtList:
		return len(t.elems)
	case string:
		return len([]rune(t))
	}
	f, _ := tagFloat64(v)
	return int(math.Floor(f))
}

// tagString is NbtUtils.toPrettyComponent's text (TextComponentTagVisitor
// with no indentation): SNBT with a space after each separator. Compound
// keys are sorted, where vanilla prints them in hash order.
func tagString(v any) string {
	var b strings.Builder
	writeTag(&b, v, 0)
	return b.String()
}

func writeTag(b *strings.Builder, v any, depth int) {
	switch t := v.(type) {
	case string:
		b.WriteString(snbtQuote(t))
	case nbtByte:
		b.WriteString(strconv.Itoa(int(t)) + "b")
	case nbtShort:
		b.WriteString(strconv.Itoa(int(t)) + "s")
	case nbtInt:
		b.WriteString(strconv.Itoa(int(t)))
	case nbtLong:
		b.WriteString(strconv.FormatInt(int64(t), 10) + "L")
	case nbtFloat:
		b.WriteString(jFloat(float32(t)) + "f")
	case nbtDouble:
		b.WriteString(jDoubleNBT(float64(t)) + "d")
	case *nbtList:
		if t.arr != 0 {
			b.WriteString("[" + string(t.arr) + ";")
			for i, e := range t.elems {
				if i >= 128 {
					b.WriteString("<...>")
					break
				}
				b.WriteByte(' ')
				writeTag(b, e, depth+1)
				if i != len(t.elems)-1 {
					b.WriteByte(',')
				}
			}
			b.WriteByte(']')
			return
		}
		switch {
		case len(t.elems) == 0:
			b.WriteString("[]")
		case depth >= 64:
			b.WriteString("[<...>]")
		default:
			// Short non-numeric lists are the "wrapped" form, which stops at
			// 128 elements; numeric or long ones print whole.
			wrapped := len(t.elems) < 8
			if wrapped {
				numeric := true
				for _, e := range t.elems {
					numeric = numeric && tagIsNumeric(e)
				}
				wrapped = !numeric
			}
			b.WriteByte('[')
			for i, e := range t.elems {
				if wrapped && i >= 128 {
					b.WriteString("<...>")
					break
				}
				if i != 0 {
					b.WriteString(", ")
				}
				writeTag(b, e, depth+1)
			}
			b.WriteByte(']')
		}
	case map[string]any:
		switch {
		case len(t) == 0:
			b.WriteString("{}")
		case depth >= 64:
			b.WriteString("{<...>}")
		default:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i != 0 {
					b.WriteString(", ")
				}
				if snbtSimpleKey(k) {
					b.WriteString(k)
				} else {
					b.WriteString(snbtQuote(k))
				}
				b.WriteString(": ")
				writeTag(b, t[k], depth+1)
			}
			b.WriteByte('}')
		}
	}
}

// jDoubleNBT is Double.toString for a DoubleTag (NaN and the infinities
// spelt as Java does).
func jDoubleNBT(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return jDouble(f)
}

// snbtSimpleKey is TextComponentTagVisitor.SIMPLE_VALUE: [A-Za-z0-9._+-]+.
func snbtSimpleKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

// snbtQuote is StringTag.quoteAndEscape: double quotes unless the text holds
// a double quote before any single one, backslashes and the quote escaped,
// control characters written as escapes.
func snbtQuote(s string) string {
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"' || c == '\'':
			if quote == 0 {
				quote = '"'
				if c == '"' {
					quote = '\''
				}
			}
			if quote == c {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < ' ':
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	if quote == 0 {
		quote = '"'
	}
	return string(quote) + b.String() + string(quote)
}

// tagText is DataCommands.getAsText, what /data modify … string takes from a
// tag: a string's own text, a number's SNBT; anything else is refused.
func tagText(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case map[string]any, *nbtList, nil:
		return "", false
	}
	return tagString(v), true
}

// ---- the engine's untyped views ---------------------------------------------

// nbtKeyTypes are the saved types of the fields the untyped block-entity and
// item views (blockentitynbt.go) produce as plain numbers; every other whole
// number there is an int and every fraction a double.
var nbtKeyTypes = map[string]byte{
	"Slot":             'b',
	"Delay":            's',
	"is_waxed":         'b',
	"has_glowing_text": 'b',
}

// tagFromView types an untyped view (map[string]any, []any, int64, float64,
// bool, string) as tags, key naming the field a value sits in.
func tagFromView(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = tagFromView(e, k)
		}
		return out
	case []any:
		out := &nbtList{elems: make([]any, len(t))}
		for i, e := range t {
			out.elems[i] = tagFromView(e, key)
		}
		return out
	case bool:
		if t {
			return nbtByte(1)
		}
		return nbtByte(0)
	case int64:
		switch nbtKeyTypes[key] {
		case 'b':
			return nbtByte(t)
		case 's':
			return nbtShort(t)
		}
		return nbtInt(t)
	case int:
		return tagFromView(int64(t), key)
	case float64:
		return nbtDouble(t)
	case float32:
		return nbtFloat(t)
	}
	return v
}

// tagToView is tagFromView's inverse: the untyped form carriedFromNBT and
// applySummonNBT read (every whole number an int64, every fraction a
// float64, a typed array a plain list).
func tagToView(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = tagToView(e)
		}
		return out
	case *nbtList:
		out := make([]any, len(t.elems))
		for i, e := range t.elems {
			out[i] = tagToView(e)
		}
		return out
	case nbtFloat, nbtDouble:
		f, _ := tagFloat64(v)
		return f
	case nbtByte, nbtShort, nbtInt, nbtLong:
		n, _ := tagInt64(v)
		return n
	}
	return v
}

// tagDoubles is a list of doubles (Pos, Motion).
func tagDoubles(v ...float64) *nbtList {
	l := &nbtList{elems: make([]any, len(v))}
	for i, f := range v {
		l.elems[i] = nbtDouble(f)
	}
	return l
}

// tagFloats is a list of floats (Rotation).
func tagFloats(v ...float32) *nbtList {
	l := &nbtList{elems: make([]any, len(v))}
	for i, f := range v {
		l.elems[i] = nbtFloat(f)
	}
	return l
}

// tagBool is a boolean as NBT keeps one, a byte.
func tagBool(b bool) nbtByte {
	if b {
		return 1
	}
	return 0
}

// tagUUID is UUIDUtil.CODEC's saved form: four big-endian ints.
func tagUUID(u [16]byte) *nbtList {
	l := &nbtList{arr: 'I', elems: make([]any, 4)}
	for i := 0; i < 4; i++ {
		l.elems[i] = nbtInt(int32(uint32(u[i*4])<<24 | uint32(u[i*4+1])<<16 | uint32(u[i*4+2])<<8 | uint32(u[i*4+3])))
	}
	return l
}
