package worldgen

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// The placement pass of the vanilla generator (VanillaPlacement): a
// chunk's structures, sited where 26.3 sites them (vanillastructs.go) and
// built by the engine's structure code, then the eleven decoration steps of
// the chunk and its eight neighbours (vanilladecorate.go), each feature
// placed by vanillafeatures.go and kept where it falls in this chunk.
//
// Features read the terrain before features, never the chunk being
// written, so every chunk that replays a neighbour's decoration draws it
// the same way; a feature does not write over a structure's blocks.

func init() { RegisterVanillaPlacement(newVanillaPlacer) }

// vanillaPlacer is one dimension's placement pass.
type vanillaPlacer struct {
	g       *Generator
	dim     Dimension
	terrain VanillaTerrain // the terrain before features; its WG heightmaps
	base    *vpHeightCache // getBaseHeight's noise-only heights, which structures read
	biomes  VanillaBiomes
	zoom    int64
	decor   *vanillaDecor
	structs *vanillaStructs

	bmu    sync.Mutex
	bcache map[[2]int32][]string // a chunk's noise biomes (short names)
}

// vpPlacers finds a generator's placement pass (the structure code asks
// it where to put things); vpPlacerCount lets a native-only process skip
// the lookup.
var (
	vpPlacers     sync.Map // *Generator → *vanillaPlacer
	vpPlacerCount atomic.Int32
)

// newVanillaPlacer builds a dimension's placement pass.
func newVanillaPlacer(ctx VanillaGenContext) VanillaPlacement {
	if ctx.Terrain == nil || ctx.Biomes == nil {
		return nil
	}
	dim := map[Dimension]string{DimOverworld: "overworld", DimNether: "nether", DimEnd: "end"}[ctx.Dim]
	if dim == "" {
		return nil
	}
	p := &vanillaPlacer{g: ctx.Gen, dim: ctx.Dim, terrain: ctx.Terrain, base: newVPHeightCache(ctx.Terrain), biomes: ctx.Biomes,
		zoom: vpObfuscateSeed(ctx.Seed), bcache: map[[2]int32][]string{}}
	decor, err := newVanillaDecor(ctx.Seed, dim)
	if err != nil {
		return nil
	}
	p.decor = decor
	// WorldGenerationContext: the level's limits within the noise settings'.
	lo := max(ctx.Terrain.MinY(), p.decor.minY)
	p.decor.height = min(ctx.Terrain.Ceiling(), p.decor.minY+p.decor.height) - lo
	p.decor.minY, p.decor.seaLevel = lo, ctx.Terrain.SeaLevel()
	p.structs = newVanillaStructs(ctx.Seed, dim, p.noiseBiome, p.base)
	if ctx.Preset == PresetSingleBiome && ctx.Dim == DimOverworld {
		p.structs.fixed = vpShort(ctx.Biomes.BiomeAt(0, 0, 0))
	}
	if ctx.Gen != nil {
		if _, had := vpPlacers.Swap(ctx.Gen, p); !had {
			vpPlacerCount.Add(1)
		}
	}
	return p
}

// vanillaPlacerOf is the generator's vanilla placement pass, nil in native
// mode (and in the dimensions it does not run).
func (g *Generator) vanillaPlacerOf() *vanillaPlacer {
	if g == nil || vpPlacerCount.Load() == 0 {
		return nil
	}
	if v, ok := vpPlacers.Load(g); ok {
		return v.(*vanillaPlacer)
	}
	return nil
}

// noiseBiome is the biome source at a quart cell, by short name, with the
// cell's y clamped into the world as a chunk's stored biomes are.
func (p *vanillaPlacer) noiseBiome(qx, qy, qz int) string {
	lo, hi := p.terrain.MinY()>>2, (p.terrain.Ceiling()>>2)-1
	qy = min(max(qy, lo), hi)
	return vpShort(p.biomes.BiomeAt(qx, qy, qz))
}

// chunkBiomes is the set of noise biomes a chunk's sections hold.
func (p *vanillaPlacer) chunkBiomes(cx, cz int32) []string {
	k := [2]int32{cx, cz}
	p.bmu.Lock()
	if b, ok := p.bcache[k]; ok {
		p.bmu.Unlock()
		return b
	}
	p.bmu.Unlock()
	seen := map[string]bool{}
	var out []string
	lo, hi := p.terrain.MinY()>>2, (p.terrain.Ceiling()>>2)-1
	for qy := lo; qy <= hi; qy++ {
		for qx := 0; qx < 4; qx++ {
			for qz := 0; qz < 4; qz++ {
				b := p.noiseBiome(int(cx)*4+qx, qy, int(cz)*4+qz)
				if !seen[b] {
					seen[b] = true
					out = append(out, b)
				}
			}
		}
	}
	p.bmu.Lock()
	if len(p.bcache) > 8192 {
		p.bcache = map[[2]int32][]string{}
	}
	p.bcache[k] = out
	p.bmu.Unlock()
	return out
}

