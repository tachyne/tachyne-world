package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

// The vanilla generator's surface: 26.3's MaterialSystem (SurfaceSystem)
// running the noise settings' material rule (worldgen/material_rule, with
// the conditions of worldgen/material_condition) over each column of a
// filled chunk, top down — the surface blocks by biome, the badlands'
// terracotta bands, the bedrock gradient, deepslate's dithering and the ore
// veins are all rules in the data — plus the eroded-badlands pillars and
// the frozen ocean's icebergs it adds in code, and the top-material lookup
// the carvers ask for under a grass block they cut.

// vtAnchor is a VerticalAnchor.
type vtAnchor struct {
	kind byte // 'a' absolute, 'b' above_bottom, 't' below_top
	v    int
}

func (a vtAnchor) resolve(minGenY, genDepth int) int {
	switch a.kind {
	case 'b':
		return minGenY + a.v
	case 't':
		return genDepth - 1 + minGenY - a.v
	}
	return a.v
}

func vtParseAnchor(v any) (vtAnchor, error) {
	m, _ := v.(map[string]any)
	for k, kind := range map[string]byte{"absolute": 'a', "above_bottom": 'b', "below_top": 't'} {
		if n, ok := m[k]; ok {
			f, err := vdNum(n)
			return vtAnchor{kind, int(f)}, err
		}
	}
	return vtAnchor{}, fmt.Errorf("vertical anchor %v", v)
}

// vmRule is a MaterialRule.
type vmRule struct {
	kind  byte // 'b' block, 'B' bandlands, 's' sequence, 'c' condition, 'o' ore vein
	state uint32
	seq   []*vmRule
	cond  *vmCond
	then  *vmRule
	ore   *vmOre
}

// vmOre is OreVeinRule.
type vmOre struct {
	ore, raw, filler       uint32
	rawChance              float32
	density, richness, gap vdSampler
}

// vmCond is a MaterialCondition.
type vmCond struct {
	kind       string
	biomes     map[string]bool
	noise      *vtStack
	lo, hi     float64
	is3d       bool
	random     vtPositional
	trueAt     vtAnchor
	falseAt    vtAnchor
	anchor     vtAnchor
	mult       int
	addStone   bool
	offset     int
	addSurface bool
	secondary  int
	ceiling    bool
	invert     *vmCond
}

// vmLoader reads rules and conditions for one dimension's system.
type vmLoader struct {
	g     *vtGen
	rules map[string]*vmRule
	conds map[string]*vmCond
}

