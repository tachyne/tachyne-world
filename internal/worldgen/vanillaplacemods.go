package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

// Vanilla's placement modifiers (PlacedFeature.placement) and the value
// providers and block predicates they take, for the vanilla generator's
// decoration (vanilladecorate.go). Each draws from the WorldgenRandom in
// vanilla's order, so a feature's positions follow its seed exactly.

// vpPos is a block position.
type vpPos struct{ x, y, z int }

// vpLevel is what placement reads of the world (PlacementContext's level):
// blocks, the heightmaps and the biome a block sits in (short names:
// "plains").
type vpLevel interface {
	Block(x, y, z int) uint32
	Height(hm HeightmapType, x, z int) int
	Biome(x, y, z int) string
}

// vpCtx is a PlacementContext.
type vpCtx struct {
	lv vpLevel
	// minY and height are the WorldGenerationContext's (the overworld's
	// -64 and 384); seaLevel resolves relative_to_sea_level anchors.
	minY, height, seaLevel int
	// top is the placed feature being biome-checked (nil: placed by
	// another feature, so never biome-filtered), and hasFeature answers
	// BiomeGenerationSettings.hasFeature.
	top        *vpPlacedFeature
	hasFeature func(biome, placed string) bool
}

func (c *vpCtx) outside(y int) bool { return y < c.minY || y >= c.minY+c.height }

// vpModifier is a PlacementModifier.
type vpModifier interface {
	modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos))
}

// ---- value providers --------------------------------------------------------

// vpIntProvider is an IntProvider.
type vpIntProvider interface{ sample(r *vwRandom) int }

type vpConstInt int
type vpUniformInt struct{ lo, hi int }
type vpBiasedInt struct{ lo, hi int }
type vpClampedInt struct {
	src    vpIntProvider
	lo, hi int
}
type vpClampedNormalInt struct{ mean, dev, lo, hi float32 }
type vpWeightedInt struct {
	items  []vpIntProvider
	weight []int
	total  int
}
type vpTrapezoidInt struct{ lo, hi, plateau int }

func (v vpConstInt) sample(*vwRandom) int { return int(v) }
func (v vpUniformInt) sample(r *vwRandom) int {
	return int(r.nextIntN(int32(v.hi-v.lo+1))) + v.lo
}
func (v vpBiasedInt) sample(r *vwRandom) int {
	return v.lo + int(r.nextIntN(r.nextIntN(int32(v.hi-v.lo+1))+1))
}
func (v vpClampedInt) sample(r *vwRandom) int {
	return min(max(v.src.sample(r), v.lo), v.hi)
}
func (v vpClampedNormalInt) sample(r *vwRandom) int {
	n := v.mean + float32(r.nextGaussian())*v.dev
	if n < v.lo {
		n = v.lo
	}
	if n > v.hi {
		n = v.hi
	}
	return int(n)
}
func (v vpWeightedInt) sample(r *vwRandom) int {
	sel := int(r.nextIntN(int32(v.total)))
	for i, w := range v.weight {
		sel -= w
		if sel < 0 {
			return v.items[i].sample(r)
		}
	}
	return v.items[len(v.items)-1].sample(r)
}
func (v vpTrapezoidInt) sample(r *vwRandom) int {
	if v.plateau == 0 && v.hi == -v.lo {
		return int(r.nextIntN(int32(v.hi+1))) - int(r.nextIntN(int32(v.hi+1)))
	}
	rng := v.hi - v.lo
	if v.plateau == rng {
		return int(r.nextIntN(int32(rng+1))) + v.lo
	}
	ps := (rng - v.plateau) / 2
	pe := rng - ps
	return v.lo + int(r.nextIntN(int32(pe+1))) + int(r.nextIntN(int32(ps+1)))
}

