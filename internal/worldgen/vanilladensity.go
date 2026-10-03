package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Vanilla's density functions (26.3's levelgen.densityfunction), read from
// the worldgen data and compiled as 26.3's DensityFunctionCompiler compiles
// them: references inlined, every "cache" deduplicated by its input into one
// shared cache slot, and the SliceUniformAxes rewrite that evaluates a
// function at coordinate 0 on every axis it does not depend on. Each
// compiled sampler keeps vanilla's two ways of sampling — one block
// (value) and a whole DensityVolume at once (volume) — exactly as the
// matching 26.3 sampler class writes them, because the two do not round
// alike (interpolation, noise coordinates; vanillanoisevol.go) and vanilla
// uses each where it does: the chunk fill samples final_density over the
// chunk's volume, the aquifer, the climate sampler and the material rules
// sample points. Arithmetic is float32 throughout, as vanilla's is.

// vdFn is a density function as the data writes it, references by name.
type vdFn struct {
	kind   string    // the type without its namespace; "ref" a reference, "const", "prepared" a compiled cache
	name   string    // ref: the function; noise kinds: the noise
	c      float32   // const
	kids   []*vdFn   // children (per kind, see vdParse)
	d      []float64 // double parameters
	f      []float32 // float parameters
	i      []int     // int parameters
	axis   int       // gradient/slice axis: 0 x, 1 y, 2 z
	tiling string    // gradient tiling
	metric string    // distance_to_point
	spline *vdSpline
	key    string // the raw JSON (cache dedup)
	id     int    // prepared: the cache id
}

// vdSpline is a CubicSpline over density-function coordinates.
type vdSpline struct {
	constant bool
	value    float32
	coord    *vdFn
	locs     []float32
	derivs   []float32
	vals     []*vdSpline
}

// Axis bits (DensityFunction.AXIS_*).
const (
	vdAxisX = 1
	vdAxisY = 2
	vdAxisZ = 4
)

func vdAxisBit(axis int) int { return []int{vdAxisX, vdAxisY, vdAxisZ}[axis] }

// vdParseAxis reads Direction.Axis.
func vdParseAxis(s string) (int, error) {
	switch s {
	case "x":
		return 0, nil
	case "y":
		return 1, nil
	case "z":
		return 2, nil
	}
	return 0, fmt.Errorf("unknown axis %q", s)
}

// vdLoadNamed reads a named density function from the data.
func vdLoadNamed(name string) (*vdFn, error) {
	raw, ok := vanillaWorldgenData["density_function/"+vtStripNS(name)]
	if !ok {
		return nil, fmt.Errorf("density function %q not in the data", name)
	}
	return vdParseJSON([]byte(raw))
}

func vdParseJSON(raw []byte) (*vdFn, error) {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return vdParse(v)
}

func vdNum(v any) (float64, error) {
	switch n := v.(type) {
	case json.Number:
		return strconv.ParseFloat(string(n), 64)
	case float64:
		return n, nil
	}
	return 0, fmt.Errorf("not a number: %v", v)
}

func vdKey(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// vdParse reads a density function (a number, a reference, or an object).
func vdParse(v any) (*vdFn, error) {
	switch t := v.(type) {
	case json.Number, float64:
		n, err := vdNum(t)
		if err != nil {
			return nil, err
		}
		return &vdFn{kind: "const", c: float32(n), key: vdKey(t)}, nil
	case string:
		return &vdFn{kind: "ref", name: t, key: vdKey(t)}, nil
	case map[string]any:
		return vdParseObject(t)
	}
	return nil, fmt.Errorf("density function: unexpected %T", v)
}

func vdParseObject(m map[string]any) (*vdFn, error) {
	typ, _ := m["type"].(string)
	fn := &vdFn{kind: vtStripNS(typ), key: vdKey(m)}
	child := func(field string) error {
		raw, ok := m[field]
		if !ok {
			return fmt.Errorf("%s: missing %q", typ, field)
		}
		c, err := vdParse(raw)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", typ, field, err)
		}
		fn.kids = append(fn.kids, c)
		return nil
	}
	optChild := func(field string) error {
		if _, ok := m[field]; !ok {
			fn.kids = append(fn.kids, &vdFn{kind: "const", key: "0"})
			return nil
		}
		return child(field)
	}
	num := func(field string) (float64, error) {
		raw, ok := m[field]
		if !ok {
			return 0, fmt.Errorf("%s: missing %q", typ, field)
		}
		return vdNum(raw)
	}
	f32 := func(fields ...string) error {
		for _, f := range fields {
			n, err := num(f)
			if err != nil {
				return err
			}
			fn.f = append(fn.f, float32(n))
		}
		return nil
	}
	f64 := func(fields ...string) error {
		for _, f := range fields {
			n, err := num(f)
			if err != nil {
				return err
			}
			fn.d = append(fn.d, n)
		}
		return nil
	}
	ints := func(fields ...string) error {
		for _, f := range fields {
			n, err := num(f)
			if err != nil {
				return err
			}
			fn.i = append(fn.i, int(n))
		}
		return nil
	}
	noiseName := func() error {
		s, ok := m["noise"].(string)
		if !ok {
			return fmt.Errorf("%s: noise must name a worldgen/noise", typ)
		}
		fn.name = s
		return nil
	}
	var err error
	switch fn.kind {
	case "constant":
		fn.kind = "const"
		err = f32("value")
		if err == nil {
			fn.c = fn.f[0]
		}
	case "blend_alpha", "blend_offset", "beardifier", "end_outer_islands":
	case "noise":
		if err = noiseName(); err == nil {
			if err = f64("xz_scale", "y_scale"); err == nil {
				if err = optChild("shift_x"); err == nil {
					if err = optChild("shift_y"); err == nil {
						err = optChild("shift_z")
					}
				}
			}
		}
	case "shift_a", "shift_b", "shift":
		err = noiseName()
	case "gradient":
		var axis string
		axis, _ = m["axis"].(string)
		if fn.axis, err = vdParseAxis(axis); err == nil {
			fn.tiling = "clamp_to_edge"
			if t, ok := m["tiling"].(string); ok {
				fn.tiling = t
			}
			if err = ints("from_coordinate", "to_coordinate"); err == nil {
				err = f32("from_value", "to_value")
			}
		}
	case "abs", "square", "cube", "sqrt", "half_negative", "quarter_negative", "reciprocal", "negate", "squeeze", "log", "sign",
		"cache", "blend_density":
		err = child("input")
	case "add", "sub", "mul", "div", "min", "max":
		if err = child("left"); err == nil {
			err = child("right")
		}
	case "clamp":
		if err = child("input"); err == nil {
			err = f32("min", "max")
		}
	case "lerp":
		if err = child("alpha"); err == nil {
			if err = child("first"); err == nil {
				err = child("second")
			}
		}
	case "range_choice":
		if err = child("input"); err == nil {
			if err = f32("min_inclusive", "max_exclusive"); err == nil {
				if err = child("when_in_range"); err == nil {
					err = child("when_out_of_range")
				}
			}
		}
	case "interval_select":
		if err = child("input"); err == nil {
			th, _ := m["thresholds"].([]any)
			for _, t := range th {
				n, e := vdNum(t)
				if e != nil {
					return nil, e
				}
				fn.f = append(fn.f, float32(n))
			}
			fs, _ := m["functions"].([]any)
			for _, f := range fs {
				c, e := vdParse(f)
				if e != nil {
					return nil, e
				}
				fn.kids = append(fn.kids, c)
			}
			if len(fn.f) != len(fn.kids)-2 {
				err = fmt.Errorf("interval_select: %d thresholds for %d functions", len(fn.f), len(fn.kids)-1)
			}
		}
	case "interpolated":
		if err = child("input"); err == nil {
			err = ints("cell_size_xz", "cell_size_y")
		}
	case "slice":
		var axis string
		axis, _ = m["axis"].(string)
		if fn.axis, err = vdParseAxis(axis); err == nil {
			if err = ints("coordinate"); err == nil {
				err = child("input")
			}
		}
	case "find_top_surface":
		if err = child("density"); err == nil {
			if err = child("upper_bound"); err == nil {
				err = ints("lower_bound", "cell_height")
			}
		}
	case "old_blended_noise":
		err = f64("xz_scale", "y_scale", "xz_factor", "y_factor", "smear_scale_multiplier")
	case "spline":
		fn.spline, err = vdParseSpline(m["spline"])
	case "distance_to_point":
		fn.metric, _ = m["metric"].(string)
		pt, _ := m["point"].([]any)
		if len(pt) != 3 {
			return nil, fmt.Errorf("distance_to_point: point %v", m["point"])
		}
		for _, p := range pt {
			n, e := vdNum(p)
			if e != nil {
				return nil, e
			}
			fn.i = append(fn.i, int(n))
		}
	default:
		return nil, fmt.Errorf("density function type %q is not supported", typ)
	}
	if err != nil {
		return nil, err
	}
	return fn, nil
}