func (l *vmLoader) ruleNamed(name string) (*vmRule, error) {
	if r, ok := l.rules[name]; ok {
		return r, nil
	}
	raw, ok := vanillaWorldgenData["material_rule/"+vtStripNS(name)]
	if !ok {
		return nil, fmt.Errorf("material rule %q not in the data", name)
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	r, err := l.rule(v)
	if err != nil {
		return nil, fmt.Errorf("material rule %s: %w", name, err)
	}
	l.rules[name] = r
	return r, nil
}

func (l *vmLoader) condNamed(name string) (*vmCond, error) {
	if c, ok := l.conds[name]; ok {
		return c, nil
	}
	raw, ok := vanillaWorldgenData["material_condition/"+vtStripNS(name)]
	if !ok {
		return nil, fmt.Errorf("material condition %q not in the data", name)
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	c, err := l.cond(v)
	if err != nil {
		return nil, fmt.Errorf("material condition %s: %w", name, err)
	}
	l.conds[name] = c
	return c, nil
}

func (l *vmLoader) rule(v any) (*vmRule, error) {
	if s, ok := v.(string); ok {
		return l.ruleNamed(s)
	}
	m, _ := v.(map[string]any)
	typ, _ := m["type"].(string)
	switch vtStripNS(typ) {
	case "block":
		raw, _ := json.Marshal(m["result_state"])
		s, err := vtParseState(raw)
		return &vmRule{kind: 'b', state: s}, err
	case "bandlands":
		return &vmRule{kind: 'B'}, nil
	case "sequence":
		r := &vmRule{kind: 's'}
		seq, _ := m["sequence"].([]any)
		for _, e := range seq {
			sub, err := l.rule(e)
			if err != nil {
				return nil, err
			}
			r.seq = append(r.seq, sub)
		}
		return r, nil
	case "condition":
		c, err := l.cond(m["if_true"])
		if err != nil {
			return nil, err
		}
		t, err := l.rule(m["then_run"])
		if err != nil {
			return nil, err
		}
		return &vmRule{kind: 'c', cond: c, then: t}, nil
	case "ore_vein":
		o := &vmOre{}
		var err error
		for _, f := range []struct {
			k   string
			dst *uint32
		}{{"ore_block", &o.ore}, {"raw_ore_block", &o.raw}, {"filler_block", &o.filler}} {
			raw, _ := json.Marshal(m[f.k])
			if *f.dst, err = vtParseState(raw); err != nil {
				return nil, err
			}
		}
		ch, err := vdNum(m["raw_ore_chance"])
		if err != nil {
			return nil, err
		}
		o.rawChance = float32(ch)
		for _, f := range []struct {
			k   string
			dst *vdSampler
		}{{"density", &o.density}, {"richness", &o.richness}, {"filler_gap", &o.gap}} {
			raw, _ := json.Marshal(m[f.k])
			fn, err := vdParseJSON(raw)
			if err != nil {
				return nil, err
			}
			if *f.dst, err = l.g.rs.compile(fn); err != nil {
				return nil, err
			}
		}
		return &vmRule{kind: 'o', ore: o}, nil
	}
	return nil, fmt.Errorf("material rule type %q is not supported", typ)
}

func (l *vmLoader) cond(v any) (*vmCond, error) {
	if s, ok := v.(string); ok {
		return l.condNamed(s)
	}
	m, _ := v.(map[string]any)
	typ, _ := m["type"].(string)
	c := &vmCond{kind: vtStripNS(typ)}
	num := func(k string) (float64, error) { return vdNum(m[k]) }
	var err error
	switch c.kind {
	case "biome":
		c.biomes = map[string]bool{}
		switch b := m["biome_is"].(type) {
		case string:
			c.biomes[b] = true
		case []any:
			for _, e := range b {
				s, _ := e.(string)
				c.biomes[s] = true
			}
		}
	case "noise_threshold":
		name, _ := m["noise"].(string)
		if c.noise, err = l.g.rs.noise(name); err != nil {
			return nil, err
		}
		if c.lo, err = num("min_threshold"); err != nil {
			return nil, err
		}
		if c.hi, err = num("max_threshold"); err != nil {
			return nil, err
		}
		c.is3d, _ = m["is_3d"].(bool)
	case "vertical_gradient":
		name, _ := m["random_name"].(string)
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		c.random = l.g.rs.randomFactory(name)
		if c.trueAt, err = vtParseAnchor(m["true_at_and_below"]); err != nil {
			return nil, err
		}
		if c.falseAt, err = vtParseAnchor(m["false_at_and_above"]); err != nil {
			return nil, err
		}
	case "y_above", "water":
		mult, err := num("surface_depth_multiplier")
		if err != nil {
			return nil, err
		}
		c.mult = int(mult)
		c.addStone, _ = m["add_stone_depth"].(bool)
		if c.kind == "y_above" {
			if c.anchor, err = vtParseAnchor(m["anchor"]); err != nil {
				return nil, err
			}
		} else {
			off, err := num("offset")
			if err != nil {
				return nil, err
			}
			c.offset = int(off)
		}
	case "stone_depth":
		off, err := num("offset")
		if err != nil {
			return nil, err
		}
		c.offset = int(off)
		c.addSurface, _ = m["add_surface_depth"].(bool)
		sec, err := num("secondary_depth_range")
		if err != nil {
			return nil, err
		}
		c.secondary = int(sec)
		c.ceiling = m["surface_type"] == "ceiling"
	case "not":
		if c.invert, err = l.cond(m["invert"]); err != nil {
			return nil, err
		}
	case "temperature", "steep", "hole", "above_preliminary_surface":
	default:
		return nil, fmt.Errorf("material condition type %q is not supported", typ)
	}
	return c, nil
}

// vmSystem is MaterialSystem for one dimension.
type vmSystem struct {
	g                                             *vtGen
	rule                                          *vmRule
	minGenY, genDepth                             int
	noiseRandom                                   vtPositional
	surface, surfaceSecondary, clayOffset         *vtStack
	badlandsPillar, badlandsRoof, badlandsSurface *vtStack
	icebergPillar, icebergRoof, icebergSurface    *vtStack
	clayBands                                     [192]uint32
	oreRandom                                     vtPositional // "minecraft:ore"
}

func newVMSystem(g *vtGen) (*vmSystem, error) {
	l := &vmLoader{g: g, rules: map[string]*vmRule{}, conds: map[string]*vmCond{}}
	root, err := l.ruleNamed(g.set.materialRule)
	if err != nil {
		return nil, err
	}
	rs := g.rs
	s := &vmSystem{g: g, rule: root, minGenY: g.set.minY, genDepth: g.set.height, noiseRandom: rs.random}
	for _, n := range []struct {
		name string
		dst  **vtStack
	}{
		{"minecraft:surface", &s.surface}, {"minecraft:surface_secondary", &s.surfaceSecondary},
		{"minecraft:clay_bands_offset", &s.clayOffset}, {"minecraft:badlands_pillar", &s.badlandsPillar},
		{"minecraft:badlands_pillar_roof", &s.badlandsRoof}, {"minecraft:badlands_surface", &s.badlandsSurface},
		{"minecraft:iceberg_pillar", &s.icebergPillar}, {"minecraft:iceberg_pillar_roof", &s.icebergRoof},
		{"minecraft:iceberg_surface", &s.icebergSurface},
	} {
		if *n.dst, err = rs.noise(n.name); err != nil {
			return nil, err
		}
	}
	s.oreRandom = rs.randomFactory("minecraft:ore")
	s.genBands(s.noiseRandom.fromHashOf("minecraft:clay_bands"))
	return s, nil
}

// genBands is MaterialSystem.generateBands.
func (s *vmSystem) genBands(r vtRandom) {
	b := &s.clayBands
	for i := range b {
		b[i] = Terracotta
	}
	for i := 0; i < len(b); i++ {
		i += int(r.nextInt(5)) + 1
		if i < len(b) {
			b[i] = OrangeTerracotta
		}
	}
	makeBands := func(base int, state uint32) {
		n := int(vtNextIntBetween(r, 6, 15))
		for i := 0; i < n; i++ {
			width := base + int(r.nextInt(3))
			start := int(r.nextInt(int32(len(b))))
			for p := 0; start+p < len(b) && p < width; p++ {
				b[start+p] = state
			}
		}
	}
	makeBands(1, YellowTerracotta)
	makeBands(2, BrownTerracotta)
	makeBands(1, RedTerracotta)
	white := int(vtNextIntBetween(r, 9, 15))
	ix := 0
	for start := 0; ix < white && start < len(b); start += int(r.nextInt(16)) + 4 {
		b[start] = WhiteTerracotta
		if start-1 > 0 && r.nextBoolean() {
			b[start-1] = LightGrayTerracotta
		}
		if start+1 < len(b) && r.nextBoolean() {
			b[start+1] = LightGrayTerracotta
		}
		ix++
	}
}

// band is getBand.
func (s *vmSystem) band(x, y, z int) uint32 {
	off := vtJavaRound(s.clayOffset.get(float64(x), 0, float64(z)) * 4)
	n := len(s.clayBands)
	return s.clayBands[((y+off+n)%n+n)%n]
}

// vtJavaRound is Math.round(float).
func vtJavaRound(v float32) int { return int(math.Floor(float64(v) + 0.5)) }

// surfaceDepth is getSurfaceDepth.
func (s *vmSystem) surfaceDepth(x, z int) int {
	n := float64(s.surface.get(float64(x), 0, float64(z)))
	return int(n*2.75 + 3.0 + s.noiseRandom.at(int32(x), 0, int32(z)).nextDouble()*0.25)
}

// vmColumn is what the rules read of the column they run on.
type vmColumn interface {
	block(y int) uint32
	set(y int, s uint32)
}

// vmCtx is MaterialRuleContext.
type vmCtx struct {
	sys      *vmSystem
	nc       *vtNoiseChunk
	biomeAt  func(x, y, z int) string
	expected vtVolume
	prelim   vtVolume
	prelimB  []float32

	xzGen, yGen int64

	blockX, blockZ, gradX, gradZ, surfaceDepth int
	secGen                                     int64
	secondary                                  float64
	minGen                                     int64
	minSurface                                 int

	blockY, waterHeight, stoneBelow, stoneAbove int
	biome                                       string
	biomeOK                                     bool

	n2d, n3d map[*vtStack]vmNoiseCache
	ores     map[*vmOre]*vmOreBufs
}

type vmNoiseCache struct {
	gen int64
	v   float64
}

type vmOreBufs struct{ density, richness []float32 }

func (s *vmSystem) newCtx(nc *vtNoiseChunk, expected vtVolume, biomeAt func(x, y, z int) string) *vmCtx {
	c := &vmCtx{sys: s, nc: nc, biomeAt: biomeAt, expected: expected,
		prelim: vtVol(expected.sx, 1, expected.sz, expected.minX, 0, expected.minZ),
		n2d:    map[*vtStack]vmNoiseCache{}, n3d: map[*vtStack]vmNoiseCache{}, ores: map[*vmOre]*vmOreBufs{}}
	c.xzGen = math.MinInt64 + 1
	c.yGen = math.MinInt64 + 1
	c.secGen = c.xzGen - 1
	c.minGen = c.xzGen - 1
	// Compiling the rules prefills the ore veins' densities over the
	// expected volume (getDensitiesInChunk), in the rules' order.
	c.prefill(s.rule)
	return c
}

func (c *vmCtx) prefill(r *vmRule) {
	switch r.kind {
	case 's':
		for _, e := range r.seq {
			c.prefill(e)
		}
	case 'c':
		c.prefill(r.then)
	case 'o':
		if _, ok := c.ores[r.ore]; ok {
			return
		}
		b := &vmOreBufs{density: vdBuf(c.expected.size()), richness: vdBuf(c.expected.size())}
		r.ore.density.volume(c.nc.ctx, b.density, c.expected)
		r.ore.richness.volume(c.nc.ctx, b.richness, c.expected)
		c.ores[r.ore] = b
	}
}

func (c *vmCtx) updateXZ(x, z, gx, gz int) {
	c.xzGen++
	c.yGen++
	c.blockX, c.blockZ, c.gradX, c.gradZ = x, z, gx, gz
	c.surfaceDepth = c.sys.surfaceDepth(x, z)
}

func (c *vmCtx) updateY(stoneAbove, stoneBelow, waterHeight, y int) {
	c.yGen++
	c.biomeOK = false
	c.blockY, c.waterHeight, c.stoneBelow, c.stoneAbove = y, waterHeight, stoneBelow, stoneAbove
}

func (c *vmCtx) surfaceSecondary() float64 {
	if c.secGen != c.xzGen {
		c.secGen = c.xzGen
		c.secondary = float64(c.sys.surfaceSecondary.get(float64(c.blockX), 0, float64(c.blockZ)))
	}
	return c.secondary
}

func (c *vmCtx) getBiome() string {
	if !c.biomeOK {
		c.biome = c.biomeAt(c.blockX, c.blockY, c.blockZ)
		c.biomeOK = true
	}
	return c.biome
}

// minSurfaceLevel is getMinSurfaceLevel: the chunk surface level (the
// noise settings' chunk_surface_level, over the chunk's columns at once)
// plus the surface depth, less eight.
func (c *vmCtx) minSurfaceLevel() int {
	if c.minGen != c.xzGen {
		c.minGen = c.xzGen
		var p float32
		if i := c.prelim.indexOf(c.blockX, 0, c.blockZ); i != -1 {
			if c.prelimB == nil {
				c.prelimB = vdBuf(c.prelim.size())
				c.sys.g.chunkSurface.volume(c.nc.ctx, c.prelimB, c.prelim)
			}
			p = c.prelimB[i]
		} else {
			p = c.sys.g.chunkSurface.value(c.nc.ctx, c.blockX, 0, c.blockZ)
		}
		c.minSurface = int(math.Floor(float64(p))) + c.surfaceDepth - 8
	}
	return c.minSurface
}

func (c *vmCtx) noise(n *vtStack, is3d bool) float64 {
	if is3d {
		e, ok := c.n3d[n]
		if !ok || e.gen != c.yGen {
			e = vmNoiseCache{c.yGen, float64(n.get(float64(c.blockX), float64(c.blockY), float64(c.blockZ)))}
			c.n3d[n] = e
		}
		return e.v
	}
	e, ok := c.n2d[n]
	if !ok || e.gen != c.xzGen {
		e = vmNoiseCache{c.xzGen, float64(n.get(float64(c.blockX), 0, float64(c.blockZ)))}
		c.n2d[n] = e
	}
	return e.v
}

// test is a condition's evaluator.
func (c *vmCtx) test(k *vmCond) bool {
	s := c.sys
	switch k.kind {
	case "biome":
		return k.biomes[c.getBiome()]
	case "noise_threshold":
		v := c.noise(k.noise, k.is3d)
		return v >= k.lo && v <= k.hi
	case "vertical_gradient":
		t := k.trueAt.resolve(s.minGenY, s.genDepth)
		f := k.falseAt.resolve(s.minGenY, s.genDepth)
		switch {
		case c.blockY <= t:
			return true
		case c.blockY >= f:
			return false
		}
		p := mthMap(float64(c.blockY), float64(t), float64(f), 1, 0)
		return float64(k.random.at(int32(c.blockX), int32(c.blockY), int32(c.blockZ)).nextFloat()) < p
	case "y_above":
		y := c.blockY
		if k.addStone {
			y += c.stoneAbove
		}
		return y >= k.anchor.resolve(s.minGenY, s.genDepth)+c.surfaceDepth*k.mult
	case "water":
		if c.waterHeight == math.MinInt32 {
			return true
		}
		y := c.blockY
		if k.addStone {
			y += c.stoneAbove
		}
		return y >= c.waterHeight+k.offset+c.surfaceDepth*k.mult
	case "stone_depth":
		depth := c.stoneAbove
		if k.ceiling {
			depth = c.stoneBelow
		}
		surface := 0
		if k.addSurface {
			surface = c.surfaceDepth
		}
		sec := 0
		if k.secondary != 0 {
			sec = int(mthMap(c.surfaceSecondary(), -1, 1, 0, float64(k.secondary)))
		}
		return depth <= 1+k.offset+surface+sec
	case "not":
		return !c.test(k.invert)
	case "hole":
		return c.surfaceDepth <= 0
	case "steep":
		return c.gradX <= -4 || c.gradZ >= 4
	case "above_preliminary_surface":
		return c.blockY >= c.minSurfaceLevel()
	case "temperature":
		return vtColdEnoughToSnow(c.getBiome(), c.blockX, c.blockY, c.blockZ, s.g.set.seaLevel)
	}
	return false
}

// apply is a rule's tryApply: the block, or 0 with false for none.
func (c *vmCtx) apply(r *vmRule) (uint32, bool) {
	switch r.kind {
	case 'b':
		return r.state, true
	case 'B':
		return c.sys.band(c.blockX, c.blockY, c.blockZ), true
	case 's':
		for _, e := range r.seq {
			if s, ok := c.apply(e); ok {
				return s, true
			}
		}
		return 0, false
	case 'c':
		if !c.test(r.cond) {
			return 0, false
		}
		return c.apply(r.then)
	case 'o':
		return c.ore(r.ore)
	}
	return 0, false
}

// ore is OreVeinRule's evaluator.
func (c *vmCtx) ore(o *vmOre) (uint32, bool) {
	x, y, z := c.blockX, c.blockY, c.blockZ
	bufs := c.ores[o]
	get := func(s vdSampler, buf []float32) float32 {
		if i := c.expected.indexOf(x, y, z); i != -1 {
			return buf[i]
		}
		return s.value(c.nc.ctx, x, y, z)
	}
	d := get(o.density, bufs.density)
	if d <= 0 {
		return 0, false
	}
	r := c.sys.oreRandom.at(int32(x), int32(y), int32(z))
	if r.nextFloat() > d {
		return 0, false
	}
	rich := get(o.richness, bufs.richness)
	if r.nextFloat() < rich && o.gap.value(c.nc.ctx, x, y, z) < 0 {
		if r.nextFloat() < o.rawChance {
			return o.raw, true
		}
		return o.ore, true
	}
	return o.filler, true
}

// vtIsFluid reports a block holding a fluid (water, lava, or waterlogged).
func vtIsFluid(s uint32) bool {
	return IsFluid(s) || HoldsWater(s) || IsWaterlogged(s)
}

// buildSurface is MaterialSystem.buildSurface over a filled chunk.
func (s *vmSystem) buildSurface(ch *Chunk, cx, cz int32, nc *vtNoiseChunk, biomeAt func(x, y, z int) string) {
	minBX, minBZ := int(cx)*16, int(cz)*16
	minY := s.minGenY
	maxY := s.minGenY + s.genDepth - 1
	// The narrowed volume: up to the top of the highest section with a block.
	top := MinY - 1
	for i := len(ch.Sections) - 1; i >= 0; i-- {
		if !vtSectionEmpty(&ch.Sections[i]) {
			top = MinY + i*16 + 15
			break
		}
	}
	vol := nc.vol
	narrowed := vtVol(vol.sx, max(top-vol.minY+1, 1), vol.sz, vol.minX, vol.minY, vol.minZ)
	ctx := s.newCtx(nc, narrowed, biomeAt)
	col := &vmChunkColumn{ch: ch, minY: minY, maxY: maxY}
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			bx, bz := minBX+x, minBZ+z
			col.x, col.z = x, z
			start := vtHeightWS(ch, x, z) + 1
			biome := biomeAt(bx, start, bz)
			if biome == "minecraft:eroded_badlands" {
				s.erodedBadlands(col, bx, bz, start)
			}
			height := vtHeightWS(ch, x, z) + 1
			gx := vtHeightWS(ch, min(x+1, 15), z) - vtHeightWS(ch, max(x-1, 0), z)
			gz := vtHeightWS(ch, x, min(z+1, 15)) - vtHeightWS(ch, x, max(z-1, 0))
			ctx.updateXZ(bx, bz, gx, gz)
			stoneAbove := 0
			water := math.MinInt32
			nextCeiling := math.MaxInt32
			endY := minY
			for y := height; y >= endY; y-- {
				old := col.block(y)
				switch {
				case old == Air || vtIsAir(old):
					stoneAbove = 0
					water = math.MinInt32
				case vtIsFluid(old):
					if water == math.MinInt32 {
						water = y + 1
					}
				default:
					if nextCeiling >= y {
						nextCeiling = vaqWayBelow
						for la := y - 1; la >= endY-1; la-- {
							if !vtIsStone(col.block(la)) {
								nextCeiling = la + 1
								break
							}
						}
					}
					stoneAbove++
					stoneBelow := y - nextCeiling + 1
					ctx.updateY(stoneAbove, stoneBelow, water, y)
					if y >= minY && y <= maxY {
						if st, ok := ctx.apply(s.rule); ok {
							col.set(y, st)
						}
					}
				}
			}
			if biome == "minecraft:frozen_ocean" || biome == "minecraft:deep_frozen_ocean" {
				s.frozenOcean(ctx.minSurfaceLevel(), biome, col, bx, bz, start)
			}
		}
	}
}

