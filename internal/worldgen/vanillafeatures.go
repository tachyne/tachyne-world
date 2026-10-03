package worldgen

import (
	"encoding/json"
	"math"
	"strconv"
	"sync"
)

// The configured features the vanilla generator's decoration places
// (vanilladecorate.go hands each placement here): the selectors that pick
// between placed features, ores and scattered ores (OreFeature and
// ScatteredOreFeature, draw for draw), single blocks from a block state
// provider, and trees, huge mushrooms and fallen trees through the
// engine's tree placers (treeplacer.go) drawing from the feature's own
// stream. Every feature draws from the stream it is handed, so the next
// placement of the same placed feature continues where vanilla's would.
//
// A feature reads the terrain before features (VanillaTerrain) and writes
// only the chunk being generated: each chunk replays its neighbours'
// decoration and keeps its own part, so a feature straddling a border is
// drawn the same way in every chunk that holds a piece of it.

// vpFeature is a configured feature: its type and its config.
type vpFeature struct {
	name string // the registry name ("" inline)
	typ  string
	raw  map[string]json.RawMessage

	once   sync.Once
	ore    *vpOreCfg
	sel    []vpSelEntry
	def    *vpPlacedFeature
	choose []*vpPlacedFeature
	block  vpStateProvider
	ext    *vpExtra
	ext3   *vpExtra3
}

type vpSelEntry struct {
	chance float32
	weight int
	pf     *vpPlacedFeature
}

var (
	vpFeatMu       sync.Mutex
	vpFeatCache    = map[string]*vpFeature{}
	vpInlinePlaced = map[string]*vpPlacedFeature{}
)

// vpFeatureRef resolves a configured feature: a registry name or an
// inline definition.
func vpFeatureRef(raw json.RawMessage) *vpFeature {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return vpFeatureNamed(vpShort(name))
	}
	key := string(raw)
	vpFeatMu.Lock()
	defer vpFeatMu.Unlock()
	if f, ok := vpFeatCache[key]; ok {
		return f
	}
	f := vpNewFeature("", raw)
	vpFeatCache[key] = f
	return f
}

func vpFeatureNamed(name string) *vpFeature {
	vpFeatMu.Lock()
	defer vpFeatMu.Unlock()
	if f, ok := vpFeatCache["#"+name]; ok {
		return f
	}
	raw, ok := mustVPData().Features[name]
	if !ok {
		return nil
	}
	f := vpNewFeature(name, raw)
	vpFeatCache["#"+name] = f
	return f
}

func vpNewFeature(name string, raw json.RawMessage) *vpFeature {
	f := &vpFeature{name: name}
	if json.Unmarshal(raw, &f.raw) != nil {
		return f
	}
	var t string
	_ = json.Unmarshal(f.raw["type"], &t)
	f.typ = vpShort(t)
	return f
}

