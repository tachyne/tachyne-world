package server

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /compute (ComputeCommand, 26.3): evaluate a number provider in a loot
// context and print what it returned.
//
//	/compute default|block <pos>|entity <target> float <provider> [<scale>]
//	/compute default|block <pos>|entity <target> integer <provider>
//
// The context (LootContextSources) always carries the source as this and
// the source position as origin; block adds the block there, entity the
// target entity. A provider is an id in the context_float_provider or
// context_int_provider registry, or written inline as SNBT — the id is tried
// first, so a bare number reads as an id (ResourceOrIdArgument's grammar).
//
// Providers and the loot conditions inside them are decoded into closures
// once, then run on the hub. Arithmetic follows Java's: an int overflow or a
// division by zero is an ArithmeticException, which the command reports as
// an invalid value with the exception's message, and a float that is not
// finite is invalid too.

const computeUsage = "Usage: /compute default|block <pos>|entity <target> float <provider> [<scale>] | integer <provider>"

// computeCtx is the LootContext a provider runs in.
type computeCtx struct {
	h      *hub
	this   *tracked   // THIS_ENTITY: the command's source
	target *cmdEntity // TARGET_ENTITY: entity source only
	hasBS  bool       // BLOCK_STATE: block source only
	state  uint32
}

// arithErr is an ArithmeticException, carrying its message.
type arithErr string

func (e arithErr) Error() string { return string(e) }

type (
	intProv   func(c *computeCtx) (int32, error)
	floatProv func(c *computeCtx) (float32, error)
	numCond   func(c *computeCtx) bool
)

// getInt is ContextIntProvider.getInt: an exception reads as 0.
func (p intProv) getInt(c *computeCtx) int32 {
	v, err := p(c)
	if err != nil {
		return 0
	}
	return v
}

// getFloat is ContextFloatProvider.getFloat: an exception or a value that is
// not finite reads as 0.
func (p floatProv) getFloat(c *computeCtx) float32 {
	v, err := p(c)
	if err != nil || !finite32(v) {
		return 0
	}
	return v
}

// getFloatOrThrow refuses a value that is not finite.
func (p floatProv) getFloatOrThrow(c *computeCtx) (float32, error) {
	v, err := p(c)
	if err == nil && !finite32(v) {
		err = arithErr("Invalid value: " + jFloat(v))
	}
	return v, err
}

func finite32(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

// javaF2I is Java's (int) cast of a floating value: NaN is 0, and anything
// beyond int's range saturates.
func javaF2I(v float64) int32 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	}
	return int32(v)
}

// javaF2L is Java's (long) cast of a floating value.
func javaF2L(v float64) int64 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt64:
		return math.MaxInt64
	case v <= math.MinInt64:
		return math.MinInt64
	}
	return int64(v)
}

// longToIntSafe is ContextIntProvider.longToIntSafe.
func longToIntSafe(v int64) (int32, error) {
	if int64(int32(v)) != v {
		return 0, arithErr(fmt.Sprintf("Value %d can't be safely converted to int", v))
	}
	return int32(v), nil
}

// javaMaxF and javaMinF are Math.max and Math.min on floats: NaN wins, and
// 0.0 is above -0.0.
func javaMaxF(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(a)) {
		return b
	}
	if a >= b {
		return a
	}
	return b
}

func javaMinF(a, b float32) float32 {
	if a != a {
		return a
	}
	if a == 0 && b == 0 && math.Signbit(float64(b)) {
		return b
	}
	if a <= b {
		return a
	}
	return b
}

var (
	mthSinOnce  sync.Once
	mthSinTable []float32
)

// mthSin and mthCos are Mth.sin and Mth.cos: a 65536-entry table.
func mthSin(v float64) float32 {
	mthSinOnce.Do(func() {
		mthSinTable = make([]float32, 65536)
		for i := range mthSinTable {
			mthSinTable[i] = float32(math.Sin(float64(i) / 10430.378350470453))
		}
	})
	return mthSinTable[javaF2L(v*10430.378350470453)&65535]
}

func mthCos(v float64) float32 { return mthSin(v + 16384.0/10430.378350470453) }

// --- decoding ---

// provDecodeDepth bounds nesting, inline or through registry references.
const provDecodeDepth = 64

type provDecoder struct{ depth int }

func (d *provDecoder) enter() error {
	if d.depth++; d.depth > provDecodeDepth {
		return fmt.Errorf("provider nested too deeply")
	}
	return nil
}

func (d *provDecoder) leave() { d.depth-- }

// registryJSON decodes one generated registry entry into the same shape
// parseSNBT gives: maps, lists, strings, bools, int64 and float64.
func registryJSON(defs map[string]string, reg, id string) (any, error) {
	src, ok := defs[nsID(id)]
	if !ok {
		return nil, fmt.Errorf("Failed to get element %s", nsID(id))
	}
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s %s: %v", reg, id, err)
	}
	return fromJSONNumbers(v), nil
}

func fromJSONNumbers(v any) any {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		for k, e := range t {
			t[k] = fromJSONNumbers(e)
		}
	case []any:
		for i, e := range t {
			t[i] = fromJSONNumbers(e)
		}
	}
	return v
}