func parseVPInt(raw json.RawMessage) (vpIntProvider, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return vpConstInt(n), nil
	}
	var o struct {
		Type         string          `json:"type"`
		Value        *int            `json:"value"`
		Min          int             `json:"min_inclusive"`
		Max          int             `json:"max_inclusive"`
		Source       json.RawMessage `json:"source"`
		Mean         float32         `json:"mean"`
		Dev          float32         `json:"deviation"`
		Plateau      int             `json:"plateau"`
		TMin         int             `json:"min"`
		TMax         int             `json:"max"`
		Distribution []struct {
			Data   json.RawMessage `json:"data"`
			Weight int             `json:"weight"`
		} `json:"distribution"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	switch vpShort(o.Type) {
	case "constant":
		if o.Value == nil {
			return nil, fmt.Errorf("constant int without a value")
		}
		return vpConstInt(*o.Value), nil
	case "uniform":
		return vpUniformInt{o.Min, o.Max}, nil
	case "biased_to_bottom":
		return vpBiasedInt{o.Min, o.Max}, nil
	case "clamped":
		src, err := parseVPInt(o.Source)
		if err != nil {
			return nil, err
		}
		return vpClampedInt{src, o.Min, o.Max}, nil
	case "clamped_normal":
		return vpClampedNormalInt{o.Mean, o.Dev, float32(o.Min), float32(o.Max)}, nil
	case "weighted_list":
		w := vpWeightedInt{}
		for _, d := range o.Distribution {
			p, err := parseVPInt(d.Data)
			if err != nil {
				return nil, err
			}
			w.items, w.weight = append(w.items, p), append(w.weight, d.Weight)
			w.total += d.Weight
		}
		return w, nil
	case "trapezoid":
		return vpTrapezoidInt{o.TMin, o.TMax, o.Plateau}, nil
	}
	return nil, fmt.Errorf("unknown int provider %q", o.Type)
}

// vpAnchor is a VerticalAnchor.
type vpAnchor struct {
	kind int8 // 0 absolute, 1 above_bottom, 2 below_top, 3 relative_to_sea_level
	v    int
}

func (a vpAnchor) resolve(c *vpCtx) int {
	switch a.kind {
	case 1:
		return c.minY + a.v
	case 2:
		return c.height - 1 + c.minY - a.v
	case 3:
		return c.seaLevel + a.v
	}
	return a.v
}

func parseVPAnchor(raw json.RawMessage) (vpAnchor, error) {
	var o map[string]int
	if err := json.Unmarshal(raw, &o); err != nil {
		return vpAnchor{}, err
	}
	for k, v := range o {
		switch k {
		case "absolute":
			return vpAnchor{0, v}, nil
		case "above_bottom":
			return vpAnchor{1, v}, nil
		case "below_top":
			return vpAnchor{2, v}, nil
		case "relative_to_sea_level":
			return vpAnchor{3, v}, nil
		}
	}
	return vpAnchor{}, fmt.Errorf("unknown vertical anchor %s", raw)
}

// vpHeight is a HeightProvider.
type vpHeight interface {
	sample(r *vwRandom, c *vpCtx) int
}

type vpConstHeight vpAnchor
type vpUniformHeight struct{ lo, hi vpAnchor }
type vpTrapezoidHeight struct {
	lo, hi  vpAnchor
	plateau int
}
type vpBiasedHeight struct {
	lo, hi vpAnchor
	inner  int
	very   bool
}

func (h vpConstHeight) sample(_ *vwRandom, c *vpCtx) int { return vpAnchor(h).resolve(c) }
func (h vpUniformHeight) sample(r *vwRandom, c *vpCtx) int {
	lo, hi := h.lo.resolve(c), h.hi.resolve(c)
	if lo > hi {
		return lo
	}
	return int(r.nextIntN(int32(hi-lo+1))) + lo
}
func (h vpTrapezoidHeight) sample(r *vwRandom, c *vpCtx) int {
	lo, hi := h.lo.resolve(c), h.hi.resolve(c)
	if lo > hi {
		return lo
	}
	rng := hi - lo
	if h.plateau >= rng {
		return int(r.nextIntN(int32(rng+1))) + lo
	}
	ps := (rng - h.plateau) / 2
	pe := rng - ps
	return lo + int(r.nextIntN(int32(pe+1))) + int(r.nextIntN(int32(ps+1)))
}

// mthNextInt is Mth.nextInt(random, min, max).
func mthNextInt(r *vwRandom, lo, hi int) int {
	if lo >= hi {
		return lo
	}
	return int(r.nextIntN(int32(hi-lo+1))) + lo
}

func (h vpBiasedHeight) sample(r *vwRandom, c *vpCtx) int {
	lo, hi := h.lo.resolve(c), h.hi.resolve(c)
	if hi-lo-h.inner+1 <= 0 {
		return lo
	}
	if !h.very {
		limit := int(r.nextIntN(int32(hi - lo - h.inner + 1)))
		return int(r.nextIntN(int32(limit+h.inner))) + lo
	}
	up := mthNextInt(r, lo+h.inner, hi)
	biased := mthNextInt(r, lo, up-1)
	return mthNextInt(r, lo, biased-1+h.inner)
}

func parseVPHeight(raw json.RawMessage) (vpHeight, error) {
	var o struct {
		Type    string          `json:"type"`
		Value   json.RawMessage `json:"value"`
		Min     json.RawMessage `json:"min_inclusive"`
		Max     json.RawMessage `json:"max_inclusive"`
		Plateau int             `json:"plateau"`
		Inner   *int            `json:"inner"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	if o.Type == "" { // a bare anchor is a constant height
		a, err := parseVPAnchor(raw)
		return vpConstHeight(a), err
	}
	if vpShort(o.Type) == "constant" {
		a, err := parseVPAnchor(o.Value)
		return vpConstHeight(a), err
	}
	lo, err := parseVPAnchor(o.Min)
	if err != nil {
		return nil, err
	}
	hi, err := parseVPAnchor(o.Max)
	if err != nil {
		return nil, err
	}
	inner := 1
	if o.Inner != nil {
		inner = *o.Inner
	}
	switch vpShort(o.Type) {
	case "uniform":
		return vpUniformHeight{lo, hi}, nil
	case "trapezoid":
		return vpTrapezoidHeight{lo, hi, o.Plateau}, nil
	case "biased_to_bottom":
		return vpBiasedHeight{lo, hi, inner, false}, nil
	case "very_biased_to_bottom":
		return vpBiasedHeight{lo, hi, inner, true}, nil
	}
	return nil, fmt.Errorf("unknown height provider %q", o.Type)
}