func vdParseSpline(v any) (*vdSpline, error) {
	switch t := v.(type) {
	case json.Number, float64:
		n, err := vdNum(t)
		if err != nil {
			return nil, err
		}
		return &vdSpline{constant: true, value: float32(n)}, nil
	case map[string]any:
		coord, err := vdParse(t["coordinate"])
		if err != nil {
			return nil, fmt.Errorf("spline coordinate: %w", err)
		}
		s := &vdSpline{coord: coord}
		pts, _ := t["points"].([]any)
		if len(pts) == 0 {
			return nil, fmt.Errorf("spline with no points")
		}
		for _, p := range pts {
			pm, _ := p.(map[string]any)
			loc, err := vdNum(pm["location"])
			if err != nil {
				return nil, err
			}
			der, err := vdNum(pm["derivative"])
			if err != nil {
				return nil, err
			}
			val, err := vdParseSpline(pm["value"])
			if err != nil {
				return nil, err
			}
			s.locs = append(s.locs, float32(loc))
			s.derivs = append(s.derivs, float32(der))
			s.vals = append(s.vals, val)
		}
		return s, nil
	}
	return nil, fmt.Errorf("spline: unexpected %T", v)
}

// ---- compile: inline, caches, slicing ----

// vdCompiler is DensityFunctionCompiler for one world (a RandomState): the
// named functions read so far, the caches prepared, and the noises.
type vdCompiler struct {
	rs       *vtState
	named    map[string]*vdFn
	prepared map[string]*vdFn // cache input key → prepared cache
	nCaches  int
	samplers map[*vdFn]vdSampler // compiled prepared caches, by node
}

func newVDCompiler(rs *vtState) *vdCompiler {
	return &vdCompiler{rs: rs, named: map[string]*vdFn{}, prepared: map[string]*vdFn{}, samplers: map[*vdFn]vdSampler{}}
}

func (c *vdCompiler) resolve(name string) (*vdFn, error) {
	if f, ok := c.named[name]; ok {
		return f, nil
	}
	f, err := vdLoadNamed(name)
	if err != nil {
		return nil, err
	}
	c.named[name] = f
	return f, nil
}

// compile is getSampler: the optimiser rule (inline references, prepare
// caches; then SliceUniformAxes from all axes) and the sampler.
func (c *vdCompiler) compile(f *vdFn) (vdSampler, error) {
	g, err := c.optimize(f)
	if err != nil {
		return nil, err
	}
	return c.build(g)
}

func (c *vdCompiler) optimize(f *vdFn) (*vdFn, error) {
	g, err := c.inline(f)
	if err != nil {
		return nil, err
	}
	return vdSliceUniform(g, vdAxisX|vdAxisY|vdAxisZ), nil
}

// inline is the optimiser's first rule: a reference becomes its function, a
// cache becomes the prepared cache of its input (one per distinct input).
func (c *vdCompiler) inline(f *vdFn) (*vdFn, error) {
	for f.kind == "ref" {
		r, err := c.resolve(f.name)
		if err != nil {
			return nil, err
		}
		f = r
	}
	if f.kind == "cache" {
		in := f.kids[0]
		if p, ok := c.prepared[in.key]; ok {
			return p, nil
		}
		id := c.nCaches
		c.nCaches++
		opt, err := c.optimize(in)
		if err != nil {
			return nil, err
		}
		p := &vdFn{kind: "prepared", id: id, kids: []*vdFn{opt}, key: f.key}
		c.prepared[in.key] = p
		return p, nil
	}
	return vdRewriteChildren(f, c.inline)
}

// vdRewriteChildren is rewriteChildren: f with rule applied to every child
// (f itself when nothing changed).
func vdRewriteChildren(f *vdFn, rule func(*vdFn) (*vdFn, error)) (*vdFn, error) {
	switch f.kind {
	case "prepared", "shift_a", "shift_b", "shift", "const", "gradient", "blend_alpha", "blend_offset", "beardifier",
		"end_outer_islands", "distance_to_point", "old_blended_noise":
		return f, nil
	}
	changed := false
	var kids []*vdFn
	if len(f.kids) > 0 {
		kids = make([]*vdFn, len(f.kids))
		for i, k := range f.kids {
			n, err := rule(k)
			if err != nil {
				return nil, err
			}
			kids[i] = n
			changed = changed || n != k
		}
	}
	var spl *vdSpline
	if f.spline != nil {
		var err error
		spl, err = vdMapSpline(f.spline, rule, &changed)
		if err != nil {
			return nil, err
		}
	}
	if !changed {
		return f, nil
	}
	g := *f
	g.kids = kids
	g.spline = spl
	return &g, nil
}