// vpPlacedRef resolves a placed feature: a registry name or an inline
// {feature, placement}.
func vpPlacedRef(raw json.RawMessage) *vpPlacedFeature {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return mustVPData().Placed[vpShort(name)]
	}
	key := string(raw)
	vpFeatMu.Lock()
	if pf, ok := vpInlinePlaced[key]; ok {
		vpFeatMu.Unlock()
		return pf
	}
	vpFeatMu.Unlock()
	var in struct {
		Feature   json.RawMessage   `json:"feature"`
		Placement []json.RawMessage `json:"placement"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	pf := &vpPlacedFeature{Inline: in.Feature}
	var fname string
	if json.Unmarshal(in.Feature, &fname) == nil {
		pf.Feature = vpShort(fname)
	}
	for _, m := range in.Placement {
		mod, err := parseVPModifier(m)
		if err != nil {
			return nil
		}
		pf.Modifiers = append(pf.Modifiers, mod)
	}
	vpFeatMu.Lock()
	vpInlinePlaced[key] = pf
	vpFeatMu.Unlock()
	return pf
}

// feature is the placed feature's configured feature.
func (pf *vpPlacedFeature) feature() *vpFeature {
	if pf.Feature != "" {
		return vpFeatureNamed(pf.Feature)
	}
	return vpFeatureRef(pf.Inline)
}

// vpExec places features into one chunk.
type vpExec struct {
	ch           *Chunk
	baseX, baseZ int
	ctx          *vpCtx // the placement context (terrain reads)
	terrain      VanillaTerrain
	// unhandled counts placements of feature types not implemented here.
	unhandled map[string]int
	// protect marks the chunk's cells a structure built (section-major,
	// as Chunk.Sections): features leave them be.
	protect []bool
	// cold is Biome.coldEnoughToSnow at a position (nil: never).
	cold func(biome string, x, y, z int) bool
	// ov is what the feature being placed has written, in the chunk or
	// beyond it: a feature reads its own blocks back, as it would in a
	// world, and a chunk replaying a neighbour's feature reads them too.
	ov map[vpPos]uint32
	// g is the generator, for the engine's End decorators (nil in tests).
	g *Generator
}

// vpOverlayLevel is the terrain with the placing feature's own writes.
type vpOverlayLevel struct {
	vpLevel
	e *vpExec
}

func (l vpOverlayLevel) Block(x, y, z int) uint32 {
	if s, ok := l.e.ov[vpPos{x, y, z}]; ok {
		return s
	}
	return l.vpLevel.Block(x, y, z)
}

// beginFeature starts a top-level placement: the overlay of the previous
// one is dropped.
func (e *vpExec) beginFeature() {
	if len(e.ov) > 0 {
		e.ov = nil
	}
}

func (e *vpExec) inChunk(x, y, z int) bool {
	lx, lz := x-e.baseX, z-e.baseZ
	return lx >= 0 && lx < 16 && lz >= 0 && lz < 16 && y >= MinY && y < MinY+len(e.ch.Sections)*16
}

// set records a feature's write and makes it in the chunk if it lies
// there and no structure holds the cell.
func (e *vpExec) set(x, y, z int, s uint32) {
	if e.ov == nil {
		e.ov = map[vpPos]uint32{}
	}
	e.ov[vpPos{x, y, z}] = s
	if !e.inChunk(x, y, z) {
		return
	}
	lx, lz := x-e.baseX, z-e.baseZ
	if e.protect != nil {
		dy := y - MinY
		if e.protect[(dy>>4)*4096+((dy&15)*16+lz)*16+lx] {
			return
		}
	}
	setSectionBlock(e.ch, lx, y, lz, s, true)
}

// get reads the terrain before features.
func (e *vpExec) get(x, y, z int) uint32 { return e.ctx.lv.Block(x, y, z) }

// placePlaced is PlacedFeature.place: the modifiers without a biome check,
// then the feature at each position.
func (e *vpExec) placePlaced(pf *vpPlacedFeature, r *vwRandom, p vpPos) bool {
	if pf == nil {
		return false
	}
	c := *e.ctx
	c.top = nil
	any := false
	vpPlace(&c, pf, r, p, func(q vpPos) {
		if e.place(pf.feature(), r, q) {
			any = true
		}
	})
	return any
}

// place is Feature.place for a configured feature.
func (e *vpExec) place(f *vpFeature, r *vwRandom, p vpPos) bool {
	if f == nil {
		return false
	}
	f.once.Do(f.parse)
	switch f.typ {
	case "random_selector":
		for _, s := range f.sel {
			if r.nextFloat() < s.chance {
				return e.placePlaced(s.pf, r, p)
			}
		}
		return e.placePlaced(f.def, r, p)
	case "simple_random_selector":
		if len(f.choose) == 0 {
			return false
		}
		return e.placePlaced(f.choose[r.nextIntN(int32(len(f.choose)))], r, p)
	case "random_boolean_selector":
		if r.nextBool() {
			return e.placePlaced(f.choose[0], r, p)
		}
		return e.placePlaced(f.choose[1], r, p)
	case "weighted_random_selector":
		total := 0
		for _, s := range f.sel {
			total += s.weight
		}
		if total == 0 {
			return false
		}
		pick := int(r.nextIntN(int32(total)))
		for _, s := range f.sel {
			if pick -= s.weight; pick < 0 {
				return e.placePlaced(s.pf, r, p)
			}
		}
		return false
	case "sequence":
		for _, pf := range f.choose {
			if !e.placePlaced(pf, r, p) {
				return false
			}
		}
		return true
	case "ore":
		return e.placeOre(f.ore, r, p)
	case "scattered_ore":
		return e.placeScatteredOre(f.ore, r, p)
	case "simple_block":
		return e.placeSimpleBlock(f, r, p)
	case "tree":
		if c := TreeFeatures[f.name]; c != nil {
			e.placeTree(r, p, func(d TreeDriver) { PlaceTree(c, p.x, p.y, p.z, vpTreeRNG{r}, d) })
			return true
		}
	case "fallen_tree":
		if c := FallenTrees[f.name]; c != nil {
			e.placeTree(r, p, func(d TreeDriver) { PlaceFallenTree(c, p.x, p.y, p.z, vpTreeRNG{r}, d) })
			return true
		}
	case "huge_brown_mushroom", "huge_red_mushroom":
		brown := f.typ == "huge_brown_mushroom"
		e.placeTree(r, p, func(d TreeDriver) { PlaceHugeMushroom(brown, p.x, p.y, p.z, vpTreeRNG{r}, d) })
		return true
	}
	if placed, ok := e.placeExtra3(f, r, p); ok {
		return placed
	}
	if placed, ok := e.placeExtra(f, r, p); ok {
		return placed
	}
	if e.unhandled != nil {
		e.unhandled[f.typ]++
	}
	return false
}

// parse reads the parts of a config its type needs.
func (f *vpFeature) parse() {
	placedList := func(k string) []*vpPlacedFeature {
		var raws []json.RawMessage
		_ = json.Unmarshal(f.raw[k], &raws)
		var out []*vpPlacedFeature
		for _, r := range raws {
			out = append(out, vpPlacedRef(r))
		}
		return out
	}
	switch f.typ {
	case "random_selector":
		var ents []struct {
			Chance  float32         `json:"chance"`
			Feature json.RawMessage `json:"feature"`
		}
		_ = json.Unmarshal(f.raw["features"], &ents)
		for _, en := range ents {
			f.sel = append(f.sel, vpSelEntry{chance: en.Chance, pf: vpPlacedRef(en.Feature)})
		}
		f.def = vpPlacedRef(f.raw["default"])
	case "simple_random_selector", "sequence", "overlay":
		f.choose = placedList("features")
	case "random_boolean_selector":
		f.choose = []*vpPlacedFeature{vpPlacedRef(f.raw["feature_true"]), vpPlacedRef(f.raw["feature_false"])}
	case "weighted_random_selector":
		var ents []struct {
			Data   json.RawMessage `json:"data"`
			Weight int             `json:"weight"`
		}
		_ = json.Unmarshal(f.raw["features"], &ents)
		for _, en := range ents {
			f.sel = append(f.sel, vpSelEntry{weight: en.Weight, pf: vpPlacedRef(en.Data)})
		}
	case "ore", "scattered_ore":
		f.ore = parseVPOre(f.raw)
	case "simple_block":
		f.block = parseVPStateProvider(f.raw["to_place"])
	default:
		if !f.parseExtra3() {
			f.parseExtra()
		}
	}
}

// ---- ores ---------------------------------------------------------------------

// vpRuleTest is a RuleTest (an ore target's predicate on the replaced
// state; random_block_match draws).
type vpRuleTest func(s uint32, y int, r *vwRandom) bool

type vpOreTarget struct {
	test  vpRuleTest
	state uint32
}

type vpOreCfg struct {
	targets []vpOreTarget
	size    int
	discard float32
}

func parseVPRule(raw json.RawMessage) vpRuleTest {
	var o map[string]json.RawMessage
	if json.Unmarshal(raw, &o) != nil {
		return func(uint32, int, *vwRandom) bool { return false }
	}
	var t string
	_ = json.Unmarshal(o["predicate_type"], &t)
	list := func() []vpRuleTest {
		var raws []json.RawMessage
		_ = json.Unmarshal(o["rules"], &raws)
		var out []vpRuleTest
		for _, r := range raws {
			out = append(out, parseVPRule(r))
		}
		return out
	}
	switch vpShort(t) {
	case "always_true":
		return func(uint32, int, *vwRandom) bool { return true }
	case "tag_match":
		var tag string
		_ = json.Unmarshal(o["tag"], &tag)
		rs := vpTagRangesOf(vpShort(tag))
		return func(s uint32, _ int, _ *vwRandom) bool { return vpInRanges(rs, s) }
	case "block_match":
		var b string
		_ = json.Unmarshal(o["block"], &b)
		rs := vpBlockRanges([]string{b})
		return func(s uint32, _ int, _ *vwRandom) bool { return vpInRanges(rs, s) }
	case "random_block_match":
		var b string
		var p float32
		_ = json.Unmarshal(o["block"], &b)
		_ = json.Unmarshal(o["probability"], &p)
		rs := vpBlockRanges([]string{b})
		return func(s uint32, _ int, r *vwRandom) bool { return vpInRanges(rs, s) && r.nextFloat() < p }
	case "height_match":
		var lo, hi int
		_ = json.Unmarshal(o["min_inclusive"], &lo)
		_ = json.Unmarshal(o["max_inclusive"], &hi)
		return func(_ uint32, y int, _ *vwRandom) bool { return lo <= y && y <= hi }
	case "not":
		in := parseVPRule(o["rule"])
		return func(s uint32, y int, r *vwRandom) bool { return !in(s, y, r) }
	case "any_of":
		rules := list()
		return func(s uint32, y int, r *vwRandom) bool {
			for _, t := range rules {
				if t(s, y, r) {
					return true
				}
			}
			return false
		}
	case "all_of":
		rules := list()
		return func(s uint32, y int, r *vwRandom) bool {
			for _, t := range rules {
				if !t(s, y, r) {
					return false
				}
			}
			return true
		}
	}
	return func(uint32, int, *vwRandom) bool { return false }
}

func parseVPOre(raw map[string]json.RawMessage) *vpOreCfg {
	c := &vpOreCfg{}
	_ = json.Unmarshal(raw["size"], &c.size)
	_ = json.Unmarshal(raw["discard_chance_on_air_exposure"], &c.discard)
	var ts []struct {
		State  json.RawMessage `json:"state"`
		Target json.RawMessage `json:"target"`
	}
	_ = json.Unmarshal(raw["targets"], &ts)
	for _, t := range ts {
		c.targets = append(c.targets, vpOreTarget{test: parseVPRule(t.Target), state: vpParseState(t.State)})
	}
	return c
}

// canPlaceOre is AbstractOreFeature.canPlaceOre.
func (e *vpExec) canPlaceOre(c *vpOreCfg, s uint32, t vpOreTarget, x, y, z int, r *vwRandom) bool {
	if !t.test(s, y, r) {
		return false
	}
	switch {
	case c.discard <= 0:
		return true
	case c.discard < 1 && r.nextFloat() >= c.discard:
		return true
	}
	for _, d := range [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		if e.get(x+d[0], y+d[1], z+d[2]) == Air {
			return false
		}
	}
	return true
}

// writable is WorldGenRegion.ensureCanWrite for a decoration that started
// in chunk (ox, oz): the 3x3 chunks round it, inside the world's height.
func (e *vpExec) writable(ox, oz, x, y, z int) bool {
	dx, dz := (x>>4)-(ox>>4), (z>>4)-(oz>>4)
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 && !e.ctx.outside(y)
}

// placeOre is OreFeature.place.
func (e *vpExec) placeOre(c *vpOreCfg, r *vwRandom, p vpPos) bool {
	size := float32(c.size)
	dir := r.nextFloat() * float32(math.Pi)
	spread := size / 8
	maxR := int(math.Ceil(float64((size/16*2 + 1) / 2)))
	sinD, cosD := math.Sin(float64(dir)), math.Cos(float64(dir))
	x0, x1 := float64(p.x)+sinD*float64(spread), float64(p.x)-sinD*float64(spread)
	z0, z1 := float64(p.z)+cosD*float64(spread), float64(p.z)-cosD*float64(spread)
	y0 := float64(p.y + int(r.nextIntN(3)) - 2)
	y1 := float64(p.y + int(r.nextIntN(3)) - 2)
	cs := int(math.Ceil(float64(spread)))
	xs, ys, zs := p.x-cs-maxR, p.y-2-maxR, p.z-cs-maxR
	sxz, sy := 2*(cs+maxR), 2*(2+maxR)
	for x := xs; x <= xs+sxz; x++ {
		for z := zs; z <= zs+sxz; z++ {
			if ys <= e.ctx.lv.Height(HeightOceanFloorWG, x, z) {
				return e.doPlaceOre(c, r, p, x0, x1, z0, z1, y0, y1, xs, ys, zs, sxz, sy)
			}
		}
	}
	return false
}

func (e *vpExec) doPlaceOre(c *vpOreCfg, r *vwRandom, origin vpPos, x0, x1, z0, z1, y0, y1 float64, xs, ys, zs, sxz, sy int) bool {
	n := c.size
	data := make([]float64, n*4)
	for i := 0; i < n; i++ {
		step := float32(i) / float32(n)
		ss := r.nextDouble() * float64(n) / 16
		rad := (float64(mthSin(float64(float32(math.Pi)*step))+1)*ss + 1) / 2
		data[i*4] = x0 + float64(step)*(x1-x0)
		data[i*4+1] = y0 + float64(step)*(y1-y0)
		data[i*4+2] = z0 + float64(step)*(z1-z0)
		data[i*4+3] = rad
	}
	for i := 0; i < n-1; i++ {
		if data[i*4+3] <= 0 {
			continue
		}
		for j := i + 1; j < n; j++ {
			if data[j*4+3] <= 0 {
				continue
			}
			dx, dy, dz := data[i*4]-data[j*4], data[i*4+1]-data[j*4+1], data[i*4+2]-data[j*4+2]
			dr := data[i*4+3] - data[j*4+3]
			if dr*dr > dx*dx+dy*dy+dz*dz {
				if dr > 0 {
					data[j*4+3] = -1
				} else {
					data[i*4+3] = -1
				}
			}
		}
	}
	// tested is OreFeature's bit set, with its index (the box's far faces
	// alias cells a row on, as vanilla's do).
	tested := make([]bool, sxz+sy*sxz+sxz*sxz*sy+sxz+1)
	placed := 0
	for i := 0; i < n; i++ {
		rad := data[i*4+3]
		if rad < 0 {
			continue
		}
		xx, yy, zz := data[i*4], data[i*4+1], data[i*4+2]
		xMin := max(int(math.Floor(xx-rad)), xs)
		yMin := max(int(math.Floor(yy-rad)), ys)
		zMin := max(int(math.Floor(zz-rad)), zs)
		xMax := max(int(math.Floor(xx+rad)), xMin)
		yMax := max(int(math.Floor(yy+rad)), yMin)
		zMax := max(int(math.Floor(zz+rad)), zMin)
		for x := xMin; x <= xMax; x++ {
			xd := (float64(x) + 0.5 - xx) / rad
			if xd*xd >= 1 {
				continue
			}
			for y := yMin; y <= yMax; y++ {
				yd := (float64(y) + 0.5 - yy) / rad
				if xd*xd+yd*yd >= 1 {
					continue
				}
				for z := zMin; z <= zMax; z++ {
					zd := (float64(z) + 0.5 - zz) / rad
					if xd*xd+yd*yd+zd*zd >= 1 || e.ctx.outside(y) {
						continue
					}
					bi := x - xs + (y-ys)*sxz + (z-zs)*sxz*sy
					if bi < 0 || bi >= len(tested) || tested[bi] {
						continue
					}
					tested[bi] = true
					if !e.writable(origin.x, origin.z, x, y, z) {
						continue
					}
					s := e.get(x, y, z)
					for _, t := range c.targets {
						if e.canPlaceOre(c, s, t, x, y, z, r) {
							e.set(x, y, z, t.state)
							placed++
							break
						}
					}
				}
			}
		}
	}
	return placed > 0
}

// placeScatteredOre is ScatteredOreFeature.place.
func (e *vpExec) placeScatteredOre(c *vpOreCfg, r *vwRandom, p vpPos) bool {
	tries := int(r.nextIntN(int32(c.size + 1)))
	off := func(d int) int {
		f := r.nextFloat() - r.nextFloat()
		return int(math.Floor(float64(f*float32(d)) + 0.5))
	}
	for i := 0; i < tries; i++ {
		d := min(i, 7)
		dx := off(d)
		dy := off(d)
		dz := off(d)
		x, y, z := p.x+dx, p.y+dy, p.z+dz
		s := e.get(x, y, z)
		for _, t := range c.targets {
			if e.canPlaceOre(c, s, t, x, y, z, r) {
				e.set(x, y, z, t.state)
				break
			}
		}
	}
	return true
}

// ---- single blocks ---------------------------------------------------------------

// vpStateProvider is a BlockStateProvider: getOptionalState (ok false:
// none). getState is vpGetState.
type vpStateProvider func(c *vpCtx, r *vwRandom, p vpPos) (uint32, bool)

// vpGetState is BlockStateProvider.getState: the provider's state, or the
// block already there.
func vpGetState(pr vpStateProvider, c *vpCtx, r *vwRandom, p vpPos) uint32 {
	if pr != nil {
		if s, ok := pr(c, r, p); ok {
			return s
		}
	}
	return c.blockAt(p)
}

// vpParseState reads a block state: "minecraft:x" or {id, properties} —
// the block's DEFAULT state with the listed properties set (as the state
// codec reads it); air for a block the engine does not know.
func vpParseState(raw json.RawMessage) uint32 {
	var name string
	var props map[string]string
	if json.Unmarshal(raw, &name) != nil {
		var o struct {
			ID    string            `json:"id"`
			Name  string            `json:"Name"`
			Props map[string]string `json:"properties"`
		}
		_ = json.Unmarshal(raw, &o)
		name, props = o.ID, o.Props
		if name == "" {
			name = o.Name
		}
	}
	return vpStateOf(vpShort(name), props)
}

// vpStateOf is a block's default state with properties set.
func vpStateOf(name string, props map[string]string) uint32 {
	s, ok := blockDefaultState[name]
	if !ok {
		return Air
	}
	if len(props) == 0 {
		return s
	}
	info, ok := InfoForState(s)
	if !ok {
		return s
	}
	for k, v := range props {
		if info.HasProperty(k) {
			s = SetProperty(info, s, k, v)
		}
	}
	return s
}

func parseVPStateProvider(raw json.RawMessage) vpStateProvider {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		// A name is a registered provider (worldgen/block_state_provider).
		if pr, ok := mustVPData().StateProviders[vpShort(name)]; ok {
			return parseVPStateProvider(pr)
		}
		s := vpParseState(raw)
		return func(*vpCtx, *vwRandom, vpPos) (uint32, bool) { return s, true }
	}
	var o map[string]json.RawMessage
	_ = json.Unmarshal(raw, &o)
	var t string
	_ = json.Unmarshal(o["type"], &t)
	switch vpShort(t) {
	case "":
		s := vpParseState(raw)
		return func(*vpCtx, *vwRandom, vpPos) (uint32, bool) { return s, true }
	case "simple", "simple_state_provider":
		s := vpParseState(o["state"])
		return func(*vpCtx, *vwRandom, vpPos) (uint32, bool) { return s, true }
	case "weighted":
		var ents []struct {
			Data   json.RawMessage `json:"data"`
			Weight int             `json:"weight"`
		}
		_ = json.Unmarshal(o["entries"], &ents)
		states := make([]uint32, len(ents))
		total := 0
		for i, en := range ents {
			states[i] = vpParseState(en.Data)
			total += en.Weight
		}
		return func(_ *vpCtx, r *vwRandom, _ vpPos) (uint32, bool) {
			if total == 0 {
				return 0, false
			}
			pick := int(r.nextIntN(int32(total)))
			for i, en := range ents {
				if pick -= en.Weight; pick < 0 {
					return states[i], true
				}
			}
			return 0, false
		}
	case "randomized_int":
		src := parseVPStateProvider(o["source"])
		var prop string
		_ = json.Unmarshal(o["property"], &prop)
		vals, err := parseVPInt(o["values"])
		return func(c *vpCtx, r *vwRandom, p vpPos) (uint32, bool) {
			s, ok := src(c, r, p)
			if !ok || err != nil {
				return s, ok
			}
			v := vals.sample(r)
			if info, ok := InfoForState(s); ok && info.HasProperty(prop) {
				return SetProperty(info, s, prop, strconv.Itoa(v)), true
			}
			return s, true
		}
	case "rule_based":
		type rule struct {
			test vpPredicate
			then vpStateProvider
		}
		var rs []struct {
			If   json.RawMessage `json:"if_true"`
			Then json.RawMessage `json:"then"`
		}
		_ = json.Unmarshal(o["rules"], &rs)
		var rules []rule
		for _, r := range rs {
			t, err := parseVPPredicate(r.If)
			if err != nil {
				continue
			}
			rules = append(rules, rule{t, parseVPStateProvider(r.Then)})
		}
		var fallback vpStateProvider
		if f, ok := o["fallback"]; ok {
			fallback = parseVPStateProvider(f)
		}
		return func(c *vpCtx, r *vwRandom, p vpPos) (uint32, bool) {
			for _, ru := range rules {
				if ru.test.test(c, p) {
					if s, ok := ru.then(c, r, p); ok {
						return s, true
					}
				}
			}
			if fallback != nil {
				return fallback(c, r, p)
			}
			return 0, false
		}
	case "rotated":
		src := parseVPStateProvider(o["state"])
		var dir string
		_ = json.Unmarshal(o["direction"], &dir)
		return func(c *vpCtx, r *vwRandom, p vpPos) (uint32, bool) {
			d := dir
			if d == "" {
				d = [6]string{"down", "up", "north", "south", "west", "east"}[r.nextIntN(6)]
			}
			s := vpGetState(src, c, r, p)
			info, ok := InfoForState(s)
			if !ok {
				return s, true
			}
			axis := map[string]string{"down": "y", "up": "y", "north": "z", "south": "z", "west": "x", "east": "x"}[d]
			if info.HasProperty("axis") {
				s = SetProperty(info, s, "axis", axis)
			}
			if info.HasProperty("facing") {
				s = SetProperty(info, s, "facing", d)
			}
			return s, true
		}
	case "random_block":
		bl, tag := []string(nil), ""
		var one string
		if json.Unmarshal(o["blocks"], &one) == nil {
			if len(one) > 0 && one[0] == '#' {
				tag = vpShort(one[1:])
			} else {
				bl = []string{vpShort(one)}
			}
		} else {
			_ = json.Unmarshal(o["blocks"], &bl)
		}
		if tag != "" {
			vpLoadTags()
			bl = vpBlockTags[tag]
		}
		return func(_ *vpCtx, r *vwRandom, _ vpPos) (uint32, bool) {
			if len(bl) == 0 {
				return 0, false
			}
			return vpStateOf(vpShort(bl[r.nextIntN(int32(len(bl)))]), nil), true
		}
	case "copy_properties":
		src := parseVPStateProvider(o["source"])
		return func(c *vpCtx, r *vwRandom, p vpPos) (uint32, bool) {
			s := vpGetState(src, c, r, p)
			old := c.blockAt(p)
			if oi, ok := InfoForState(old); ok {
				if ni, ok := InfoForState(s); ok {
					for k, v := range StateProps(old) {
						if ni.HasProperty(k) && oi.HasProperty(k) {
							s = SetProperty(ni, s, k, v)
						}
					}
				}
			}
			return s, true
		}
	case "noise_threshold", "noise", "dual_noise":
		// The noise providers' NormalNoise (seeded by a legacy source) is
		// the terrain core's; until it is shared, these pick by the
		// biome-info noise between their states.
		var states []json.RawMessage
		for _, k := range []string{"states", "high_states", "low_states"} {
			var ss []json.RawMessage
			if json.Unmarshal(o[k], &ss) == nil {
				states = append(states, ss...)
			}
		}
		if d, ok := o["default_state"]; ok {
			states = append([]json.RawMessage{d}, states...)
		}
		parsed := make([]uint32, len(states))
		for i, s := range states {
			parsed[i] = vpParseState(s)
		}
		return func(_ *vpCtx, _ *vwRandom, p vpPos) (uint32, bool) {
			if len(parsed) == 0 {
				return 0, false
			}
			n := float64(vpBiomeInfoNoise().get2(float64(p.x)/48, float64(p.z)/48))
			i := int((n + 1) / 2 * float64(len(parsed)))
			return parsed[min(max(i, 0), len(parsed)-1)], true
		}
	}
	return func(*vpCtx, *vwRandom, vpPos) (uint32, bool) { return 0, false }
}

// placeSimpleBlock is SimpleBlockFeature.place: the provider's state, if
// it can survive there (on supporting ground, for plants), a double plant
// with its upper half.
func (e *vpExec) placeSimpleBlock(f *vpFeature, r *vwRandom, p vpPos) bool {
	if f.block == nil {
		return false
	}
	s, ok := f.block(e.ctx, r, p)
	if !ok {
		return false
	}
	if !e.canSurvive(s, p) {
		return false
	}
	if upper, double := vpDoubleUpper(s); double {
		above := e.get(p.x, p.y+1, p.z)
		if above != Air && (vpFluidOf(above) != vpFluidOf(s) || !IsReplaceable(above)) {
			return false
		}
		e.set(p.x, p.y, p.z, s)
		e.set(p.x, p.y+1, p.z, upper)
		return true
	}
	e.set(p.x, p.y, p.z, s)
	return true
}

// vpDoubleUpper is a double plant's upper half for its lower-half state.
func vpDoubleUpper(s uint32) (uint32, bool) {
	info, ok := InfoForState(s)
	if !ok || !info.HasProperty("half") {
		return 0, false
	}
	if GetProperty(info, s, "half") != "lower" {
		return 0, false
	}
	return SetProperty(info, s, "half", "upper"), true
}

// canSurvive approximates BlockState.canSurvive for what simple_block
// places: plants on ground that supports vegetation (sugar cane and cactus
// by their own rules, water plants in water), anything else anywhere.
func (e *vpExec) canSurvive(s uint32, p vpPos) bool {
	name, _ := StateName(s)
	c := *e.ctx
	switch name {
	case "cactus", "sugar_cane", "mangrove_propagule":
		return vpWouldSurvive{block: name}.test(&c, p)
	case "seagrass", "tall_seagrass", "kelp", "kelp_plant", "sea_pickle":
		return HoldsWater(e.get(p.x, p.y, p.z))
	}
	if NeedsGroundSupport(s) || IsFlower(s) {
		return vpWouldSurvive{block: name}.test(&c, p)
	}
	return true
}

// ---- trees ---------------------------------------------------------------------

// vpTreeRNG draws a tree from its feature's stream.
type vpTreeRNG struct{ r *vwRandom }

func (t vpTreeRNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(t.r.nextIntN(int32(n)))
}

func (t vpTreeRNG) Float64() float64 { return float64(t.r.nextFloat()) }

// placeTree grows a tree-like feature through the engine's placers: reads
// from the terrain before features (identical in every chunk that holds a
// piece of the tree), writes clipped to this chunk.
func (e *vpExec) placeTree(r *vwRandom, p vpPos, grow func(TreeDriver)) {
	surface := func(x, z int) int { return e.ctx.lv.Height(HeightWorldSurfaceWG, x, z) }
	set := func(x, y, z int, state uint32, leaf bool) {
		if !e.inChunk(x, y, z) {
			return
		}
		if leaf {
			cur := sectionBlockAt(e.ch, x-e.baseX, y, z-e.baseZ)
			if cur != Air && !IsLeaves(cur) {
				return
			}
		}
		e.set(x, y, z, state)
	}
	read := func(x, y, z int) uint32 {
		if e.inChunk(x, y, z) {
			return sectionBlockAt(e.ch, x-e.baseX, y, z-e.baseZ)
		}
		return e.get(x, y, z)
	}
	grow(TreeDriver{
		Set: set,
		Free: func(x, y, z int) bool {
			return y >= surface(x, z) || IsReplaceable(e.get(x, y, z)) && !IsFluid(e.get(x, y, z))
		},
		Read:        read,
		DirtGround:  func(x, y, z int) bool { return IsDirtTag(e.get(x, y, z)) },
		SurfaceTop:  surface,
		RootThrough: func(x, y, z int) bool { return IsRootGrowThrough(e.get(x, y, z)) },
	})
}