// neighbourhoodBiomes is the biomes of a chunk's 3x3 neighbourhood: what
// applyBiomeDecoration's possible-feature set is drawn from.
func (p *vanillaPlacer) neighbourhoodBiomes(cx, cz int32) []string {
	seen := map[string]bool{}
	var out []string
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			for _, b := range p.chunkBiomes(cx+dx, cz+dz) {
				if !seen[b] {
					seen[b] = true
					out = append(out, b)
				}
			}
		}
	}
	return out
}

// vpTerrainLevel is placement's view of the world: the terrain before
// features, its heightmaps (the feature-dependent ones as the terrain
// gives them), and the biome a block shows (BiomeManager's zoom).
type vpTerrainLevel struct{ p *vanillaPlacer }

func (l vpTerrainLevel) Block(x, y, z int) uint32 {
	if y < l.p.terrain.MinY() || y >= l.p.terrain.Ceiling() {
		return Air
	}
	return l.p.terrain.BlockAt(x, y, z)
}

// Height is the heightmap decoration reads: the terrain's own WG
// heightmaps (noise, surface and carvers — what a chunk keeps up as it is
// carved), which stand for the feature-dependent kinds too.
func (l vpTerrainLevel) Height(hm HeightmapType, x, z int) int {
	if hm == HeightOceanFloor || hm == HeightOceanFloorWG {
		return l.p.terrain.Height(HeightOceanFloorWG, x, z)
	}
	return l.p.terrain.Height(HeightWorldSurfaceWG, x, z)
}

func (l vpTerrainLevel) Biome(x, y, z int) string {
	qx, qy, qz := vpZoom(l.p.zoom, x, y, z)
	return l.p.noiseBiome(qx, qy, qz)
}

// Decorate is the placement pass for one chunk.
func (p *vanillaPlacer) Decorate(ch *Chunk, cx, cz int32) {
	if p.g != nil {
		switch p.dim {
		case DimNether:
			p.g.stampVanillaNetherStructures(ch, cx, cz)
		case DimEnd:
			p.g.stampEndCities(ch, cx, cz) // the End's generator sites them as 26.3 does
		default:
			p.g.stampVanillaStructures(ch, cx, cz)
		}
	}
	lv := vpTerrainLevel{p}
	e := p.newExec(ch, cx, cz)
	// A structure's blocks are not the terrain's: features keep off them.
	e.protect = make([]bool, len(ch.Sections)*4096)
	for s := range ch.Sections {
		for i := 0; i < 4096; i++ {
			y := MinY + s*16 + i>>8
			x, z := e.baseX+i&15, e.baseZ+(i>>4)&15
			if ch.Sections[s][i] != lv.Block(x, y, z) {
				e.protect[s*4096+i] = true
			}
		}
	}
	type src struct {
		cx, cz int32
		biomes []string
	}
	var srcs []src
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			srcs = append(srcs, src{cx + dx, cz + dz, p.neighbourhoodBiomes(cx+dx, cz+dz)})
		}
	}
	for step := 0; step < vpStepCount; step++ {
		for _, s := range srcs {
			p.decor.decorateSteps(s.cx, s.cz, lv, s.biomes, step, step, func(int, int, *vpPlacedFeature, *vwRandom) {
				e.beginFeature()
			}, func(pl *vpPlacement) {
				e.place(pl.placed.feature(), pl.rng, pl.pos)
			}, nil)
		}
	}
}

// newExec is the feature placer for one chunk.
func (p *vanillaPlacer) newExec(ch *Chunk, cx, cz int32) *vpExec {
	e := &vpExec{ch: ch, baseX: int(cx) << 4, baseZ: int(cz) << 4, terrain: p.terrain, g: p.g}
	e.ctx = &vpCtx{lv: vpOverlayLevel{vpTerrainLevel{p}, e}, minY: p.decor.minY, height: p.decor.height, seaLevel: p.decor.seaLevel}
	if p.g != nil {
		e.cold = func(b string, x, y, z int) bool { return p.g.coldEnoughToSnow("minecraft:"+b, x, y, z) }
	} else {
		e.cold = func(b string, x, y, z int) bool {
			t, ok := biomeTemperature["minecraft:"+b]
			return ok && t < snowTemperature && y <= SeaLevel+17
		}
	}
	return e
}