// vtIsAir is BlockState.isAir: air, cave air, void air.
var vtCaveAir, vtVoidAir = blockBase("cave_air"), blockBase("void_air")

func vtIsAir(s uint32) bool { return s == Air || s == vtCaveAir || s == vtVoidAir }

func vtIsStone(s uint32) bool { return s != Air && !vtIsAir(s) && !vtIsFluid(s) }

func vtSectionEmpty(sec *[4096]uint32) bool {
	for _, s := range sec {
		if s != Air {
			return false
		}
	}
	return true
}

// vtHeightWS is the chunk's WORLD_SURFACE_WG height at a column: its
// highest non-air block (MinY-1 for none).
func vtHeightWS(ch *Chunk, x, z int) int {
	for s := len(ch.Sections) - 1; s >= 0; s-- {
		sec := &ch.Sections[s]
		for ly := 15; ly >= 0; ly-- {
			if b := sec[(ly*16+z)*16+x]; b != Air && !vtIsAir(b) {
				return MinY + s*16 + ly
			}
		}
	}
	return MinY - 1
}

// vmChunkColumn is the BlockColumn over one column of a chunk.
type vmChunkColumn struct {
	ch         *Chunk
	x, z       int
	minY, maxY int
}

func (c *vmChunkColumn) block(y int) uint32 { return c.ch.getGen(c.x, y, c.z) }
func (c *vmChunkColumn) set(y int, s uint32) {
	if y >= c.minY && y <= c.maxY {
		c.ch.setGen(c.x, y, c.z, s)
	}
}