// ---- modifiers ----------------------------------------------------------------

type vpCount struct{ n vpIntProvider }
type vpInSquare struct{}
type vpHeightRange struct{ h vpHeight }
type vpHeightmap struct{ hm HeightmapType }
type vpRarity struct{ chance int }
type vpBiomeFilter struct{}
type vpOffset struct{ x, y, z vpIntProvider }
type vpPredicateFilter struct{ p vpPredicate }
type vpWaterDepth struct{ max int }
type vpSurfaceRelative struct {
	hm     HeightmapType
	lo, hi int64
}
type vpEnvScan struct {
	dir           [3]int
	target, allow vpPredicate
	maxSteps      int
}
type vpEveryLayer struct{ n vpIntProvider }
type vpNoiseCount struct {
	ratio          int
	factor, offset float64
}
type vpNoiseThreshold struct {
	level        float64
	below, above int
}
type vpFixed struct{ pos []vpPos }
type vpRandomChance struct{ chance float32 }

func (m vpCount) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	n := m.n.sample(r)
	for i := 0; i < n; i++ {
		out(p)
	}
}

func (vpInSquare) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	x := int(r.nextIntN(16)) + p.x
	z := int(r.nextIntN(16)) + p.z
	out(vpPos{x, p.y, z})
}

func (m vpHeightRange) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	out(vpPos{p.x, m.h.sample(r, c), p.z})
}

func (m vpHeightmap) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if h := c.lv.Height(m.hm, p.x, p.z); h > c.minY {
		out(vpPos{p.x, h, p.z})
	}
}

func (m vpRarity) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if r.nextFloat() < 1/float32(m.chance) {
		out(p)
	}
}

func (vpBiomeFilter) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if c.top == nil {
		panic("vanilla placement: biome check outside a top-level feature")
	}
	if c.hasFeature(c.lv.Biome(p.x, p.y, p.z), c.top.Name) {
		out(p)
	}
}

func (m vpOffset) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	dx := m.x.sample(r)
	dy := m.y.sample(r)
	dz := m.z.sample(r)
	out(vpPos{p.x + dx, p.y + dy, p.z + dz})
}

func (m vpPredicateFilter) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if m.p.test(c, p) {
		out(p)
	}
}

func (m vpWaterDepth) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	floor := c.lv.Height(HeightOceanFloor, p.x, p.z)
	surf := c.lv.Height(HeightWorldSurface, p.x, p.z)
	if surf-floor <= m.max {
		out(p)
	}
}