func vdMapSpline(s *vdSpline, rule func(*vdFn) (*vdFn, error), changed *bool) (*vdSpline, error) {
	if s.constant {
		return s, nil
	}
	coord, err := rule(s.coord)
	if err != nil {
		return nil, err
	}
	if coord != s.coord {
		*changed = true
	}
	out := &vdSpline{coord: coord, locs: s.locs, derivs: s.derivs, vals: make([]*vdSpline, len(s.vals))}
	for i, v := range s.vals {
		if out.vals[i], err = vdMapSpline(v, rule, changed); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// vdDomain is domainAxes.
func vdDomain(f *vdFn) int {
	switch f.kind {
	case "const":
		return 0
	case "gradient":
		return vdAxisBit(f.axis)
	case "noise":
		axes := 7
		if f.d[1] == 0 {
			axes &^= vdAxisY
		}
		if f.d[0] == 0 {
			axes &^= vdAxisX | vdAxisZ
		}
		return axes | vdDomain(f.kids[0]) | vdDomain(f.kids[1]) | vdDomain(f.kids[2])
	case "shift_a", "shift_b", "blend_alpha", "blend_offset", "end_outer_islands":
		return vdAxisX | vdAxisZ
	case "shift", "beardifier", "old_blended_noise", "distance_to_point":
		return 7
	case "slice":
		return vdDomain(f.kids[0]) &^ vdAxisBit(f.axis)
	case "find_top_surface":
		return (vdDomain(f.kids[0]) | vdDomain(f.kids[1])) &^ vdAxisY
	case "spline":
		axes := 0
		vdForEachCoord(f.spline, func(c *vdFn) { axes |= vdDomain(c) })
		return axes
	}
	axes := 0
	for _, k := range f.kids {
		axes |= vdDomain(k)
	}
	return axes
}

func vdForEachCoord(s *vdSpline, fn func(*vdFn)) {
	if s.constant {
		return
	}
	fn(s.coord)
	for _, v := range s.vals {
		vdForEachCoord(v, fn)
	}
}

// vdSliceUniform is SliceUniformAxes(parent).rewrite.
func vdSliceUniform(f *vdFn, parent int) *vdFn {
	if f.kind == "const" || f.kind == "gradient" {
		return f
	}
	domain := vdDomain(f)
	rule := func(k *vdFn) (*vdFn, error) { return vdSliceUniform(k, domain), nil }
	g, _ := vdRewriteChildren(f, rule)
	if parent == domain {
		return g
	}
	removed := parent &^ domain
	have := 0
	for s := g; s.kind == "slice"; s = s.kids[0] {
		have |= vdAxisBit(s.axis)
	}
	removed &^= have
	slice := func(in *vdFn, axis int) *vdFn {
		return &vdFn{kind: "slice", axis: axis, i: []int{0}, kids: []*vdFn{in}}
	}
	if removed&vdAxisX != 0 {
		g = slice(g, 0)
	}
	if removed&vdAxisZ != 0 {
		g = slice(g, 2)
	}
	if removed&vdAxisY != 0 {
		g = slice(g, 1)
	}
	return g
}

// ---- samplers ----

// vdSampler is DensitySampler.
type vdSampler interface {
	value(c *vdCtx, x, y, z int) float32
	volume(c *vdCtx, out []float32, v vtVolume)
}

// vdCtx is SamplerContext: the cache cells (nil: caches disabled), and the
// chunk's beardifier.
type vdCtx struct {
	cells []vdCell
	beard vdSampler
}

type vdCell struct {
	hasVol bool
	vol    vtVolume
	buf    []float32
	hasVal bool
	kx     int
	ky     int
	kz     int
	val    float32
}

// newVDCtx is a SamplerContext with caches enabled.
func newVDCtx() *vdCtx { return &vdCtx{cells: []vdCell{}} }

// vdUncached is SamplerContext.EMPTY_UNCACHED.
func vdUncached() *vdCtx { return &vdCtx{} }

func (c *vdCtx) cell(id int) *vdCell {
	if c.cells == nil {
		return nil
	}
	if id >= len(c.cells) {
		n := (id + 16) / 16 * 16
		c.cells = append(c.cells, make([]vdCell, n-len(c.cells))...)
	}
	return &c.cells[id]
}

func vdBuf(n int) []float32 { return make([]float32, n) }

// vdBuild compiles a rewritten function into its sampler.
func (c *vdCompiler) build(f *vdFn) (vdSampler, error) {
	kid := func(i int) (vdSampler, error) { return c.build(f.kids[i]) }
	constOf := func(i int) (float32, bool) {
		if f.kids[i].kind == "const" {
			return f.kids[i].c, true
		}
		return 0, false
	}
	switch f.kind {
	case "const":
		return vdConst(f.c), nil
	case "prepared":
		if s, ok := c.samplers[f]; ok {
			return s, nil
		}
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		s := &vdCache{id: f.id, in: in}
		c.samplers[f] = s
		return s, nil
	case "blend_alpha":
		return vdConst(1), nil
	case "blend_offset":
		return vdConst(0), nil
	case "beardifier":
		return vdBeard{}, nil
	case "blend_density":
		return kid(0)
	case "noise":
		n, err := c.rs.noise(f.name)
		if err != nil {
			return nil, err
		}
		allZero := f.kids[0].kind == "const" && f.kids[0].c == 0 && f.kids[1].kind == "const" && f.kids[1].c == 0 &&
			f.kids[2].kind == "const" && f.kids[2].c == 0
		if allZero {
			return &vdNoise{n: n, xz: f.d[0], ys: f.d[1]}, nil
		}
		sx, err := kid(0)
		if err != nil {
			return nil, err
		}
		sz, err := kid(2)
		if err != nil {
			return nil, err
		}
		var sy vdSampler
		if !(f.kids[1].kind == "const" && f.kids[1].c == 0) {
			if sy, err = kid(1); err != nil {
				return nil, err
			}
		}
		return &vdShiftedNoise{n: n, xz: f.d[0], ys: f.d[1], sx: sx, sy: sy, sz: sz}, nil
	case "shift_a", "shift":
		n, err := c.rs.noise(f.name)
		if err != nil {
			return nil, err
		}
		ys := 0.0
		if f.kind == "shift" {
			ys = 0.25
		}
		return &vdConstMul{in: &vdNoise{n: n, xz: 0.25, ys: ys}, k: 4}, nil
	case "shift_b":
		n, err := c.rs.noise(f.name)
		if err != nil {
			return nil, err
		}
		return &vdShiftB{n: n}, nil
	case "gradient":
		return newVDGradient(f), nil
	case "abs", "square", "cube", "sqrt", "half_negative", "quarter_negative", "reciprocal", "negate", "squeeze", "log", "sign":
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		return &vdUnary{op: f.kind, in: in}, nil
	case "add", "sub", "mul", "div", "min", "max":
		l, err := kid(0)
		if err != nil {
			return nil, err
		}
		r, err := kid(1)
		if err != nil {
			return nil, err
		}
		lc, lok := constOf(0)
		rc, rok := constOf(1)
		switch f.kind {
		case "add":
			if lok {
				return &vdConstAdd{in: r, k: lc}, nil
			}
			if rok {
				return &vdConstAdd{in: l, k: rc}, nil
			}
		case "sub":
			if lok {
				return &vdConstSub{k: lc, in: r}, nil
			}
			if rok {
				return &vdConstAdd{in: l, k: -rc}, nil
			}
		case "mul":
			if lok {
				return &vdConstMul{in: r, k: lc}, nil
			}
			if rok {
				return &vdConstMul{in: l, k: rc}, nil
			}
		case "div":
			if lok {
				return &vdConstDiv{k: lc, in: r}, nil
			}
			if rok {
				return &vdConstMul{in: l, k: 1 / rc}, nil
			}
		case "min", "max":
			if lok {
				return &vdConstMinMax{max: f.kind == "max", in: r, k: lc}, nil
			}
			if rok {
				return &vdConstMinMax{max: f.kind == "max", in: l, k: rc}, nil
			}
		}
		return &vdBinary{op: f.kind, l: l, r: r}, nil
	case "clamp":
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		return &vdClamp{in: in, lo: f.f[0], hi: f.f[1]}, nil
	case "lerp":
		a, err := kid(0)
		if err != nil {
			return nil, err
		}
		x, err := kid(1)
		if err != nil {
			return nil, err
		}
		y, err := kid(2)
		if err != nil {
			return nil, err
		}
		return &vdLerp{a: a, first: x, second: y}, nil
	case "range_choice":
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		a, err := kid(1)
		if err != nil {
			return nil, err
		}
		b, err := kid(2)
		if err != nil {
			return nil, err
		}
		return &vdRangeChoice{in: in, lo: f.f[0], hi: f.f[1], inR: a, outR: b}, nil
	case "interval_select":
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		s := &vdIntervalSelect{in: in, th: f.f}
		for i := 1; i < len(f.kids); i++ {
			k, err := kid(i)
			if err != nil {
				return nil, err
			}
			s.fns = append(s.fns, k)
		}
		return s, nil
	case "interpolated":
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		return &vdInterpolated{in: in, cxz: f.i[0], cy: f.i[1], invXZ: 1 / float32(f.i[0]), invY: 1 / float32(f.i[1])}, nil
	case "slice":
		if f.kids[0].kind == "slice" {
			in := f.kids[0]
			if (f.axis == 0 && in.axis == 2) || (f.axis == 2 && in.axis == 0) {
				s, err := c.build(in.kids[0])
				if err != nil {
					return nil, err
				}
				x, z := f.i[0], in.i[0]
				if f.axis == 2 {
					x, z = in.i[0], f.i[0]
				}
				return &vdSliceXZ{in: s, x: x, z: z}, nil
			}
		}
		in, err := kid(0)
		if err != nil {
			return nil, err
		}
		return &vdSlice{in: in, axis: f.axis, at: f.i[0]}, nil
	case "find_top_surface":
		d, err := kid(0)
		if err != nil {
			return nil, err
		}
		u, err := kid(1)
		if err != nil {
			return nil, err
		}
		return &vdSlice{in: &vdFindTop{density: d, upper: u, lower: f.i[0], cell: f.i[1]}, axis: 1, at: 0}, nil
	case "old_blended_noise":
		return c.rs.blendedNoise(f.d[0], f.d[1], f.d[2], f.d[3], f.d[4]), nil
	case "spline":
		return c.buildSpline(f.spline)
	case "end_outer_islands":
		return c.rs.endIslands(), nil
	case "distance_to_point":
		return &vdDistance{x: f.i[0], y: f.i[1], z: f.i[2], metric: f.metric}, nil
	}
	return nil, fmt.Errorf("density function %q cannot be compiled", f.kind)
}

// vdConst is ConstantFunction.Sampler.
type vdConst float32

func (k vdConst) value(*vdCtx, int, int, int) float32 { return float32(k) }
func (k vdConst) volume(_ *vdCtx, out []float32, _ vtVolume) {
	for i := range out {
		out[i] = float32(k)
	}
}

// vdBeard is the beardifier: the chunk's structure adaptation, from the
// context (zero without one).
type vdBeard struct{}

func (vdBeard) value(c *vdCtx, x, y, z int) float32 {
	if c.beard != nil {
		return c.beard.value(c, x, y, z)
	}
	return 0
}

func (vdBeard) volume(c *vdCtx, out []float32, v vtVolume) {
	if c.beard != nil {
		c.beard.volume(c, out, v)
		return
	}
	for i := range out {
		out[i] = 0
	}
}

// vdCache is CachingDensitySampler over the context's cache cell.
type vdCache struct {
	id int
	in vdSampler
}

func (s *vdCache) volume(c *vdCtx, out []float32, v vtVolume) {
	cell := c.cell(s.id)
	if cell == nil {
		s.in.volume(c, out, v)
		return
	}
	if !cell.hasVol || cell.vol != v {
		cell.vol = v
		cell.buf = vdBuf(v.size())
		cell.hasVol = true
		s.in.volume(c, cell.buf, v)
	}
	copy(out, cell.buf)
}

func (s *vdCache) value(c *vdCtx, x, y, z int) float32 {
	cell := c.cell(s.id)
	if cell == nil {
		return s.in.value(c, x, y, z)
	}
	if cell.hasVal && cell.kx == x && cell.ky == y && cell.kz == z && cell.val == cell.val {
		return cell.val
	}
	if cell.hasVol {
		if i := cell.vol.indexOf(x, y, z); i != -1 {
			return cell.buf[i]
		}
	}
	val := s.in.value(c, x, y, z)
	cell.hasVal, cell.kx, cell.ky, cell.kz, cell.val = true, x, y, z, val
	return val
}

// vdNoise is NoiseFunction.Sampler.
type vdNoise struct {
	n      *vtStack
	xz, ys float64
}

func (s *vdNoise) value(_ *vdCtx, x, y, z int) float32 {
	return s.n.get(float64(x)*s.xz, float64(y)*s.ys, float64(z)*s.xz)
}

func (s *vdNoise) volume(_ *vdCtx, out []float32, v vtVolume) {
	for i := range out {
		out[i] = 0
	}
	s.n.addToVolume(out, v, s.xz, s.ys, 1)
}

// vdShiftedNoise is NoiseFunction.ShiftedXzSampler (sy nil) and
// ShiftedXyzSampler.
type vdShiftedNoise struct {
	n          *vtStack
	xz, ys     float64
	sx, sy, sz vdSampler
}

func (s *vdShiftedNoise) value(c *vdCtx, x, y, z int) float32 {
	nx := float64(x)*s.xz + float64(s.sx.value(c, x, y, z))
	ny := float64(y) * s.ys
	if s.sy != nil {
		ny += float64(s.sy.value(c, x, y, z))
	}
	nz := float64(z)*s.xz + float64(s.sz.value(c, x, y, z))
	return s.n.get(nx, ny, nz)
}

func (s *vdShiftedNoise) volume(c *vdCtx, out []float32, v vtVolume) {
	s.sx.volume(c, out, v)
	var sy []float32
	if s.sy != nil {
		sy = vdBuf(v.size())
		s.sy.volume(c, sy, v)
	}
	sz := vdBuf(v.size())
	s.sz.volume(c, sz, v)
	i := 0
	for iz := 0; iz < v.sz; iz++ {
		bz := float64(v.blockZ(iz)) * s.xz
		for ix := 0; ix < v.sx; ix++ {
			bx := float64(v.blockX(ix)) * s.xz
			for iy := 0; iy < v.sy; iy++ {
				nx := bx + float64(out[i])
				ny := float64(v.blockY(iy)) * s.ys
				if sy != nil {
					ny += float64(sy[i])
				}
				nz := bz + float64(sz[i])
				out[i] = s.n.get(nx, ny, nz)
				i++
			}
		}
	}
}

// vdShiftB is ShiftNoiseFunction.ShiftB's sampler.
type vdShiftB struct{ n *vtStack }

func (s *vdShiftB) value(_ *vdCtx, x, _, z int) float32 {
	return s.n.get(float64(z)*0.25, float64(x)*0.25, 0) * 4
}

func (s *vdShiftB) volume(_ *vdCtx, out []float32, v vtVolume) {
	t := vtVolume{v.sz, v.sx, 1, v.minZ, v.minX, 0, v.stepZ, v.stepX, 1}
	buf := vdBuf(t.size())
	s.n.addToVolume(buf, t, 0.25, 0.25, 4)
	for iz := 0; iz < v.sz; iz++ {
		for ix := 0; ix < v.sx; ix++ {
			val := buf[t.index(iz, ix, 0)]
			base := v.index(ix, 0, iz)
			for k := 0; k < v.sy; k++ {
				out[base+k] = val
			}
		}
	}
}

// vdGradient is GradientFunction's samplers.
type vdGradient struct {
	axis, from, lo, hi, rng int
	tiling                  string
	fromV, factor           float32
}

func newVDGradient(f *vdFn) *vdGradient {
	from, to := f.i[0], f.i[1]
	rng := to - from
	return &vdGradient{axis: f.axis, from: from, lo: min(from, to), hi: max(from, to), rng: rng, tiling: f.tiling,
		fromV: f.f[0], factor: (f.f[1] - f.f[0]) / float32(rng)}
}

func (g *vdGradient) compute(c int) float32 {
	switch g.tiling {
	case "repeat":
		return g.fromV + float32(floorModInt(c-g.from, g.rng))*g.factor
	case "mirrored_repeat":
		rel := c - g.from
		tile := floorDiv(rel, g.rng)
		local := rel - tile*g.rng
		if tile&1 == 0 {
			return g.fromV + float32(local)*g.factor
		}
		return g.fromV + float32(g.rng-local)*g.factor
	}
	cc := c
	if cc < g.lo {
		cc = g.lo
	} else if cc > g.hi {
		cc = g.hi
	}
	return g.fromV + float32(cc-g.from)*g.factor
}

func (g *vdGradient) value(_ *vdCtx, x, y, z int) float32 {
	return g.compute([3]int{x, y, z}[g.axis])
}

func (g *vdGradient) volume(_ *vdCtx, out []float32, v vtVolume) {
	switch g.axis {
	case 0:
		for ix := 0; ix < v.sx; ix++ {
			val := g.compute(v.blockX(ix))
			for iz := 0; iz < v.sz; iz++ {
				b := v.index(ix, 0, iz)
				for k := 0; k < v.sy; k++ {
					out[b+k] = val
				}
			}
		}
	case 1:
		for iy := 0; iy < v.sy; iy++ {
			val := g.compute(v.blockY(iy))
			for iz := 0; iz < v.sz; iz++ {
				for ix := 0; ix < v.sx; ix++ {
					out[v.index(ix, iy, iz)] = val
				}
			}
		}
	default:
		for iz := 0; iz < v.sz; iz++ {
			val := g.compute(v.blockZ(iz))
			b := v.index(0, 0, iz)
			for k := 0; k < v.sx*v.sy; k++ {
				out[b+k] = val
			}
		}
	}
}

// vdUnary is UnaryFunction's samplers.
type vdUnary struct {
	op string
	in vdSampler
}

func vdApplyUnary(op string, v float32) float32 {
	switch op {
	case "abs":
		return float32(math.Abs(float64(v)))
	case "square":
		return v * v
	case "cube":
		return v * v * v
	case "sqrt":
		return float32(math.Sqrt(float64(v)))
	case "half_negative":
		if v > 0 {
			return v
		}
		return v * 0.5
	case "quarter_negative":
		if v > 0 {
			return v
		}
		return v * 0.25
	case "reciprocal":
		return 1 / v
	case "negate":
		return -v
	case "squeeze":
		c := vdMthClamp(v, -1, 1)
		return c/2 - c*c*c/24
	case "log":
		return float32(math.Log(float64(v)))
	case "sign":
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return v
	}
	panic("vanilla density: unknown unary " + op)
}

func (s *vdUnary) value(c *vdCtx, x, y, z int) float32 {
	return vdApplyUnary(s.op, s.in.value(c, x, y, z))
}

func (s *vdUnary) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i, x := range out {
		out[i] = vdApplyUnary(s.op, x)
	}
}

// vdMthClamp is Mth.clamp(float).
func vdMthClamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	return vcMin(v, hi)
}