// numIntValue and numFloatValue are Codec.INT and Codec.FLOAT on a numeric
// tag (Number.intValue / floatValue); a byte is a number too.
func numIntValue(v any) (int32, bool) {
	switch n := v.(type) {
	case int64:
		return int32(n), true
	case float64:
		return javaF2I(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func numFloatValue(v any) (float32, bool) {
	switch n := v.(type) {
	case int64:
		return float32(n), true
	case float64:
		return float32(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// typedMap is a dispatched value's map and its type, namespace dropped.
func typedMap(v any, what string) (map[string]any, string, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("Not a map: %v", v)
	}
	ts, ok := m["type"].(string)
	if !ok {
		return nil, "", fmt.Errorf("No key type in MapLike[%v]", m)
	}
	id := nsID(ts)
	if !strings.HasPrefix(id, "minecraft:") {
		return nil, "", fmt.Errorf("Unknown registry key in ResourceKey[minecraft:root / minecraft:%s]: %s", what, id)
	}
	return m, strings.TrimPrefix(id, "minecraft:"), nil
}

func field(m map[string]any, key string) (any, error) {
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("No key %s in MapLike[%v]", key, m)
	}
	return v, nil
}

// holderList is a HolderSet's entries: a list, or one entry on its own.
func holderList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return []any{v}
}

func unsupported(what, kind string) error {
	return fmt.Errorf("the %s minecraft:%s is not supported", what, kind)
}

func (d *provDecoder) intField(m map[string]any, key string) (intProv, error) {
	v, err := field(m, key)
	if err != nil {
		return nil, err
	}
	return d.intProvider(v)
}

func (d *provDecoder) floatField(m map[string]any, key string) (floatProv, error) {
	v, err := field(m, key)
	if err != nil {
		return nil, err
	}
	return d.floatProvider(v)
}

func (d *provDecoder) intList(m map[string]any) ([]intProv, error) {
	v, err := field(m, "inputs")
	if err != nil {
		return nil, err
	}
	var out []intProv
	for _, e := range holderList(v) {
		p, err := d.intProvider(e)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (d *provDecoder) floatList(m map[string]any) ([]floatProv, error) {
	v, err := field(m, "inputs")
	if err != nil {
		return nil, err
	}
	var out []floatProv
	for _, e := range holderList(v) {
		p, err := d.floatProvider(e)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// weights reads a weighted_list distribution: {data, weight} entries.
func weights(m map[string]any) ([]any, []int, error) {
	v, err := field(m, "distribution")
	if err != nil {
		return nil, nil, err
	}
	l, ok := v.([]any)
	if !ok || len(l) == 0 {
		return nil, nil, fmt.Errorf("List must have contents")
	}
	var data []any
	var ws []int
	for _, e := range l {
		em, ok := e.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("Not a map: %v", e)
		}
		dv, err := field(em, "data")
		if err != nil {
			return nil, nil, err
		}
		wv, err := field(em, "weight")
		if err != nil {
			return nil, nil, err
		}
		w, ok := numIntValue(wv)
		if !ok || w < 0 {
			return nil, nil, fmt.Errorf("Value must be non-negative: %v", wv)
		}
		data, ws = append(data, dv), append(ws, int(w))
	}
	return data, ws, nil
}

// pickWeighted is WeightedList.getRandomOrThrow.
func pickWeighted(c *computeCtx, ws []int) (int, error) {
	total := 0
	for _, w := range ws {
		total += w
	}
	if total == 0 {
		return 0, arithErr("Weighted list has no elements")
	}
	sel := c.h.rng.Intn(total)
	for i, w := range ws {
		if sel < w {
			return i, nil
		}
		sel -= w
	}
	return len(ws) - 1, nil
}

// branch decodes the shared shapes of conditional and number_dispatcher:
// the condition(s), and the value index each case selects.
func (d *provDecoder) branches(kind string, m map[string]any) (conds []numCond, vals []any, def any, err error) {
	switch kind {
	case "conditional":
		cv, err := field(m, "condition")
		if err != nil {
			return nil, nil, nil, err
		}
		cond, err := d.condition(cv)
		if err != nil {
			return nil, nil, nil, err
		}
		on, err := field(m, "on_true")
		if err != nil {
			return nil, nil, nil, err
		}
		def, ok := m["on_false"]
		if !ok {
			def = int64(0)
		}
		return []numCond{cond}, []any{on}, def, nil
	default: // number_dispatcher
		cv, err := field(m, "cases")
		if err != nil {
			return nil, nil, nil, err
		}
		l, ok := cv.([]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("Not a list: %v", cv)
		}
		for _, e := range l {
			em, ok := e.(map[string]any)
			if !ok {
				return nil, nil, nil, fmt.Errorf("Not a map: %v", e)
			}
			c, err := field(em, "condition")
			if err != nil {
				return nil, nil, nil, err
			}
			cond, err := d.condition(c)
			if err != nil {
				return nil, nil, nil, err
			}
			v, err := field(em, "value")
			if err != nil {
				return nil, nil, nil, err
			}
			conds, vals = append(conds, cond), append(vals, v)
		}
		def, ok := m["default"]
		if !ok {
			def = int64(0)
		}
		return conds, vals, def, nil
	}
}

// intProvider decodes a Holder<ContextIntProvider>: a registry id, a
// number, or a typed map.
func (d *provDecoder) intProvider(v any) (intProv, error) {
	if err := d.enter(); err != nil {
		return nil, err
	}
	defer d.leave()
	if id, ok := v.(string); ok {
		ref, err := registryJSON(intProviderDefs, "context_int_provider", id)
		if err != nil {
			return nil, err
		}
		return d.intProvider(ref)
	}
	if n, ok := numIntValue(v); ok {
		return func(*computeCtx) (int32, error) { return n, nil }, nil
	}
	m, kind, err := typedMap(v, "context_int_provider_type")
	if err != nil {
		return nil, err
	}
	unary := func(fn func(int32) (int32, error)) (intProv, error) {
		in, err := d.intField(m, "input")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (int32, error) {
			a, err := in(c)
			if err != nil {
				return 0, err
			}
			return fn(a)
		}, nil
	}
	binary := func(l, r string, fn func(a, b int32) (int32, error)) (intProv, error) {
		lp, err := d.intField(m, l)
		if err != nil {
			return nil, err
		}
		rp, err := d.intField(m, r)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (int32, error) {
			a, err := lp(c)
			if err != nil {
				return 0, err
			}
			b, err := rp(c)
			if err != nil {
				return 0, err
			}
			return fn(a, b)
		}, nil
	}
	aggregate := func(start int64, fn func(acc int64, v int32) int64, finish func(acc int64, n int) (int32, error)) (intProv, error) {
		ins, err := d.intList(m)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (int32, error) {
			acc := start
			for _, p := range ins {
				v, err := p(c)
				if err != nil {
					return 0, err
				}
				acc = fn(acc, v)
			}
			return finish(acc, len(ins))
		}, nil
	}
	const divZero = arithErr("/ by zero")
	const overflow = arithErr("integer overflow")
	switch kind {
	case "constant":
		vv, err := field(m, "value")
		if err != nil {
			return nil, err
		}
		n, ok := numIntValue(vv)
		if !ok {
			return nil, fmt.Errorf("Not a number: %v", vv)
		}
		return func(*computeCtx) (int32, error) { return n, nil }, nil
	case "abs": // Math.absExact
		return unary(func(a int32) (int32, error) {
			if a == math.MinInt32 {
				return 0, arithErr("Overflow to represent absolute value of Integer.MIN_VALUE")
			}
			return max(a, -a), nil
		})
	case "negate": // Math.negateExact
		return unary(func(a int32) (int32, error) {
			if a == math.MinInt32 {
				return 0, overflow
			}
			return -a, nil
		})
	case "sub": // Math.subtractExact
		return binary("left", "right", func(a, b int32) (int32, error) {
			r := int64(a) - int64(b)
			if int64(int32(r)) != r {
				return 0, overflow
			}
			return int32(r), nil
		})
	case "div":
		return binary("left", "right", func(a, b int32) (int32, error) {
			if b == 0 {
				return 0, divZero
			}
			return a / b, nil // MIN_VALUE / -1 wraps, as in Java
		})
	case "mod":
		return binary("left", "right", func(a, b int32) (int32, error) {
			if b == 0 {
				return 0, divZero
			}
			return a % b, nil
		})
	case "floor_div": // Math.floorDivExact
		return binary("left", "right", func(a, b int32) (int32, error) {
			if b == 0 {
				return 0, divZero
			}
			if a == math.MinInt32 && b == -1 {
				return 0, overflow
			}
			q := a / b
			if (a%b != 0) && ((a < 0) != (b < 0)) {
				q--
			}
			return q, nil
		})
	case "floor_mod": // Math.floorMod
		return binary("left", "right", func(a, b int32) (int32, error) {
			if b == 0 {
				return 0, divZero
			}
			r := a % b
			if r != 0 && (r < 0) != (b < 0) {
				r += b
			}
			return r, nil
		})
	case "pow": // Math.powExact, after the 0^0 check
		return binary("base", "exponent", func(a, b int32) (int32, error) {
			if a == 0 && b == 0 {
				return 0, arithErr("Result of 0 to the power of 0 is undefined")
			}
			if b < 0 {
				return 0, arithErr("negative exponent")
			}
			switch a {
			case 0, 1:
				return a, nil
			case -1:
				return 1 - 2*(b&1), nil
			}
			r := int64(1) // |a| ≥ 2: overflow comes within 31 steps
			for ; b > 0; b-- {
				if r *= int64(a); int64(int32(r)) != r {
					return 0, overflow
				}
			}
			return int32(r), nil
		})
	case "add":
		return aggregate(0, func(acc int64, v int32) int64 { return acc + int64(v) },
			func(acc int64, _ int) (int32, error) { return longToIntSafe(acc) })
	case "mul":
		return aggregate(1, func(acc int64, v int32) int64 { return acc * int64(v) },
			func(acc int64, _ int) (int32, error) { return longToIntSafe(acc) })
	case "avg":
		return aggregate(0, func(acc int64, v int32) int64 { return acc + int64(v) },
			func(acc int64, n int) (int32, error) {
				if n == 0 {
					return 0, divZero
				}
				return longToIntSafe(acc / int64(n))
			})
	case "max":
		return aggregate(math.MinInt32, func(acc int64, v int32) int64 { return max(acc, int64(v)) },
			func(acc int64, _ int) (int32, error) { return int32(acc), nil })
	case "min":
		return aggregate(math.MaxInt32, func(acc int64, v int32) int64 { return min(acc, int64(v)) },
			func(acc int64, _ int) (int32, error) { return int32(acc), nil })
	case "from_float": // ContextIntProvider.floatToIntSafe
		in, err := d.floatField(m, "input")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (int32, error) {
			f, err := in(c)
			if err != nil {
				return 0, err
			}
			if !finite32(f) {
				return 0, arithErr("Value " + jFloat(f) + " can't be safely converted to int")
			}
			return longToIntSafe(javaF2L(float64(f)))
		}, nil
	case "uniform": // Mth.nextInt
		return d.uniformInt(m)
	case "binomial":
		n, err := d.intField(m, "n")
		if err != nil {
			return nil, err
		}
		p, err := d.floatField(m, "p")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (int32, error) {
			trials, err := n(c)
			if err != nil {
				return 0, err
			}
			chance, err := p.getFloatOrThrow(c)
			if err != nil {
				return 0, err
			}
			if trials > binomialTrialCap { // a guard for the hub, which a real server would hang on
				return 0, arithErr(fmt.Sprintf("Value %d is too many trials", trials))
			}
			hits := int32(0)
			for i := int32(0); i < trials; i++ {
				if c.h.rng.Float32() < chance {
					hits++
				}
			}
			return hits, nil
		}, nil
	case "weighted_list":
		data, ws, err := weights(m)
		if err != nil {
			return nil, err
		}
		ps := make([]intProv, len(data))
		for i, dv := range data {
			if ps[i], err = d.intProvider(dv); err != nil {
				return nil, err
			}
		}
		return func(c *computeCtx) (int32, error) {
			i, err := pickWeighted(c, ws)
			if err != nil {
				return 0, err
			}
			return ps[i](c)
		}, nil
	case "conditional", "number_dispatcher":
		conds, vals, def, err := d.branches(kind, m)
		if err != nil {
			return nil, err
		}
		ps := make([]intProv, len(vals)+1)
		for i, v := range append(vals, def) {
			if ps[i], err = d.intProvider(v); err != nil {
				return nil, err
			}
		}
		return func(c *computeCtx) (int32, error) {
			for i, cond := range conds {
				if cond(c) {
					return ps[i](c)
				}
			}
			return ps[len(conds)](c)
		}, nil
	case "score":
		return d.scoreProvider(m)
	case "storage": // no command storage exists here: always the fallback
		if _, err := field(m, "storage"); err != nil {
			return nil, err
		}
		if _, err := field(m, "path"); err != nil {
			return nil, err
		}
		fb, ok := m["fallback"]
		if !ok {
			fb = int64(0)
		}
		return d.intProvider(fb)
	case "environment_attribute":
		return nil, unsupported("int provider", kind)
	}
	return nil, fmt.Errorf("Unknown registry key in ResourceKey[minecraft:root / minecraft:context_int_provider_type]: minecraft:%s", kind)
}

// binomialTrialCap bounds a binomial draw's loop on the hub.
const binomialTrialCap = 1 << 24

// uniformInt is UniformGenerator: Mth.nextInt(random, min, max).
func (d *provDecoder) uniformInt(m map[string]any) (intProv, error) {
	lo, err := d.intField(m, "min")
	if err != nil {
		return nil, err
	}
	hi, err := d.intField(m, "max")
	if err != nil {
		return nil, err
	}
	return func(c *computeCtx) (int32, error) {
		a, err := lo(c)
		if err != nil {
			return 0, err
		}
		b, err := hi(c)
		if err != nil {
			return 0, err
		}
		if a >= b {
			return a, nil
		}
		return a + int32(c.h.rng.Int63n(int64(b)-int64(a)+1)), nil
	}, nil
}

// scoreProvider is ScoreboardValue: the target's score in the objective, or
// the fallback when the target, the objective or the score is missing.
func (d *provDecoder) scoreProvider(m map[string]any) (intProv, error) {
	tv, err := field(m, "target")
	if err != nil {
		return nil, err
	}
	var fixed, ctxTarget string
	switch t := tv.(type) {
	case string:
		ctxTarget = t
	case map[string]any:
		_, kind, err := typedMap(t, "loot_score_provider_type")
		if err != nil {
			return nil, err
		}
		switch kind {
		case "fixed":
			nv, err := field(t, "name")
			if err != nil {
				return nil, err
			}
			fixed, _ = nv.(string)
		case "context":
			tg, err := field(t, "target")
			if err != nil {
				return nil, err
			}
			ctxTarget, _ = tg.(string)
		default:
			return nil, fmt.Errorf("Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_score_provider_type]: minecraft:%s", kind)
		}
	default:
		return nil, fmt.Errorf("Not a string or map: %v", tv)
	}
	if fixed == "" {
		switch ctxTarget {
		case "this", "attacker", "direct_attacker", "attacking_player", "target_entity", "interacting_entity":
		default:
			return nil, fmt.Errorf("Unknown element name:%s", ctxTarget)
		}
	}
	sv, err := field(m, "score")
	if err != nil {
		return nil, err
	}
	obj, _ := sv.(string)
	fbv, ok := m["fallback"]
	if !ok {
		fbv = int64(0)
	}
	fb, err := d.intProvider(fbv)
	if err != nil {
		return nil, err
	}
	return func(c *computeCtx) (int32, error) {
		holder := fixed
		if holder == "" {
			switch ctxTarget {
			case "this":
				if c.this != nil {
					holder = c.this.p.name
				}
			case "target_entity":
				if c.target != nil && c.target.t != nil {
					holder = c.target.t.p.name
				}
			}
		}
		if sb := c.h.sb; holder != "" && sb != nil {
			if _, ok := sb.Objectives[obj]; ok {
				if v, ok := sb.Scores[holder][obj]; ok {
					return v, nil
				}
			}
		}
		return fb(c)
	}, nil
}

// floatProvider decodes a Holder<ContextFloatProvider>.
func (d *provDecoder) floatProvider(v any) (floatProv, error) {
	if err := d.enter(); err != nil {
		return nil, err
	}
	defer d.leave()
	if id, ok := v.(string); ok {
		ref, err := registryJSON(floatProviderDefs, "context_float_provider", id)
		if err != nil {
			return nil, err
		}
		return d.floatProvider(ref)
	}
	if f, ok := numFloatValue(v); ok {
		return func(*computeCtx) (float32, error) { return f, nil }, nil
	}
	m, kind, err := typedMap(v, "context_float_provider_type")
	if err != nil {
		return nil, err
	}
	unary := func(fn func(float32) float32) (floatProv, error) {
		in, err := d.floatField(m, "input")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (float32, error) {
			a, err := in(c)
			if err != nil {
				return 0, err
			}
			return fn(a), nil
		}, nil
	}
	binary := func(l, r string, fn func(c *computeCtx, a, b float32) float32) (floatProv, error) {
		lp, err := d.floatField(m, l)
		if err != nil {
			return nil, err
		}
		rp, err := d.floatField(m, r)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (float32, error) {
			a, err := lp(c)
			if err != nil {
				return 0, err
			}
			b, err := rp(c)
			if err != nil {
				return 0, err
			}
			return fn(c, a, b), nil
		}, nil
	}
	aggregate := func(start float32, fn func(acc, v float32) float32, finish func(acc float32, n int) float32) (floatProv, error) {
		ins, err := d.floatList(m)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (float32, error) {
			acc := start
			for _, p := range ins {
				v, err := p(c)
				if err != nil {
					return 0, err
				}
				acc = fn(acc, v)
			}
			return finish(acc, len(ins)), nil
		}, nil
	}
	same := func(acc float32, _ int) float32 { return acc }
	switch kind {
	case "constant":
		vv, err := field(m, "value")
		if err != nil {
			return nil, err
		}
		f, ok := numFloatValue(vv)
		if !ok {
			return nil, fmt.Errorf("Not a number: %v", vv)
		}
		return func(*computeCtx) (float32, error) { return f, nil }, nil
	case "abs":
		return unary(func(a float32) float32 { return float32(math.Abs(float64(a))) })
	case "negate":
		return unary(func(a float32) float32 { return -a })
	case "ceil": // Mth.ceil returns an int
		return unary(func(a float32) float32 { return float32(javaF2I(math.Ceil(float64(a)))) })
	case "floor":
		return unary(func(a float32) float32 { return float32(math.Floor(float64(a))) })
	case "round": // Math.round(float) returns an int
		return unary(func(a float32) float32 { return float32(javaF2I(math.Floor(float64(a) + 0.5))) })
	case "truncate":
		return unary(func(a float32) float32 {
			if a > 0 {
				return float32(math.Floor(float64(a)))
			}
			return float32(math.Ceil(float64(a)))
		})
	case "sqrt":
		return unary(func(a float32) float32 { return float32(math.Sqrt(float64(a))) })
	case "sin":
		return unary(func(a float32) float32 { return mthSin(float64(a)) })
	case "cos":
		return unary(func(a float32) float32 { return mthCos(float64(a)) })
	case "sub":
		return binary("left", "right", func(_ *computeCtx, a, b float32) float32 { return a - b })
	case "div":
		return binary("left", "right", func(_ *computeCtx, a, b float32) float32 { return a / b })
	case "mod":
		return binary("left", "right", func(_ *computeCtx, a, b float32) float32 {
			if b == 0 {
				return float32(math.NaN())
			}
			return float32(math.Mod(float64(a), float64(b)))
		})
	case "pow":
		return binary("base", "exponent", func(_ *computeCtx, a, b float32) float32 {
			if a == 0 && b == 0 {
				return float32(math.NaN())
			}
			return float32(math.Pow(float64(a), float64(b)))
		})
	case "uniform": // Mth.nextFloat
		return binary("min", "max", func(c *computeCtx, a, b float32) float32 {
			if a >= b {
				return a
			}
			return c.h.rng.Float32()*(b-a) + a
		})
	case "add":
		return aggregate(0, func(acc, v float32) float32 { return acc + v }, same)
	case "mul":
		return aggregate(1, func(acc, v float32) float32 { return acc * v }, same)
	case "avg":
		return aggregate(0, func(acc, v float32) float32 { return acc + v },
			func(acc float32, n int) float32 { return acc / float32(n) })
	case "length":
		return aggregate(0, func(acc, v float32) float32 { return acc + v*v },
			func(acc float32, _ int) float32 { return float32(math.Sqrt(float64(acc))) })
	case "max":
		return aggregate(-math.MaxFloat32, javaMaxF, same)
	case "min":
		return aggregate(math.MaxFloat32, javaMinF, same)
	case "from_int":
		in, err := d.intField(m, "input")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) (float32, error) {
			v, err := in(c)
			return float32(v), err
		}, nil
	case "weighted_list":
		data, ws, err := weights(m)
		if err != nil {
			return nil, err
		}
		ps := make([]floatProv, len(data))
		for i, dv := range data {
			if ps[i], err = d.floatProvider(dv); err != nil {
				return nil, err
			}
		}
		return func(c *computeCtx) (float32, error) {
			i, err := pickWeighted(c, ws)
			if err != nil {
				return 0, err
			}
			return ps[i](c)
		}, nil
	case "conditional", "number_dispatcher":
		conds, vals, def, err := d.branches(kind, m)
		if err != nil {
			return nil, err
		}
		ps := make([]floatProv, len(vals)+1)
		for i, v := range append(vals, def) {
			if ps[i], err = d.floatProvider(v); err != nil {
				return nil, err
			}
		}
		return func(c *computeCtx) (float32, error) {
			for i, cond := range conds {
				if cond(c) {
					return ps[i](c)
				}
			}
			return ps[len(conds)](c)
		}, nil
	case "storage": // no command storage exists here: always the fallback
		if _, err := field(m, "storage"); err != nil {
			return nil, err
		}
		if _, err := field(m, "path"); err != nil {
			return nil, err
		}
		fb, ok := m["fallback"]
		if !ok {
			fb = int64(0)
		}
		return d.floatProvider(fb)
	case "enchantment_level":
		// No enchantment level is in a command's context: the amount at 0.
		av, err := field(m, "amount")
		if err != nil {
			return nil, err
		}
		at0, err := levelBasedAt0(av)
		if err != nil {
			return nil, err
		}
		return func(*computeCtx) (float32, error) { return at0, nil }, nil
	case "environment_attribute":
		return nil, unsupported("float provider", kind)
	}
	return nil, fmt.Errorf("Unknown registry key in ResourceKey[minecraft:root / minecraft:context_float_provider_type]: minecraft:%s", kind)
}

// levelBasedAt0 is LevelBasedValue.calculate(0) for a constant or a linear
// value (base + per_level_above_first × (level − 1)).
func levelBasedAt0(v any) (float32, error) {
	if f, ok := numFloatValue(v); ok {
		return f, nil
	}
	m, kind, err := typedMap(v, "enchantment_level_based_value_type")
	if err != nil {
		return 0, err
	}
	if kind != "linear" {
		return 0, unsupported("level-based value", kind)
	}
	bv, err := field(m, "base")
	if err != nil {
		return 0, err
	}
	base, _ := numFloatValue(bv)
	pv, ok := m["per_level_above_first"]
	per := base
	if ok {
		per, _ = numFloatValue(pv)
	}
	return base - per, nil
}

// condition decodes a Holder<LootItemCondition>: a predicate id or a typed
// map. A condition whose context parameter a command never supplies (a
// tool, a killer, a damage source) tests as vanilla's does without it.
func (d *provDecoder) condition(v any) (numCond, error) {
	if err := d.enter(); err != nil {
		return nil, err
	}
	defer d.leave()
	if id, ok := v.(string); ok {
		ref, err := registryJSON(predicateDefs, "predicate", id)
		if err != nil {
			return nil, err
		}
		return d.condition(ref)
	}
	m, kind, err := typedMap(v, "loot_condition_type")
	if err != nil {
		return nil, err
	}
	switch kind {
	case "inverted":
		tv, err := field(m, "term")
		if err != nil {
			return nil, err
		}
		term, err := d.condition(tv)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) bool { return !term(c) }, nil
	case "all_of", "any_of":
		tv, err := field(m, "terms")
		if err != nil {
			return nil, err
		}
		var terms []numCond
		for _, e := range holderList(tv) {
			t, err := d.condition(e)
			if err != nil {
				return nil, err
			}
			terms = append(terms, t)
		}
		anyOf := kind == "any_of"
		return func(c *computeCtx) bool {
			for _, t := range terms {
				if t(c) == anyOf {
					return anyOf
				}
			}
			return !anyOf
		}, nil
	case "random_chance":
		chance, err := d.floatField(m, "chance")
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) bool { return c.h.rng.Float32() < chance.getFloat(c) }, nil
	case "match_block":
		return matchBlockCond(m)
	case "int_value_check":
		val, err := d.intField(m, "value")
		if err != nil {
			return nil, err
		}
		tv, err := field(m, "test")
		if err != nil {
			return nil, err
		}
		test, err := d.intRange(tv)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) bool { return test(c, val.getInt(c)) }, nil
	case "float_value_check":
		val, err := d.floatField(m, "value")
		if err != nil {
			return nil, err
		}
		tv, err := field(m, "test")
		if err != nil {
			return nil, err
		}
		test, err := d.floatRange(tv)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx) bool { return test(c, val.getFloat(c)) }, nil
	case "weather_check":
		rain, hasRain := m["raining"].(bool)
		thunder, hasThunder := m["thundering"].(bool)
		return func(c *computeCtx) bool {
			if hasRain && rain != c.h.raining {
				return false
			}
			return !hasThunder || thunder == c.h.thundering
		}, nil
	case "match_tool", "killed_by_player", "damage_source_properties":
		return func(*computeCtx) bool { return false }, nil // no tool, killer or damage source
	case "survives_explosion":
		return func(*computeCtx) bool { return true }, nil // no explosion radius
	case "random_chance_with_enchanted_bonus", "entity_properties", "entity_scores", "table_bonus",
		"location_check", "time_check", "enchantment_active_check", "environment_attribute_check":
		return nil, unsupported("loot condition", kind)
	}
	return nil, fmt.Errorf("Unknown registry key in ResourceKey[minecraft:root / minecraft:loot_condition_type]: minecraft:%s", kind)
}

// intRange is IntRangePredicate: a point (a provider the input must equal)
// or a line {min, max}, each bound a provider read with getInt.
func (d *provDecoder) intRange(v any) (func(c *computeCtx, in int32) bool, error) {
	if m, ok := v.(map[string]any); !ok || m["type"] != nil {
		p, err := d.intProvider(v)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx, in int32) bool { return in == p.getInt(c) }, nil
	}
	m := v.(map[string]any)
	var lo, hi intProv
	var err error
	if mv, ok := m["min"]; ok {
		if lo, err = d.intProvider(mv); err != nil {
			return nil, err
		}
	}
	if mv, ok := m["max"]; ok {
		if hi, err = d.intProvider(mv); err != nil {
			return nil, err
		}
	}
	return func(c *computeCtx, in int32) bool {
		return (lo == nil || in >= lo.getInt(c)) && (hi == nil || in <= hi.getInt(c))
	}, nil
}