func (m vpSurfaceRelative) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	s := int64(c.lv.Height(m.hm, p.x, p.z))
	if s+m.lo <= int64(p.y) && int64(p.y) <= s+m.hi {
		out(p)
	}
}

func (m vpEnvScan) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if !m.allow.test(c, p) {
		return
	}
	for i := 0; i < m.maxSteps; i++ {
		if m.target.test(c, p) {
			out(p)
			return
		}
		p = vpPos{p.x + m.dir[0], p.y + m.dir[1], p.z + m.dir[2]}
		if c.outside(p.y) {
			return
		}
		if !m.allow.test(c, p) {
			break
		}
	}
	if m.target.test(c, p) {
		out(p)
	}
}

func (m vpEveryLayer) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	for layer := 0; ; layer++ {
		found := false
		for i := 0; i < m.n.sample(r); i++ {
			x := int(r.nextIntN(16)) + p.x
			z := int(r.nextIntN(16)) + p.z
			start := c.lv.Height(HeightMotionBlocking, x, z)
			if y, ok := vpOnGroundY(c, x, start, z, layer); ok {
				out(vpPos{x, y, z})
				found = true
			}
		}
		if !found {
			return
		}
	}
}

// vpOnGroundY is CountOnEveryLayerPlacement.findOnGroundYPosition.
func vpOnGroundY(c *vpCtx, x, yStart, z, layer int) (int, bool) {
	empty := func(s uint32) bool { return s == Air || IsWater(s) && !IsBubbleColumn(s) || IsLava(s) }
	cur := c.lv.Block(x, yStart, z)
	n := 0
	for y := yStart; y >= c.minY+1; y-- {
		below := c.lv.Block(x, y-1, z)
		if !empty(below) && empty(cur) && below != Bedrock {
			if n == layer {
				return y, true
			}
			n++
		}
		cur = below
	}
	return 0, false
}

func (m vpNoiseCount) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	n := float64(vpBiomeInfoNoise().get2(float64(p.x)/m.factor, float64(p.z)/m.factor))
	count := int(math.Ceil((n + m.offset) * float64(m.ratio)))
	for i := 0; i < count; i++ {
		out(p)
	}
}

func (m vpNoiseThreshold) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	n := float64(vpBiomeInfoNoise().get2(float64(p.x)/200, float64(p.z)/200))
	count := m.above
	if n < m.level {
		count = m.below
	}
	for i := 0; i < count; i++ {
		out(p)
	}
}

func (m vpFixed) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	cx, cz := p.x>>4, p.z>>4
	for _, q := range m.pos {
		if q.x>>4 == cx && q.z>>4 == cz {
			out(q)
		}
	}
}

func (m vpRandomChance) modify(c *vpCtx, r *vwRandom, p vpPos, out func(vpPos)) {
	if r.nextFloat() < m.chance {
		out(p)
	}
}

var vpHeightmapNames = map[string]HeightmapType{
	"WORLD_SURFACE_WG": HeightWorldSurfaceWG, "WORLD_SURFACE": HeightWorldSurface,
	"OCEAN_FLOOR_WG": HeightOceanFloorWG, "OCEAN_FLOOR": HeightOceanFloor,
	"MOTION_BLOCKING": HeightMotionBlocking, "MOTION_BLOCKING_NO_LEAVES": HeightMotionBlockingNoLeaves,
}