type vdConstAdd struct {
	in vdSampler
	k  float32
}

func (s *vdConstAdd) value(c *vdCtx, x, y, z int) float32 { return s.in.value(c, x, y, z) + s.k }
func (s *vdConstAdd) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		out[i] += s.k
	}
}

type vdConstSub struct {
	k  float32
	in vdSampler
}

func (s *vdConstSub) value(c *vdCtx, x, y, z int) float32 { return s.k - s.in.value(c, x, y, z) }
func (s *vdConstSub) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		out[i] = s.k - out[i]
	}
}

type vdConstMul struct {
	in vdSampler
	k  float32
}

func (s *vdConstMul) value(c *vdCtx, x, y, z int) float32 { return s.in.value(c, x, y, z) * s.k }
func (s *vdConstMul) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		out[i] = out[i] * s.k
	}
}

type vdConstDiv struct {
	k  float32
	in vdSampler
}

func (s *vdConstDiv) value(c *vdCtx, x, y, z int) float32 { return s.k / s.in.value(c, x, y, z) }
func (s *vdConstDiv) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		out[i] = s.k / out[i]
	}
}

// vdConstMinMax is ConstMinSampler / ConstMaxSampler.
type vdConstMinMax struct {
	max bool
	in  vdSampler
	k   float32
}