// floatRange is FloatRangePredicate, intRange's float twin.
func (d *provDecoder) floatRange(v any) (func(c *computeCtx, in float32) bool, error) {
	if m, ok := v.(map[string]any); !ok || m["type"] != nil {
		p, err := d.floatProvider(v)
		if err != nil {
			return nil, err
		}
		return func(c *computeCtx, in float32) bool { return in == p.getFloat(c) }, nil
	}
	m := v.(map[string]any)
	var lo, hi floatProv
	var err error
	if mv, ok := m["min"]; ok {
		if lo, err = d.floatProvider(mv); err != nil {
			return nil, err
		}
	}
	if mv, ok := m["max"]; ok {
		if hi, err = d.floatProvider(mv); err != nil {
			return nil, err
		}
	}
	return func(c *computeCtx, in float32) bool {
		return (lo == nil || in >= lo.getFloat(c)) && (hi == nil || in <= hi.getFloat(c))
	}, nil
}

// matchBlockCond is MatchBlock over a BlockPredicate: the blocks (an id, a
// list, or a #tag) and the state properties; it needs a block in the
// context, which only the block source gives.
func matchBlockCond(m map[string]any) (numCond, error) {
	var members map[string]bool
	if bv, ok := m["blocks"]; ok {
		members = map[string]bool{}
		if s, ok := bv.(string); ok && strings.HasPrefix(s, "#") {
			names, ok := blockTagMembers(strings.TrimPrefix(nsID(strings.TrimPrefix(s, "#")), "minecraft:"))
			if !ok {
				return nil, fmt.Errorf("Unknown block tag '%s'", nsID(strings.TrimPrefix(s, "#")))
			}
			for _, n := range names {
				members[n] = true
			}
		} else {
			for _, e := range holderList(bv) {
				s, _ := e.(string)
				name := strings.TrimPrefix(nsID(s), "minecraft:")
				if _, _, ok := worldgen.BlockRangeOK(name); !ok {
					return nil, fmt.Errorf("Failed to get element %s", nsID(s))
				}
				members[name] = true
			}
		}
	}
	for _, k := range []string{"nbt", "components", "predicates"} {
		if _, ok := m[k]; ok {
			return nil, fmt.Errorf("a block predicate's %s is not supported", k)
		}
	}
	type propTest struct {
		name, exact, lo, hi string
		ranged              bool
	}
	var props []propTest
	if sv, ok := m["state"]; ok {
		sm, ok := sv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Not a map: %v", sv)
		}
		keys := make([]string, 0, len(sm))
		for k := range sm {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch t := sm[k].(type) {
			case map[string]any:
				lo, _ := t["min"].(string)
				hi, _ := t["max"].(string)
				props = append(props, propTest{name: k, lo: lo, hi: hi, ranged: true})
			default:
				props = append(props, propTest{name: k, exact: fmt.Sprint(t)})
			}
		}
	}
	return func(c *computeCtx) bool {
		if !c.hasBS {
			return false
		}
		if members != nil {
			n, ok := worldgen.StateName(c.state)
			if !ok || !members[n] {
				return false
			}
		}
		if len(props) == 0 {
			return true
		}
		info, ok := worldgen.InfoForState(c.state)
		if !ok {
			return false
		}
		for _, pt := range props {
			if !info.HasProperty(pt.name) {
				return false
			}
			have := worldgen.GetProperty(info, c.state, pt.name)
			if !pt.ranged {
				if have != pt.exact {
					return false
				}
				continue
			}
			hv, err := strconv.Atoi(have)
			if err != nil {
				return false // only numeric properties order here
			}
			if lo, err := strconv.Atoi(pt.lo); pt.lo != "" && (err != nil || hv < lo) {
				return false
			}
			if hi, err := strconv.Atoi(pt.hi); pt.hi != "" && (err != nil || hv > hi) {
				return false
			}
		}
		return true
	}, nil
}