var vpDirections = map[string][3]int{
	"down": {0, -1, 0}, "up": {0, 1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
}

func parseVPModifier(raw json.RawMessage) (vpModifier, error) {
	var o map[string]json.RawMessage
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	var typ string
	if err := json.Unmarshal(o["type"], &typ); err != nil {
		return nil, err
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(o[k], &s)
		return s
	}
	num := func(k string, def float64) float64 {
		v := def
		if r, ok := o[k]; ok {
			_ = json.Unmarshal(r, &v)
		}
		return v
	}
	switch vpShort(typ) {
	case "count":
		n, err := parseVPInt(o["count"])
		return vpCount{n}, err
	case "in_square":
		return vpInSquare{}, nil
	case "height_range":
		h, err := parseVPHeight(o["height"])
		return vpHeightRange{h}, err
	case "heightmap":
		hm, ok := vpHeightmapNames[str("heightmap")]
		if !ok {
			return nil, fmt.Errorf("unknown heightmap %s", o["heightmap"])
		}
		return vpHeightmap{hm}, nil
	case "rarity_filter":
		return vpRarity{int(num("chance", 1))}, nil
	case "biome":
		return vpBiomeFilter{}, nil
	case "offset":
		var err error
		m := vpOffset{}
		for _, f := range []struct {
			k string
			p *vpIntProvider
		}{{"x", &m.x}, {"y", &m.y}, {"z", &m.z}} {
			if *f.p, err = parseVPInt(o[f.k]); err != nil {
				return nil, err
			}
		}
		return m, nil
	case "block_predicate_filter":
		p, err := parseVPPredicate(o["predicate"])
		return vpPredicateFilter{p}, err
	case "surface_water_depth_filter":
		return vpWaterDepth{int(num("max_water_depth", 0))}, nil
	case "surface_relative_threshold_filter":
		hm, ok := vpHeightmapNames[str("heightmap")]
		if !ok {
			return nil, fmt.Errorf("unknown heightmap %s", o["heightmap"])
		}
		return vpSurfaceRelative{hm, int64(num("min_inclusive", math.MinInt32)), int64(num("max_inclusive", math.MaxInt32))}, nil
	case "environment_scan":
		m := vpEnvScan{dir: vpDirections[str("direction_of_search")], maxSteps: int(num("max_steps", 1)), allow: vpTrue{}}
		var err error
		if m.target, err = parseVPPredicate(o["target_condition"]); err != nil {
			return nil, err
		}
		if a, ok := o["allowed_search_condition"]; ok {
			if m.allow, err = parseVPPredicate(a); err != nil {
				return nil, err
			}
		}
		return m, nil
	case "count_on_every_layer":
		n, err := parseVPInt(o["count"])
		return vpEveryLayer{n}, err
	case "noise_based_count":
		return vpNoiseCount{int(num("noise_to_count_ratio", 0)), num("noise_factor", 1), num("noise_offset", 0)}, nil
	case "noise_threshold_count":
		return vpNoiseThreshold{num("noise_level", 0), int(num("below_noise", 0)), int(num("above_noise", 0))}, nil
	case "fixed_placement":
		var ps [][3]int
		if err := json.Unmarshal(o["positions"], &ps); err != nil {
			return nil, err
		}
		m := vpFixed{}
		for _, q := range ps {
			m.pos = append(m.pos, vpPos{q[0], q[1], q[2]})
		}
		return m, nil
	case "random_chance":
		return vpRandomChance{float32(num("chance", 0))}, nil
	}
	return nil, fmt.Errorf("unknown placement modifier %q", typ)
}

// ---- block predicates ----------------------------------------------------------

// vpPredicate is a BlockPredicate.
type vpPredicate interface{ test(c *vpCtx, p vpPos) bool }

type vpTrue struct{}
type vpNot struct{ p vpPredicate }
type vpAllOf []vpPredicate
type vpAnyOf []vpPredicate
type vpMatchBlocks struct {
	off    vpPos
	ranges [][2]uint32
}
type vpMatchFluids struct {
	off    vpPos
	fluids map[string]bool
}
type vpSolid struct{ off vpPos }
type vpReplaceable struct{ off vpPos }
type vpSturdy struct {
	off  vpPos
	face int
}
type vpInsideWorld struct{ off vpPos }
type vpWouldSurvive struct {
	off   vpPos
	block string
}
type vpVolume struct {
	lo, hi vpPos
	p      vpPredicate
}
type vpHeightPred struct{ lo, hi vpAnchor }

func (vpTrue) test(*vpCtx, vpPos) bool        { return true }
func (n vpNot) test(c *vpCtx, p vpPos) bool   { return !n.p.test(c, p) }
func (p vpPos) add(o vpPos) vpPos             { return vpPos{p.x + o.x, p.y + o.y, p.z + o.z} }
func (c *vpCtx) blockAt(p vpPos) uint32       { return c.lv.Block(p.x, p.y, p.z) }
func (m vpSolid) test(c *vpCtx, p vpPos) bool { return IsSolid(c.blockAt(p.add(m.off))) }
func (m vpInsideWorld) test(c *vpCtx, p vpPos) bool {
	return !c.outside(p.add(m.off).y)
}
func (m vpReplaceable) test(c *vpCtx, p vpPos) bool {
	return IsReplaceable(c.blockAt(p.add(m.off)))
}
func (m vpSturdy) test(c *vpCtx, p vpPos) bool {
	return IsFaceSturdy(c.blockAt(p.add(m.off)), m.face)
}
func (a vpAllOf) test(c *vpCtx, p vpPos) bool {
	for _, q := range a {
		if !q.test(c, p) {
			return false
		}
	}
	return true
}
func (a vpAnyOf) test(c *vpCtx, p vpPos) bool {
	for _, q := range a {
		if q.test(c, p) {
			return true
		}
	}
	return false
}
func (m vpMatchBlocks) test(c *vpCtx, p vpPos) bool {
	return vpInRanges(m.ranges, c.blockAt(p.add(m.off)))
}
func (m vpMatchFluids) test(c *vpCtx, p vpPos) bool {
	return m.fluids[vpFluidOf(c.blockAt(p.add(m.off)))]
}
func (m vpVolume) test(c *vpCtx, p vpPos) bool {
	for ox := m.lo.x; ox <= m.hi.x; ox++ {
		for oz := m.lo.z; oz <= m.hi.z; oz++ {
			for oy := m.lo.y; oy <= m.hi.y; oy++ {
				if !m.p.test(c, vpPos{p.x + ox, p.y + oy, p.z + oz}) {
					return false
				}
			}
		}
	}
	return true
}
func (m vpHeightPred) test(c *vpCtx, p vpPos) bool {
	return m.lo.resolve(c) <= p.y && p.y <= m.hi.resolve(c)
}

func vpInRanges(rs [][2]uint32, s uint32) bool {
	for _, r := range rs {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

// vpFluidOf is a block state's fluid (FluidState's type): water and lava
// sources, their flowing levels, and the water a waterlogged block or an
// underwater plant holds.
func vpFluidOf(s uint32) string {
	switch {
	case IsWater(s):
		if IsFluidSource(s, WaterBase) {
			return "water"
		}
		return "flowing_water"
	case IsLava(s):
		if s == LavaBase {
			return "lava"
		}
		return "flowing_lava"
	case HoldsWater(s):
		return "water"
	}
	return "empty"
}

// vpWouldSurvive tests the few states the placements ask would_survive of,
// with their blocks' canSurvive: a sapling, propagule or firefly bush
// needs ground that supports it; cactus and sugar cane their own rules.
func (m vpWouldSurvive) test(c *vpCtx, p vpPos) bool {
	p = p.add(m.off)
	below := c.blockAt(vpPos{p.x, p.y - 1, p.z})
	switch m.block {
	case "mangrove_propagule":
		return vpTagHas("supports_mangrove_propagule", below)
	case "cactus":
		for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
			n := c.blockAt(vpPos{p.x + d[0], p.y, p.z + d[1]})
			if IsSolid(n) || IsLava(n) {
				return false
			}
		}
		above := c.blockAt(vpPos{p.x, p.y + 1, p.z})
		return (vpIsBlock(below, "cactus") || vpTagHas("supports_cactus", below)) && !IsFluid(above)
	case "sugar_cane":
		if vpIsBlock(below, "sugar_cane") {
			return true
		}
		if !vpTagHas("supports_sugar_cane", below) {
			return false
		}
		for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
			n := c.blockAt(vpPos{p.x + d[0], p.y - 1, p.z + d[1]})
			if HoldsWater(n) || vpTagHas("supports_sugar_cane_adjacently", n) {
				return true
			}
		}
		return false
	}
	return vpTagHas("supports_vegetation", below)
}

func vpIsBlock(s uint32, name string) bool {
	lo, hi, ok := BlockRangeOK(name)
	return ok && s >= lo && s <= hi
}

// vpBlockRanges resolves a list of block names to their state ranges.
func vpBlockRanges(names []string) [][2]uint32 {
	var out [][2]uint32
	for _, n := range names {
		if lo, hi, ok := BlockRangeOK(vpShort(n)); ok {
			out = append(out, [2]uint32{lo, hi})
		}
	}
	return out
}

var (
	vpTagsOnce    sync.Once
	vpBlockTags   map[string][]string
	vpFluidTags   map[string][]string
	vpTagRangesMu sync.Mutex
	vpTagRanges   = map[string][][2]uint32{}
)

// vpLoadTags reads the baked tags on their own: the placement data's
// parse resolves tags while it runs, so they cannot come from it.
func vpLoadTags() {
	vpTagsOnce.Do(func() {
		var in struct {
			Block map[string][]string `json:"block_tags"`
			Fluid map[string][]string `json:"fluid_tags"`
		}
		if err := json.Unmarshal(vanillaPlacementJSON, &in); err != nil {
			panic("vanilla placement tags: " + err.Error())
		}
		vpBlockTags, vpFluidTags = in.Block, in.Fluid
	})
}

// vpTagRangesOf is a baked block tag as state ranges.
func vpTagRangesOf(tag string) [][2]uint32 {
	vpLoadTags()
	vpTagRangesMu.Lock()
	defer vpTagRangesMu.Unlock()
	if r, ok := vpTagRanges[tag]; ok {
		return r
	}
	names, ok := vpBlockTags[tag]
	if !ok {
		panic("vanilla placement: block tag " + tag + " not baked")
	}
	r := vpBlockRanges(names)
	vpTagRanges[tag] = r
	return r
}

func vpTagHas(tag string, s uint32) bool { return vpInRanges(vpTagRangesOf(tag), s) }

func parseVPOffset(o map[string]json.RawMessage) (vpPos, error) {
	r, ok := o["offset"]
	if !ok {
		return vpPos{}, nil
	}
	var v [3]int
	if err := json.Unmarshal(r, &v); err != nil {
		return vpPos{}, err
	}
	return vpPos{v[0], v[1], v[2]}, nil
}

func parseVPPredicate(raw json.RawMessage) (vpPredicate, error) {
	var o map[string]json.RawMessage
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	var typ string
	if err := json.Unmarshal(o["type"], &typ); err != nil {
		return nil, err
	}
	off, err := parseVPOffset(o)
	if err != nil {
		return nil, err
	}
	list := func(k string) ([]vpPredicate, error) {
		var raws []json.RawMessage
		if err := json.Unmarshal(o[k], &raws); err != nil {
			return nil, err
		}
		var ps []vpPredicate
		for _, r := range raws {
			p, err := parseVPPredicate(r)
			if err != nil {
				return nil, err
			}
			ps = append(ps, p)
		}
		return ps, nil
	}
	names := func(k string) ([]string, string, error) {
		var one string
		if err := json.Unmarshal(o[k], &one); err == nil {
			if len(one) > 0 && one[0] == '#' {
				return nil, vpShort(one[1:]), nil
			}
			return []string{vpShort(one)}, "", nil
		}
		var many []string
		err := json.Unmarshal(o[k], &many)
		for i := range many {
			many[i] = vpShort(many[i])
		}
		return many, "", err
	}
	switch vpShort(typ) {
	case "true":
		return vpTrue{}, nil
	case "not":
		p, err := parseVPPredicate(o["predicate"])
		return vpNot{p}, err
	case "all_of":
		ps, err := list("predicates")
		return vpAllOf(ps), err
	case "any_of":
		ps, err := list("predicates")
		return vpAnyOf(ps), err
	case "matching_blocks":
		bl, tag, err := names("blocks")
		if err != nil {
			return nil, err
		}
		if tag != "" {
			return vpMatchBlocks{off, vpTagRangesOf(tag)}, nil
		}
		return vpMatchBlocks{off, vpBlockRanges(bl)}, nil
	case "matching_block_tag":
		var tag string
		if err := json.Unmarshal(o["tag"], &tag); err != nil {
			return nil, err
		}
		return vpMatchBlocks{off, vpTagRangesOf(vpShort(tag))}, nil
	case "matching_fluids":
		fl, tag, err := names("fluids")
		if err != nil {
			return nil, err
		}
		if tag != "" {
			vpLoadTags()
			fl = vpFluidTags[tag]
		}
		m := vpMatchFluids{off, map[string]bool{}}
		for _, f := range fl {
			m.fluids[vpShort(f)] = true
		}
		return m, nil
	case "solid":
		return vpSolid{off}, nil
	case "replaceable":
		return vpReplaceable{off}, nil
	case "has_sturdy_face":
		var d string
		_ = json.Unmarshal(o["direction"], &d)
		face := map[string]int{"down": FaceDown, "up": FaceUp, "north": FaceNorth, "south": FaceSouth, "west": FaceWest, "east": FaceEast}[d]
		return vpSturdy{off, face}, nil
	case "inside_world_bounds":
		return vpInsideWorld{off}, nil
	case "would_survive":
		var st struct {
			Name string `json:"Name"`
		}
		if err := json.Unmarshal(o["state"], &st); err != nil {
			var n string
			if err := json.Unmarshal(o["state"], &n); err != nil {
				return nil, err
			}
			st.Name = n
		}
		return vpWouldSurvive{off, vpShort(st.Name)}, nil
	case "volume_match":
		var lo, hi [3]int
		if err := json.Unmarshal(o["min"], &lo); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(o["max"], &hi); err != nil {
			return nil, err
		}
		p, err := parseVPPredicate(o["match"])
		return vpVolume{vpPos{lo[0], lo[1], lo[2]}, vpPos{hi[0], hi[1], hi[2]}, p}, err
	case "height_range":
		lo, err := parseVPAnchor(o["min_inclusive"])
		if err != nil {
			return nil, err
		}
		hi, err := parseVPAnchor(o["max_inclusive"])
		return vpHeightPred{lo, hi}, err
	}
	return nil, fmt.Errorf("unknown block predicate %q", typ)
}

// ---- Biome.BIOME_INFO_NOISE ---------------------------------------------------------

// vpSimplex is SimplexNoise (2-D), as Biome.BIOME_INFO_NOISE builds it.
type vpSimplex struct {
	perms      [256]uint8
	offX, offY float64
}

var (
	vpInfoNoiseOnce sync.Once
	vpInfoNoise     *vpSimplex
)

// vpBiomeInfoNoise is new SimplexNoise(new WorldgenRandom(new
// LegacyRandomSource(2345)), true): its offsets drawn and discarded (times
// zero), then the permutation.
func vpBiomeInfoNoise() *vpSimplex {
	vpInfoNoiseOnce.Do(func() {
		r := newVWLegacy(2345)
		n := &vpSimplex{}
		n.offX = r.nextDouble() * 0
		n.offY = r.nextDouble() * 0
		_ = r.nextDouble()
		for i := range n.perms {
			n.perms[i] = uint8(i)
		}
		for i := 0; i < 256; i++ {
			o := int(r.nextIntN(int32(256 - i)))
			n.perms[i], n.perms[o+i] = n.perms[o+i], n.perms[i]
		}
		vpInfoNoise = n
	})
	return vpInfoNoise
}

func (n *vpSimplex) permute(x int) int { return int(n.perms[x&0xFF]) }

var (
	vpSqrt3 = math.Sqrt(3)
	vpF2    = 0.5 * (vpSqrt3 - 1)
	vpG2    = (3 - vpSqrt3) / 6
)

func vpCorner(idx int, x, y float64) float64 {
	t := 0.5 - x*x - y*y
	if t < 0 {
		return 0
	}
	t *= t
	g := vnGrad[idx]
	return t * t * (float64(g[0])*x + float64(g[1])*y)
}

// get2 is SimplexNoise.get(x, y).
func (n *vpSimplex) get2(xin, yin float64) float32 {
	xin += n.offX
	yin += n.offY
	s := (xin + yin) * vpF2
	i := int(math.Floor(xin + s))
	j := int(math.Floor(yin + s))
	t := float64(i+j) * vpG2
	x0 := xin - (float64(i) - t)
	y0 := yin - (float64(j) - t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1 := x0 - float64(i1) + vpG2
	y1 := y0 - float64(j1) + vpG2
	x2 := x0 - 1 + 2*vpG2
	y2 := y0 - 1 + 2*vpG2
	ii, jj := i&0xFF, j&0xFF
	gi0 := n.permute(ii+n.permute(jj)) % 12
	gi1 := n.permute(ii+i1+n.permute(jj+j1)) % 12
	gi2 := n.permute(ii+1+n.permute(jj+1)) % 12
	return float32(70 * (vpCorner(gi0, x0, y0) + vpCorner(gi1, x1, y1) + vpCorner(gi2, x2, y2)))
}