func (s *vdConstMinMax) value(c *vdCtx, x, y, z int) float32 {
	if s.max {
		return vcMax(s.in.value(c, x, y, z), s.k)
	}
	return vcMin(s.in.value(c, x, y, z), s.k)
}

func (s *vdConstMinMax) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		if s.max {
			if s.k > out[i] {
				out[i] = s.k
			}
		} else if s.k < out[i] {
			out[i] = s.k
		}
	}
}

// vdBinary is BinaryFunction's two-input samplers.
type vdBinary struct {
	op   string
	l, r vdSampler
}

func (s *vdBinary) value(c *vdCtx, x, y, z int) float32 {
	l := s.l.value(c, x, y, z)
	switch s.op {
	case "add":
		return l + s.r.value(c, x, y, z)
	case "sub":
		return l - s.r.value(c, x, y, z)
	case "mul":
		if l == 0 {
			return 0
		}
		return l * s.r.value(c, x, y, z)
	case "div":
		if l == 0 {
			return 0
		}
		return l / s.r.value(c, x, y, z)
	case "min":
		return vcMin(l, s.r.value(c, x, y, z))
	default:
		return vcMax(l, s.r.value(c, x, y, z))
	}
}

func (s *vdBinary) volume(c *vdCtx, out []float32, v vtVolume) {
	s.l.volume(c, out, v)
	r := vdBuf(v.size())
	s.r.volume(c, r, v)
	switch s.op {
	case "add":
		for i := range out {
			out[i] += r[i]
		}
	case "sub":
		for i := range out {
			out[i] += -r[i]
		}
	case "mul":
		for i := range out {
			out[i] = out[i] * r[i]
		}
	case "div":
		for i := range out {
			out[i] = out[i] / r[i]
		}
	case "min":
		for i := range out {
			if r[i] < out[i] {
				out[i] = r[i]
			}
		}
	default:
		for i := range out {
			if r[i] > out[i] {
				out[i] = r[i]
			}
		}
	}
}