// --- the command ---

// identChar is Identifier.isAllowedInIdentifier.
func identChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '_' || c == ':' || c == '/' || c == '.' || c == '-'
}

// validIdent reports whether s reads as an Identifier: one namespace colon
// at most, and a namespace without '/'.
func validIdent(s string) bool {
	ns, path, found := strings.Cut(s, ":")
	if !found {
		return !strings.Contains(s, ":")
	}
	return !strings.ContainsAny(ns, "/") && !strings.Contains(path, ":") && path != ""
}

// computeProvider is a parsed provider argument: its registry id when it
// named one ("" inline), and its decoded value.
type computeProvider struct {
	id    string
	float floatProv
	int   intProv
}

// parseComputeProvider reads the provider argument at the start of s and
// returns what follows it. An identifier is tried first; otherwise the text
// is SNBT.
func parseComputeProvider(s string, isFloat bool) (computeProvider, string, string) {
	reg, defs := "minecraft:context_int_provider", intProviderDefs
	if isFloat {
		reg, defs = "minecraft:context_float_provider", floatProviderDefs
	}
	i := 0
	for i < len(s) && identChar(s[i]) {
		i++
	}
	var v any
	var cp computeProvider
	if i > 0 && validIdent(s[:i]) {
		if i < len(s) && s[i] != ' ' {
			return cp, "", "Expected whitespace to end one argument, but found trailing data"
		}
		id := nsID(s[:i])
		if _, ok := defs[id]; !ok {
			return cp, "", fmt.Sprintf("Can't find element '%s' in registry '%s'", id, reg)
		}
		cp.id, v = id, id
		s = s[i:]
	} else {
		val, n, err := parseSNBTPrefix(s)
		if err != nil {
			return cp, "", err.Error()
		}
		v, s = val, s[n:]
	}
	d := &provDecoder{}
	var err error
	if isFloat {
		cp.float, err = d.floatProvider(v)
	} else {
		cp.int, err = d.intProvider(v)
	}
	if err != nil {
		return cp, "", "Failed to parse structure: " + err.Error()
	}
	return cp, strings.TrimSpace(s), ""
}