// erodedBadlands is erodedBadlandsExtension: the eroded badlands' stone
// pillars over the surface.
func (s *vmSystem) erodedBadlands(col *vmChunkColumn, x, z, height int) {
	buf := math.Min(math.Abs(float64(s.badlandsSurface.get(float64(x), 0, float64(z)))*8.25),
		float64(s.badlandsPillar.get(float64(x)*0.2, 0, float64(z)*0.2)*15))
	if buf <= 0 {
		return
	}
	floor := math.Abs(float64(s.badlandsRoof.get(float64(x)*0.75, 0, float64(z)*0.75)) * 1.5)
	top := 64 + math.Min(buf*buf*2.5, math.Ceil(floor*50)+24)
	startY := int(math.Floor(top))
	if height > startY {
		return
	}
	for y := startY; y >= s.minGenY; y-- {
		old := col.block(y)
		if old == s.g.set.defaultBlock {
			break
		}
		if old == Water || IsWater(old) {
			return
		}
	}
	for y := startY; y >= s.minGenY && (col.block(y) == Air || vtIsAir(col.block(y))); y-- {
		col.set(y, s.g.set.defaultBlock)
	}
}

// frozenOcean is frozenOceanExtension: the icebergs.
func (s *vmSystem) frozenOcean(minSurface int, biome string, col *vmChunkColumn, x, z, height int) {
	sea := s.g.set.seaLevel
	ice := math.Min(math.Abs(float64(s.icebergSurface.get(float64(x), 0, float64(z)))*8.25),
		float64(s.icebergPillar.get(float64(x)*1.28, 0, float64(z)*1.28)*15))
	if ice <= 1.8 {
		return
	}
	roof := math.Abs(float64(s.icebergRoof.get(float64(x)*1.17, 0, float64(z)*1.17)) * 1.5)
	top := math.Min(ice*ice*1.2, math.Ceil(roof*40)+14)
	if vtBiomeTemperature(biome, x, sea, z, sea) > 0.1 {
		top -= 2
	}
	if top <= 2 {
		return
	}
	bottom := float64(sea) - top - 7
	top += float64(sea)
	r := s.noiseRandom.at(int32(x), 0, int32(z))
	maxSnow := 2 + int(r.nextInt(4))
	minSnowY := sea + 18 + int(r.nextInt(10))
	snow := 0
	for y := max(height, int(top)+1); y >= minSurface; y-- {
		b := col.block(y)
		if (b == Air || vtIsAir(b)) && y < int(top) && r.nextDouble() > 0.01 ||
			(b == Water || IsWater(b)) && y > int(bottom) && y < sea && r.nextDouble() > 0.15 {
			if snow <= maxSnow && y > minSnowY {
				col.set(y, SnowBlock)
				snow++
			} else {
				col.set(y, PackedIce)
			}
		}
	}
}