// vdClamp is ClampFunction.Sampler.
type vdClamp struct {
	in     vdSampler
	lo, hi float32
}

func (s *vdClamp) value(c *vdCtx, x, y, z int) float32 {
	return vdMthClamp(s.in.value(c, x, y, z), s.lo, s.hi)
}

func (s *vdClamp) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	for i := range out {
		out[i] = vdMthClamp(out[i], s.lo, s.hi)
	}
}

// vdLerp is LerpFunction's samplers (the constant forms differ only in
// what they skip computing).
type vdLerp struct{ a, first, second vdSampler }

func (s *vdLerp) value(c *vdCtx, x, y, z int) float32 {
	a := s.a.value(c, x, y, z)
	if a == 0 {
		return s.first.value(c, x, y, z)
	}
	if a == 1 {
		return s.second.value(c, x, y, z)
	}
	return vnLerp(a, s.first.value(c, x, y, z), s.second.value(c, x, y, z))
}

func (s *vdLerp) volume(c *vdCtx, out []float32, v vtVolume) {
	s.a.volume(c, out, v)
	f := vdBuf(v.size())
	s.first.volume(c, f, v)
	g := vdBuf(v.size())
	s.second.volume(c, g, v)
	for i, a := range out {
		switch a {
		case 0:
			out[i] = f[i]
		case 1:
			out[i] = g[i]
		default:
			out[i] = vnLerp(a, f[i], g[i])
		}
	}
}

// vdRangeChoice is RangeChoiceFunction.Sampler.
type vdRangeChoice struct {
	in, inR, outR vdSampler
	lo, hi        float32
}

func (s *vdRangeChoice) value(c *vdCtx, x, y, z int) float32 {
	v := s.in.value(c, x, y, z)
	if v >= s.lo && v < s.hi {
		return s.inR.value(c, x, y, z)
	}
	return s.outR.value(c, x, y, z)
}

func (s *vdRangeChoice) volume(c *vdCtx, out []float32, v vtVolume) {
	s.inR.volume(c, out, v)
	in := vdBuf(v.size())
	s.in.volume(c, in, v)
	o := vdBuf(v.size())
	s.outR.volume(c, o, v)
	for i, x := range in {
		if !(x >= s.lo) || !(x < s.hi) {
			out[i] = o[i]
		}
	}
}

// vdIntervalSelect is IntervalSelectFunction's samplers.
type vdIntervalSelect struct {
	in  vdSampler
	th  []float32
	fns []vdSampler
}

func (s *vdIntervalSelect) pick(v float32) int {
	for i, t := range s.th {
		if v < t {
			return i
		}
	}
	return len(s.fns) - 1
}

func (s *vdIntervalSelect) value(c *vdCtx, x, y, z int) float32 {
	return s.fns[s.pick(s.in.value(c, x, y, z))].value(c, x, y, z)
}

func (s *vdIntervalSelect) volume(c *vdCtx, out []float32, v vtVolume) {
	s.in.volume(c, out, v)
	bufs := make([][]float32, len(s.fns))
	if len(s.th) == 1 {
		bufs[0] = vdBuf(v.size())
		s.fns[0].volume(c, bufs[0], v)
		last := len(s.fns) - 1
		bufs[last] = vdBuf(v.size())
		s.fns[last].volume(c, bufs[last], v)
		for i, x := range out {
			if x < s.th[0] {
				out[i] = bufs[0][i]
			} else {
				out[i] = bufs[last][i]
			}
		}
		return
	}
	for i, f := range s.fns {
		bufs[i] = vdBuf(v.size())
		f.volume(c, bufs[i], v)
	}
	for i, x := range out {
		out[i] = bufs[s.pick(x)][i]
	}
}

// vdInterpolated is InterpolatedFunction.Sampler: the input at the corners
// of cells cxz×cy×cxz, interpolated across them.
type vdInterpolated struct {
	in          vdSampler
	cxz, cy     int
	invXZ, invY float32
}

func (s *vdInterpolated) volume(c *vdCtx, out []float32, v vtVolume) {
	switch {
	case (v.stepX == s.cxz || v.sx == 1) && (v.stepY == s.cy || v.sy == 1) && (v.stepZ == s.cxz || v.sz == 1) &&
		floorModInt(v.minX, s.cxz) == 0 && floorModInt(v.minY, s.cy) == 0 && floorModInt(v.minZ, s.cxz) == 0:
		s.in.volume(c, out, v)
	case v.unitStep():
		s.blockStep(c, out, v)
	default:
		bv := vtVol(v.sx*v.stepX, v.sy*v.stepY, v.sz*v.stepZ, v.minX, v.minY, v.minZ)
		bb := vdBuf(bv.size())
		s.blockStep(c, bb, bv)
		for iz := 0; iz < v.sz; iz++ {
			for ix := 0; ix < v.sx; ix++ {
				for iy := 0; iy < v.sy; iy++ {
					out[v.index(ix, iy, iz)] = bb[bv.index(ix*v.stepX, iy*v.stepY, iz*v.stepZ)]
				}
			}
		}
	}
}

func (s *vdInterpolated) blockStep(c *vdCtx, out []float32, v vtVolume) {
	minCX, minCY, minCZ := floorDiv(v.minX, s.cxz), floorDiv(v.minY, s.cy), floorDiv(v.minZ, s.cxz)
	maxCX, maxCY, maxCZ := floorDiv(v.maxX(), s.cxz), floorDiv(v.maxY(), s.cy), floorDiv(v.maxZ(), s.cxz)
	nx, ny, nz := maxCX-minCX+1, maxCY-minCY+1, maxCZ-minCZ+1
	cv := vtVolume{nx, ny, nz, minCX * s.cxz, minCY * s.cy, minCZ * s.cxz, s.cxz, s.cy, s.cxz}
	if floorModInt(v.maxX(), s.cxz) != 0 {
		cv.sx++
	}
	if floorModInt(v.maxY(), s.cy) != 0 {
		cv.sy++
	}
	if floorModInt(v.maxZ(), s.cxz) != 0 {
		cv.sz++
	}
	cb := vdBuf(cv.size())
	s.in.volume(c, cb, cv)
	for cz := 0; cz < nz; cz++ {
		ncz := min(cz+1, cv.sz-1)
		for cx := 0; cx < nx; cx++ {
			ncx := min(cx+1, cv.sx-1)
			v000 := cb[cv.index(cx, 0, cz)]
			v100 := cb[cv.index(ncx, 0, cz)]
			v001 := cb[cv.index(cx, 0, ncz)]
			v101 := cb[cv.index(ncx, 0, ncz)]
			for cy := 0; cy < ny; cy++ {
				ncy := min(cy+1, cv.sy-1)
				v010 := cb[cv.index(cx, ncy, cz)]
				v110 := cb[cv.index(ncx, ncy, cz)]
				v011 := cb[cv.index(cx, ncy, ncz)]
				v111 := cb[cv.index(ncx, ncy, ncz)]
				s.fillCell(out, v, cv, cx, cy, cz, v000, v100, v010, v110, v001, v101, v011, v111)
				v000, v100, v001, v101 = v010, v110, v011, v111
			}
		}
	}
}