// computeLines are ComputeCommand's replies for a provider, by whether it
// was named.
func computeExact(id string, result int32) string {
	if id != "" {
		return fmt.Sprintf("%s returned value %d", id, result)
	}
	return fmt.Sprintf("Number provider returned value %d", result)
}

func computeRounded(id string, result int32, original float32) string {
	if id != "" {
		return fmt.Sprintf("%s returned value %s (rounded to %d)", id, jFloat(original), result)
	}
	return fmt.Sprintf("Number provider returned value %s (rounded to %d)", jFloat(original), result)
}

func computeInvalid(id, value string) string {
	if id != "" {
		return fmt.Sprintf("%s returned invalid value (%s)", id, value)
	}
	return fmt.Sprintf("Number provider returned invalid value (%s)", value)
}

// computeResult evaluates the provider and gives the reply line, and whether
// it is a success.
func computeResult(c *computeCtx, cp computeProvider, scale float32) (string, bool) {
	if cp.int != nil { // computeAsInt
		v, err := cp.int(c)
		if err != nil {
			return computeInvalid(cp.id, err.Error()), false
		}
		return computeExact(cp.id, v), true
	}
	original, err := cp.float(c) // computeAsFloat
	if err != nil {
		return computeInvalid(cp.id, err.Error()), false
	}
	if !finite32(original) {
		return computeInvalid(cp.id, jFloat(original)), false
	}
	result := javaF2I(math.Floor(float64(original * scale))) // Mth.floor
	if float32(result) == original {
		return computeExact(cp.id, result), true
	}
	return computeRounded(cp.id, result, original), true
}