// vpObfuscateSeed is BiomeManager.obfuscateSeed: the first eight bytes of
// the SHA-256 of the seed (little-endian both ways, as Guava hashes a long).
func vpObfuscateSeed(seed int64) int64 {
	var b [8]byte
	for i := range b {
		b[i] = byte(uint64(seed) >> (8 * i))
	}
	h := sha256.Sum256(b[:])
	var v uint64
	for i := 7; i >= 0; i-- {
		v = v<<8 | uint64(h[i])
	}
	return int64(v)
}

// vpZoom is BiomeManager.getBiome's choice of quart for a block: the
// nearest of the eight surrounding quart corners, each jittered by a
// per-corner hash of the obfuscated seed.
func vpZoom(seed int64, x, y, z int) (int, int, int) {
	ax, ay, az := x-2, y-2, z-2
	px, py, pz := ax>>2, ay>>2, az>>2
	fx, fy, fz := float64(ax&3)/4, float64(ay&3)/4, float64(az&3)/4
	best, bestD := 0, 0.0
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
		d := vpFiddled(seed, cx, cy, cz, dx, dy, dz)
		if i == 0 || bestD > d {
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

func vpLCG(r, c int64) int64 { return r*(r*6364136223846793005+1442695040888963407) + c }

func vpFiddled(seed int64, x, y, z int, dx, dy, dz float64) float64 {
	r := vpLCG(seed, int64(x))
	r = vpLCG(r, int64(y))
	r = vpLCG(r, int64(z))
	r = vpLCG(r, int64(x))
	r = vpLCG(r, int64(y))
	r = vpLCG(r, int64(z))
	fx := vpFiddle(r)
	r = vpLCG(r, seed)
	fy := vpFiddle(r)
	r = vpLCG(r, seed)
	fz := vpFiddle(r)
	return (dz+fz)*(dz+fz) + (dy+fy)*(dy+fy) + (dx+fx)*(dx+fx)
}

func vpFiddle(r int64) float64 {
	u := float64(((r>>24)%1024+1024)%1024) / 1024
	return (u - 0.5) * 0.9
}

// vpHeightCache is ChunkGenerator.getBaseHeight for structure starts: the
// terrain's noise-only base heights (VanillaBaseHeights) when it has them,
// else its heightmaps, cached by column — a base height is a whole noise
// column, and starts ask for the same corners again and again. It never
// reads a generated chunk, so a structure start (and the beards made from
// it) cannot wait on the terrain it shapes.
type vpHeightCache struct {
	VanillaTerrain
	mu    sync.Mutex
	cache map[[3]int32]int16
}

func newVPHeightCache(t VanillaTerrain) *vpHeightCache {
	return &vpHeightCache{VanillaTerrain: t, cache: map[[3]int32]int16{}}
}

// Height is the column's base height, computed once per column and kind.
func (c *vpHeightCache) Height(kind HeightmapType, x, z int) int {
	if kind == HeightOceanFloor {
		kind = HeightOceanFloorWG
	} else if kind != HeightOceanFloorWG {
		kind = HeightWorldSurfaceWG
	}
	k := [3]int32{int32(x), int32(z), int32(kind)}
	c.mu.Lock()
	h, ok := c.cache[k]
	c.mu.Unlock()
	if ok {
		return int(h)
	}
	var v int
	if b, ok := c.VanillaTerrain.(VanillaBaseHeights); ok {
		v = b.BaseHeight(kind, x, z)
	} else {
		v = c.VanillaTerrain.Height(kind, x, z)
	}
	c.mu.Lock()
	if len(c.cache) >= 1<<20 {
		c.cache = map[[3]int32]int16{}
	}
	c.cache[k] = int16(v)
	c.mu.Unlock()
	return v
}

// land is the base height of a column's land (OCEAN_FLOOR_WG: the first
// cell above its highest non-fluid block), surface its WORLD_SURFACE_WG.
func (p *vanillaPlacer) land(x, z int) int    { return p.base.Height(HeightOceanFloorWG, x, z) }
func (p *vanillaPlacer) surface(x, z int) int { return p.base.Height(HeightWorldSurfaceWG, x, z) }