func (s *vdInterpolated) fillCell(out []float32, ov, cv vtVolume, cx, cy, cz int,
	v000, v100, v010, v110, v001, v101, v011, v111 float32) {
	ox := cv.blockX(cx) - ov.minX
	oy := cv.blockY(cy) - ov.minY
	oz := cv.blockZ(cz) - ov.minZ
	x0, y0, z0 := max(0, -ox), max(0, -oy), max(0, -oz)
	x1 := min(s.cxz, ov.sx-ox) - 1
	y1 := min(s.cy, ov.sy-oy) - 1
	z1 := min(s.cxz, ov.sz-oz) - 1
	for z := z0; z <= z1; z++ {
		az := float32(z) * s.invXZ
		v00 := vnLerp(az, v000, v001)
		v01 := vnLerp(az, v010, v011)
		v10 := vnLerp(az, v100, v101)
		v11 := vnLerp(az, v110, v111)
		for x := x0; x <= x1; x++ {
			ax := float32(x) * s.invXZ
			a := vnLerp(ax, v00, v10)
			b := vnLerp(ax, v01, v11)
			step := (b - a) * s.invY
			val := a + step*float32(y0)
			i := ov.index(ox+x, oy+y0, oz+z)
			for y := y0; y <= y1; y++ {
				out[i] = val
				i++
				val += step
			}
		}
	}
}

func (s *vdInterpolated) value(c *vdCtx, x, y, z int) float32 {
	xi, yi, zi := floorModInt(x, s.cxz), floorModInt(y, s.cy), floorModInt(z, s.cxz)
	if xi == 0 && yi == 0 && zi == 0 {
		return s.in.value(c, x, y, z)
	}
	v := vtVolume{2, 2, 2, x - xi, y - yi, z - zi, s.cxz, s.cy, s.cxz}
	b := vdBuf(8)
	s.in.volume(c, b, v)
	return vnLerp3(float32(xi)/float32(s.cxz), float32(yi)/float32(s.cy), float32(zi)/float32(s.cxz),
		b[v.index(0, 0, 0)], b[v.index(1, 0, 0)], b[v.index(0, 1, 0)], b[v.index(1, 1, 0)],
		b[v.index(0, 0, 1)], b[v.index(1, 0, 1)], b[v.index(0, 1, 1)], b[v.index(1, 1, 1)])
}

// vdSlice is SliceFunction's single-axis samplers: the input at a fixed
// coordinate on one axis.
type vdSlice struct {
	in   vdSampler
	axis int
	at   int
}

func (s *vdSlice) value(c *vdCtx, x, y, z int) float32 {
	switch s.axis {
	case 0:
		return s.in.value(c, s.at, y, z)
	case 1:
		return s.in.value(c, x, s.at, z)
	}
	return s.in.value(c, x, y, s.at)
}

func (s *vdSlice) volume(c *vdCtx, out []float32, v vtVolume) {
	switch s.axis {
	case 0:
		if v.sx == 1 && v.minX == s.at {
			s.in.volume(c, out, v)
			return
		}
		iv := vtVolume{1, v.sy, v.sz, s.at, v.minY, v.minZ, v.stepX, v.stepY, v.stepZ}
		ib := vdBuf(iv.size())
		s.in.volume(c, ib, iv)
		i := 0
		for z := 0; z < v.sz; z++ {
			for x := 0; x < v.sx; x++ {
				for y := 0; y < v.sy; y++ {
					out[i] = ib[iv.index(0, y, z)]
					i++
				}
			}
		}
	case 1:
		if v.sy == 1 && v.minY == s.at {
			s.in.volume(c, out, v)
			return
		}
		iv := vtVolume{v.sx, 1, v.sz, v.minX, s.at, v.minZ, v.stepX, v.stepY, v.stepZ}
		ib := vdBuf(iv.size())
		s.in.volume(c, ib, iv)
		for z := 0; z < v.sz; z++ {
			for x := 0; x < v.sx; x++ {
				val := ib[iv.index(x, 0, z)]
				b := v.index(x, 0, z)
				for k := 0; k < v.sy; k++ {
					out[b+k] = val
				}
			}
		}
	default:
		if v.sz == 1 && v.minZ == s.at {
			s.in.volume(c, out, v)
			return
		}
		iv := vtVolume{v.sx, v.sy, 1, v.minX, v.minY, s.at, v.stepX, v.stepY, v.stepZ}
		ib := vdBuf(iv.size())
		s.in.volume(c, ib, iv)
		i := 0
		for z := 0; z < v.sz; z++ {
			for x := 0; x < v.sx; x++ {
				for y := 0; y < v.sy; y++ {
					out[i] = ib[iv.index(x, y, 0)]
					i++
				}
			}
		}
	}
}

// vdSliceXZ is SliceFunction.XzSampler.
type vdSliceXZ struct {
	in   vdSampler
	x, z int
}

func (s *vdSliceXZ) value(c *vdCtx, _, y, _ int) float32 { return s.in.value(c, s.x, y, s.z) }

func (s *vdSliceXZ) volume(c *vdCtx, out []float32, v vtVolume) {
	if v.sx == 1 && v.sz == 1 && v.minX == s.x && v.minZ == s.z {
		s.in.volume(c, out, v)
		return
	}
	iv := vtVolume{1, v.sy, 1, s.x, v.minY, s.z, v.stepX, v.stepY, v.stepZ}
	ib := vdBuf(iv.size())
	s.in.volume(c, ib, iv)
	for y := 0; y < v.sy; y++ {
		val := ib[iv.index(0, y, 0)]
		i := v.index(0, y, 0)
		for k := 0; k < v.sx*v.sz; k++ {
			out[i] = val
			i += v.sy
		}
	}
}

// vdFindTop is FindTopSurfaceFunction.Sampler (always under a y slice).
type vdFindTop struct {
	density, upper vdSampler
	lower, cell    int
}

func (s *vdFindTop) from(c *vdCtx, x, z int, upper float32) float32 {
	top := int(math.Floor(float64(upper/float32(s.cell)))) * s.cell
	if top <= s.lower {
		return float32(s.lower)
	}
	for y := top; y >= s.lower; y -= s.cell {
		if s.density.value(c, x, y, z) > 0 {
			return float32(y)
		}
	}
	return float32(s.lower)
}

func (s *vdFindTop) value(c *vdCtx, x, y, z int) float32 {
	return s.from(c, x, z, s.upper.value(c, x, y, z))
}

func (s *vdFindTop) volume(c *vdCtx, out []float32, v vtVolume) {
	if v.sy != 1 {
		panic("find_top_surface sampled with sizeY " + strconv.Itoa(v.sy))
	}
	s.upper.volume(c, out, v)
	i := 0
	for iz := 0; iz < v.sz; iz++ {
		bz := v.blockZ(iz)
		for ix := 0; ix < v.sx; ix++ {
			out[i] = s.from(c, v.blockX(ix), bz, out[i])
			i++
		}
	}
}

// vdDistance is DistanceToPointFunction.Sampler.
type vdDistance struct {
	x, y, z int
	metric  string
}