func (s *Server) cmdCompute(p *player, args []string) {
	if !s.isOp(p.name) { // ComputeCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 3 {
		p.tell(computeUsage)
		return
	}
	var (
		hasPos bool
		pos    blockPos
		target string
		rest   []string
	)
	switch args[0] {
	case "default":
		rest = args[1:]
	case "block":
		if len(args) < 6 {
			p.tell(computeUsage)
			return
		}
		x, y, z, ok := parsePosition(args[1:4], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell(computeUsage)
			return
		}
		hasPos, pos, rest = true, blockPos{floorInt(x), floorInt(y), floorInt(z)}, args[4:]
	case "entity":
		target, rest = args[1], args[2:]
	default:
		p.tell(computeUsage)
		return
	}
	if len(rest) < 2 || (rest[0] != "float" && rest[0] != "integer") {
		p.tell(computeUsage)
		return
	}
	isFloat := rest[0] == "float"
	cp, tail, msg := parseComputeProvider(strings.Join(rest[1:], " "), isFloat)
	if msg != "" {
		p.tell(msg)
		return
	}
	scale := float32(1)
	if tail != "" {
		if !isFloat || strings.Contains(tail, " ") {
			p.tell("Incorrect argument for command")
			return
		}
		f, err := strconv.ParseFloat(tail, 32)
		if err != nil {
			p.tell(fmt.Sprintf("Invalid float '%s'", tail))
			return
		}
		scale = float32(f)
	}
	s.onHub(func(players map[int32]*tracked) {
		t := players[p.eid]
		if t == nil {
			return
		}
		c := &computeCtx{h: s.hub, this: t}
		if hasPos { // BlockPosArgument.getLoadedBlockPos
			if !s.hub.cloneLoaded(t.dim, pos, pos) {
				cmdFail(p, "That position is not loaded")
				return
			}
			if !s.hub.inWorldYIn(t.dim, pos.y) {
				cmdFail(p, "That position is out of this world!")
				return
			}
			c.hasBS, c.state = true, s.hub.worldFor(t.dim).At(pos.x, pos.y, pos.z)
		}
		if target != "" {
			en, ok := s.hub.singleEntity(players, p.eid, target, func(m string) { cmdFail(p, m) })
			if !ok {
				return
			}
			c.target = &en
		}
		line, ok := computeResult(c, cp, scale)
		if !ok {
			cmdFail(p, line)
			return
		}
		s.hub.cmdSuccess(players, p, line, false)
	})
}

// computeProviderIDs are the registry ids /compute suggests for its kind.
func computeProviderIDs(isFloat bool) []string {
	defs := intProviderDefs
	if isFloat {
		defs = floatProviderDefs
	}
	return mapKeys(defs)
}