// topMaterial is MaterialSystem.topMaterial: the block the rules put at
// pos as a surface (stone depth one above and below), for the carvers.
func (s *vmSystem) topMaterial(ch *Chunk, nc *vtNoiseChunk, biomeAt func(x, y, z int) string, x, y, z int, underFluid bool) (uint32, bool) {
	ctx := s.newCtx(nc, vtVol(1, 1, 1, x, y, z), biomeAt)
	lx, lz := x&15, z&15
	gx := vtHeightWS(ch, min(lx+1, 15), lz) - vtHeightWS(ch, max(lx-1, 0), lz)
	gz := vtHeightWS(ch, lx, min(lz+1, 15)) - vtHeightWS(ch, lx, max(lz-1, 0))
	ctx.updateXZ(x, z, gx, gz)
	water := math.MinInt32
	if underFluid {
		water = y + 1
	}
	ctx.updateY(1, 1, water, y)
	return ctx.apply(s.rule)
}

// ---- biome temperature (Biome.getHeightAdjustedTemperature) ----

var (
	vtBiomeNoiseOnce         sync.Once
	vtTempNoise, vtInfoNoise *vtSimplex
	vtFrozenNoise            [3]*vtSimplex
)

func vtBiomeNoises() {
	vtBiomeNoiseOnce.Do(func() {
		vtTempNoise = newVTSimplex(newVTLegacy(1234))
		r := newVTLegacy(3456)
		for i := range vtFrozenNoise {
			vtFrozenNoise[i] = newVTSimplex(r)
		}
		vtInfoNoise = newVTSimplex(newVTLegacy(2345))
	})
}