func (s *vdDistance) value(_ *vdCtx, x, y, z int) float32 {
	dx, dy, dz := float32(s.x-x), float32(s.y-y), float32(s.z-z)
	switch s.metric {
	case "euclidean_squared":
		return dx*dx + dy*dy + dz*dz
	case "manhattan":
		return vcAbs(dx) + vcAbs(dy) + vcAbs(dz)
	case "chebyshev":
		return vcMax(vcMax(vcAbs(dx), vcAbs(dy)), vcAbs(dz))
	}
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

func (s *vdDistance) volume(c *vdCtx, out []float32, v vtVolume) {
	i := 0
	for iz := 0; iz < v.sz; iz++ {
		for ix := 0; ix < v.sx; ix++ {
			for iy := 0; iy < v.sy; iy++ {
				out[i] = s.value(c, v.blockX(ix), v.blockY(iy), v.blockZ(iz))
				i++
			}
		}
	}
}

// ---- splines ----

// vdSplineSampler is SplineFunction.Sampler: the spline with each distinct
// coordinate function given an index, sampled once per point (the point
// pass) or once per volume, lazily (the volume pass).
type vdSplineSampler struct {
	root   *vdCSpline
	coords []vdSampler
}

type vdCSpline struct {
	constant bool
	value    float32
	coord    int
	locs     []float32
	derivs   []float32
	vals     []*vdCSpline
}

func (c *vdCompiler) buildSpline(s *vdSpline) (vdSampler, error) {
	ss := &vdSplineSampler{}
	index := map[*vdFn]int{}
	var conv func(s *vdSpline) (*vdCSpline, error)
	conv = func(s *vdSpline) (*vdCSpline, error) {
		if s.constant {
			return &vdCSpline{constant: true, value: s.value}, nil
		}
		i, ok := index[s.coord]
		if !ok {
			smp, err := c.build(s.coord)
			if err != nil {
				return nil, err
			}
			i = len(ss.coords)
			index[s.coord] = i
			ss.coords = append(ss.coords, smp)
		}
		out := &vdCSpline{coord: i, locs: s.locs, derivs: s.derivs}
		for _, v := range s.vals {
			cv, err := conv(v)
			if err != nil {
				return nil, err
			}
			out.vals = append(out.vals, cv)
		}
		return out, nil
	}
	root, err := conv(s)
	if err != nil {
		return nil, err
	}
	ss.root = root
	return ss, nil
}

// vdSplineInput reads a coordinate's value for the point being sampled.
type vdSplineInput func(coord int) float32

func vdLinearExtend(input float32, locs []float32, value float32, derivs []float32, i int) float32 {
	d := derivs[i]
	if d == 0 {
		return value
	}
	return value + d*(input-locs[i])
}

// sample is CubicSpline.Multipoint.sample.
func (s *vdCSpline) sample(in vdSplineInput) float32 {
	if s.constant {
		return s.value
	}
	input := in(s.coord)
	start := sort.Search(len(s.locs), func(i int) bool { return input < s.locs[i] }) - 1
	last := len(s.locs) - 1
	if start < 0 {
		return vdLinearExtend(input, s.locs, s.vals[0].sample(in), s.derivs, 0)
	}
	if start == last {
		return vdLinearExtend(input, s.locs, s.vals[last].sample(in), s.derivs, last)
	}
	x1, x2 := s.locs[start], s.locs[start+1]
	t := (input - x1) / (x2 - x1)
	d1, d2 := s.derivs[start], s.derivs[start+1]
	y1 := s.vals[start].sample(in)
	y2 := s.vals[start+1].sample(in)
	a := d1*(x2-x1) - (y2 - y1)
	b := -d2*(x2-x1) + (y2 - y1)
	return vnLerp(t, y1, y2) + t*(1-t)*vnLerp(t, a, b)
}

func (s *vdSplineSampler) value(c *vdCtx, x, y, z int) float32 {
	cached := make([]float32, len(s.coords))
	have := make([]bool, len(s.coords))
	return s.root.sample(func(i int) float32 {
		if !have[i] {
			cached[i] = s.coords[i].value(c, x, y, z)
			have[i] = true
		}
		return cached[i]
	})
}

func (s *vdSplineSampler) volume(c *vdCtx, out []float32, v vtVolume) {
	bufs := make([][]float32, len(s.coords))
	idx := 0
	in := func(i int) float32 {
		if bufs[i] == nil {
			bufs[i] = vdBuf(v.size())
			s.coords[i].volume(c, bufs[i], v)
		}
		return bufs[i][idx]
	}
	for idx = range out {
		out[idx] = s.root.sample(in)
	}
}

// ---- the blended noise and the End islands ----

// vdBlended is BlendedNoise's compiled sampler: lerp between the two limit
// noises by the clamped main noise.
func (rs *vtState) blendedNoise(xzScale, yScale, xzFactor, yFactor, smear float64) vdSampler {
	r := rs.createRandom("minecraft:terrain")
	xzMul, yMul := 684.412*xzScale, 684.412*yScale
	limitSmear := yMul * smear
	mainSmear := limitSmear / yFactor
	minLimit := vtBlendedFbm(r, -15, limitSmear, float64(float32(0.99998474)))
	maxLimit := vtBlendedFbm(r, -15, limitSmear, float64(float32(0.99998474)))
	main := vtBlendedFbm(r, -7, mainSmear, 12.75)
	choice := &vdClamp{in: &vdConstAdd{in: &vdNoise{n: main, xz: xzMul / xzFactor, ys: yMul / yFactor}, k: 0.5}, lo: 0, hi: 1}
	return &vdLerp{a: choice, first: &vdNoise{n: minLimit, xz: xzMul, ys: yMul}, second: &vdNoise{n: maxLimit, xz: xzMul, ys: yMul}}
}

// vdEndIslands is EndIslandFunction's sampler.
type vdEndIslands struct{ n *vtSimplex }

func (rs *vtState) endIslands() vdSampler {
	r := newVTLegacy(rs.seed)
	for i := 0; i < 17292; i++ {
		r.next(32)
	}
	return &vdEndIslands{newVTSimplex(r)}
}

func (s *vdEndIslands) height(sx, sz int) float32 {
	cx, cz := sx/2, sz/2
	subX, subZ := sx%2, sz%2
	doffs := float32(-100)
	for xo := -12; xo <= 12; xo++ {
		for zo := -12; zo <= 12; zo++ {
			tx, tz := int64(cx+xo), int64(cz+zo)
			if tx*tx+tz*tz > 4096 && s.n.get2(float64(tx), float64(tz)) < -0.9 {
				size := float32(math.Mod(float64(vcAbs(float32(tx))*3439+vcAbs(float32(tz))*147), 13)) + 9
				xd := float32(subX - xo*2)
				zd := float32(subZ - zo*2)
				nd := 100 - float32(math.Sqrt(float64(xd*xd+zd*zd)))*size
				nd = vdMthClamp(nd, -100, 80)
				doffs = vcMax(doffs, nd)
			}
		}
	}
	return doffs
}

func (s *vdEndIslands) value(_ *vdCtx, x, _, z int) float32 {
	return (s.height(x/8, z/8) - 8) / 128
}

func (s *vdEndIslands) volume(c *vdCtx, out []float32, v vtVolume) {
	for iz := 0; iz < v.sz; iz++ {
		for ix := 0; ix < v.sx; ix++ {
			val := s.value(c, v.blockX(ix), 0, v.blockZ(iz))
			b := v.index(ix, 0, iz)
			for k := 0; k < v.sy; k++ {
				out[b+k] = val
			}
		}
	}
}