// vtBiomeTemperature is Biome.getHeightAdjustedTemperature (the frozen
// modifier's open-water patches, and the drop above sea level + 17).
func vtBiomeTemperature(biome string, x, y, z, seaLevel int) float32 {
	vtBiomeNoises()
	t := float32(biomeTemperature[biome])
	if frozenModifier[biome] {
		// FROZEN_TEMPERATURE_NOISE: three simplex layers at frequencies
		// 1, 1/2, 1/4 with amplitudes 1/7, 2/7, 4/7.
		fx, fz := float64(x)*0.05, float64(z)*0.05
		var large float32
		for i, l := range []struct {
			freq float64
			amp  float32
		}{{1, 0.14285715}, {0.5, 0.2857143}, {0.25, 0.5714286}} {
			large += l.amp * vtFrozenNoise[i].get2(fx*l.freq, fz*l.freq)
		}
		icePatches := float64(large*7) + float64(vtInfoNoise.get2(float64(x)*0.2, float64(z)*0.2))
		if icePatches < 0.3 && vtInfoNoise.get2(float64(x)*0.09, float64(z)*0.09) < 0.8 {
			t = 0.2
		}
	}
	snowLevel := seaLevel + 17
	if y > snowLevel {
		v := vtTempNoise.get2(float64(float32(x)/8), float64(float32(z)/8)) * 8
		return t - (v+float32(y)-float32(snowLevel))*0.05/40
	}
	return t
}

// vtColdEnoughToSnow is Biome.coldEnoughToSnow.
func vtColdEnoughToSnow(biome string, x, y, z, seaLevel int) bool {
	return vtBiomeTemperature(biome, x, y, z, seaLevel) < 0.15
}

// ---- BiomeManager ----

// vtZoomSeed is BiomeManager.obfuscateSeed: SHA-256 of the seed's eight
// little-endian bytes, its first eight bytes as a little-endian long.
func vtZoomSeed(seed int64) int64 {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h := sha256.Sum256(b[:])
	return int64(binary.LittleEndian.Uint64(h[:8]))
}

func vtLCG(r, c int64) int64 {
	r *= r*6364136223846793005 + 1442695040888963407
	return r + c
}

func vtFiddle(r int64) float64 {
	u := float64(floorModInt64(r>>24, 1024)) / 1024.0
	return (u - 0.5) * 0.9
}

func floorModInt64(a, b int64) int64 { return ((a % b) + b) % b }

// vtFuzzedQuart is BiomeManager.getBiome's choice: the quart whose noise
// biome a block shows.
func vtFuzzedQuart(zoom int64, x, y, z int) (int, int, int) {
	ax, ay, az := x-2, y-2, z-2
	px, py, pz := ax>>2, ay>>2, az>>2
	fx, fy, fz := float64(ax&3)/4, float64(ay&3)/4, float64(az&3)/4
	best, bestD := 0, math.Inf(1)
	for i := 0; i < 8; i++ {
		cx, cy, cz := px, py, pz
		dx, dy, dz := fx, fy, fz
		if i&4 != 0 {
			cx, dx = px+1, fx-1
		}
		if i&2 != 0 {
			cy, dy = py+1, fy-1
		}
		if i&1 != 0 {
			cz, dz = pz+1, fz-1
		}
		r := vtLCG(zoom, int64(cx))
		r = vtLCG(r, int64(cy))
		r = vtLCG(r, int64(cz))
		r = vtLCG(r, int64(cx))
		r = vtLCG(r, int64(cy))
		r = vtLCG(r, int64(cz))
		ffx := vtFiddle(r)
		r = vtLCG(r, zoom)
		ffy := vtFiddle(r)
		r = vtLCG(r, zoom)
		ffz := vtFiddle(r)
		d := (dz+ffz)*(dz+ffz) + (dy+ffy)*(dy+ffy) + (dx+ffx)*(dx+ffx)
		if bestD > d {
			best, bestD = i, d
		}
	}
	bx, by, bz := px, py, pz
	if best&4 != 0 {
		bx++
	}
	if best&2 != 0 {
		by++
	}
	if best&1 != 0 {
		bz++
	}
	return bx, by, bz
}
