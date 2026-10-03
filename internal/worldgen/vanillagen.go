package worldgen

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

// The vanilla generator, wired: a world made with GeneratorVanilla
// generates each dimension as 26.3 does — the noise settings' router and
// aquifer (vanillafill.go), the material rules (vanillasurface.go), the
// carvers (vanillacarvers.go, vanillacanyon.go) — with the biome source and
// the placement pass plugged in through the seams (vanillaseams.go). Until
// a part registers, the terrain core stands in: the engine's own biome
// lookup for biomes, and no features.

// vtWorld is one dimension's vanilla generator.
type vtWorld struct {
	g      *Generator
	seed   int64
	dim    Dimension
	preset WorldPreset

	gen  *vtGen
	mat  *vmSystem
	zoom int64 // BiomeManager's obfuscated seed

	biomes   VanillaBiomes
	place    VanillaPlacement
	flat     []uint32 // the flat preset's layers from MinY (nil otherwise)
	carvers  bool     // the overworld carvers run
	standIn  bool     // biomes are the engine's own stand-in
	terrain  [vtTerrainSlots]atomic.Pointer[vtTerrainChunk]
	heights  [vtHeightSlots]atomic.Pointer[vtHeights]
	warnOnce sync.Once
}

const (
	vtTerrainSlots = 64   // pre-feature chunks kept (≈400 KB each)
	vtHeightSlots  = 4096 // chunks' surface heights kept
)

// vtTerrainChunk is a chunk's terrain before features.
type vtTerrainChunk struct {
	cx, cz int32
	ch     *Chunk
}

// vtHeights is a chunk's terrain heights: the highest non-air block and
// the highest solid (non-fluid) block per column (MinY-1 for none).
type vtHeights struct {
	cx, cz        int32
	surface, land [256]int16
}

// presetSettings is the noise settings a preset's overworld uses.
func presetSettings(p WorldPreset) string {
	switch p {
	case PresetLargeBiomes:
		return "minecraft:large_biomes"
	case PresetAmplified:
		return "minecraft:amplified"
	case PresetCaves:
		return "minecraft:caves"
	case PresetFloatingIslands:
		return "minecraft:floating_islands"
	}
	return "minecraft:overworld"
}

// GeneratorMode is the generator's mode.
func (g *Generator) GeneratorMode() GeneratorMode {
	if g.vw != nil {
		return GeneratorVanilla
	}
	return GeneratorNative
}

// Preset is a vanilla generator's world preset (PresetNormal in native mode).
func (g *Generator) Preset() WorldPreset {
	if g.vw != nil {
		return g.vw.preset
	}
	return PresetNormal
}

// SetGenerator picks the world's generator. Must be called at boot, before
// any chunk is generated. Vanilla mode implies vanilla caves (they are part
// of vanilla's final density); it cannot combine with earth mode or a raised
// ceiling (vanilla's noise settings fix the world's height).
func (g *Generator) SetGenerator(m GeneratorMode, p WorldPreset) error {
	g.vw = nil
	if m != GeneratorVanilla {
		return nil
	}
	if g.earth != nil {
		return errors.New("the vanilla generator cannot run in earth mode")
	}
	if g.sections != SectionCount {
		return errors.New("the vanilla generator cannot raise the ceiling")
	}
	dim := DimOverworld
	switch {
	case g.nether:
		dim = DimNether
	case g.end:
		dim = DimEnd
	}
	if dim != DimOverworld {
		// The Nether and the End are whole-dimension generators of their
		// own (RegisterVanillaDimension), which the world installs from the
		// world seed (NewNetherGenerator and NewEndGenerator scramble
		// theirs); this switch is the overworld's.
		return nil
	}
	vw := &vtWorld{g: g, seed: g.seed, dim: dim, preset: p, zoom: vtZoomSeed(g.seed)}
	g.SetCaveMode(CavesVanilla)
	if p == PresetFlat {
		vw.flat = vtFlatLayers()
		vw.biomes = vtFixedBiome("minecraft:plains")
		g.vw = vw
		return nil
	}
	gen, err := newVTGen(presetSettings(p), g.seed)
	if err != nil {
		return err
	}
	mat, err := newVMSystem(gen)
	if err != nil {
		return err
	}
	vw.gen, vw.mat, vw.carvers = gen, mat, true
	ctx := VanillaGenContext{Seed: g.seed, Dim: dim, Preset: p, Climate: vtClimate{gen}, Terrain: vtTerrain{vw}, Gen: g}
	switch {
	case p == PresetSingleBiome:
		vw.biomes = vtFixedBiome("minecraft:plains")
	case vanillaBiomesCtor != nil:
		vw.biomes = vanillaBiomesCtor(ctx)
	}
	if vw.biomes == nil {
		vw.biomes = vtNativeBiomes{NewGenerator(g.seed)}
		vw.standIn = true
		log.Printf("vanilla generator: no vanilla biome source registered; the engine's biome lookup stands in")
	}
	ctx.Biomes = vw.biomes
	if vanillaPlacementCtor != nil {
		vw.place = vanillaPlacementCtor(ctx)
	}
	if vw.place == nil {
		log.Printf("vanilla generator: no vanilla placement registered; chunks generate without features")
	}
	g.vw = vw
	return nil
}

func dimName(d Dimension) string {
	switch d {
	case DimNether:
		return "Nether"
	case DimEnd:
		return "End"
	}
	return "overworld"
}

// vtSectionBiomes, when set, picks a chunk's per-section biomes from the
// biome source (the biome source's VanillaSectionBiomes: the most common
// quart of each section).
var vtSectionBiomes func(b VanillaBiomes, cx, cz int32, minY, sections int) []string

// vtFlatLayers is the flat preset's layers: bedrock, two dirt, grass.
func vtFlatLayers() []uint32 { return []uint32{Bedrock, Dirt, Dirt, GrassBlock} }

// vtFixedBiome is FixedBiomeSource.
type vtFixedBiome string

func (b vtFixedBiome) BiomeAt(int, int, int) string { return string(b) }

// vtNativeBiomes is the stand-in biome source: the engine's own lookup at
// the quart's column, from a native generator of the seed (the vanilla
// one's heights would ask the biome source back).
type vtNativeBiomes struct{ g *Generator }

func (b vtNativeBiomes) BiomeAt(qx, _, qz int) string { return b.g.resolveBiome(qx<<2, qz<<2).Name }

// vtClimate is VanillaClimate over a dimension's router: Climate.Sampler
// at a quart's minimum corner.
type vtClimate struct{ g *vtGen }

func (c vtClimate) Sample(qx, qy, qz int) ClimatePoint {
	ctx := newVDCtx()
	x, y, z := qx<<2, qy<<2, qz<<2
	g := c.g
	return ClimatePoint{
		Temperature:     QuantizeClimate(g.temperature.value(ctx, x, y, z)),
		Humidity:        QuantizeClimate(g.vegetation.value(ctx, x, y, z)),
		Continentalness: QuantizeClimate(g.continents.value(ctx, x, y, z)),
		Erosion:         QuantizeClimate(g.erosion.value(ctx, x, y, z)),
		Depth:           QuantizeClimate(g.depth.value(ctx, x, y, z)),
		Weirdness:       QuantizeClimate(g.ridges.value(ctx, x, y, z)),
	}
}

// SampleColumn is Sample over the quarts qy0, qy0+1, … of one column, the
// column's horizontal functions computed once.
func (c vtClimate) SampleColumn(qx, qz, qy0 int, out []ClimatePoint) {
	ctx := newVDCtx()
	x, z := qx<<2, qz<<2
	g := c.g
	for i := range out {
		y := (qy0 + i) << 2
		out[i] = ClimatePoint{
			Temperature:     QuantizeClimate(g.temperature.value(ctx, x, y, z)),
			Humidity:        QuantizeClimate(g.vegetation.value(ctx, x, y, z)),
			Continentalness: QuantizeClimate(g.continents.value(ctx, x, y, z)),
			Erosion:         QuantizeClimate(g.erosion.value(ctx, x, y, z)),
			Depth:           QuantizeClimate(g.depth.value(ctx, x, y, z)),
			Weirdness:       QuantizeClimate(g.ridges.value(ctx, x, y, z)),
		}
	}
}

// vtTerrain is VanillaTerrain over a dimension's vanilla generator.
type vtTerrain struct{ w *vtWorld }

func (t vtTerrain) BlockAt(x, y, z int) uint32 { return t.w.blockAt(x, y, z) }

// Height is ChunkGenerator.getBaseHeight: the noise column alone (no
// surface, no carvers), as structure placement reads it.
func (t vtTerrain) Height(kind HeightmapType, x, z int) int { return t.w.baseHeight(kind, x, z) }
func (t vtTerrain) MinY() int                               { return t.w.minY() }
func (t vtTerrain) Ceiling() int                            { return t.w.minY() + t.w.depth() }
func (t vtTerrain) SeaLevel() int                           { return t.w.seaLevel() }

func (w *vtWorld) minY() int {
	if w.gen != nil {
		return w.gen.set.minY
	}
	return MinY
}

func (w *vtWorld) depth() int {
	if w.gen != nil {
		return w.gen.set.height
	}
	return SectionCount * 16
}

func (w *vtWorld) seaLevel() int {
	if w.gen != nil {
		return w.gen.set.seaLevel
	}
	return SeaLevel
}

// baseHeight is getBaseHeight for the WG heightmaps.
func (w *vtWorld) baseHeight(kind HeightmapType, x, z int) int {
	if w.flat != nil {
		return MinY + len(w.flat)
	}
	stop := func(_ int, s uint32) bool { return s != Air }
	if kind == HeightOceanFloorWG || kind == HeightOceanFloor {
		stop = func(_ int, s uint32) bool { return s != Air && !vtIsFluid(s) }
	}
	y := w.gen.baseColumn(x, z, stop)
	if y < w.gen.set.minY {
		return w.gen.set.minY
	}
	return y + 1
}

// noiseBiome is the biome source at a quart.
func (w *vtWorld) noiseBiome(qx, qy, qz int) string { return w.biomes.BiomeAt(qx, qy, qz) }

// biomeAt is BiomeManager.getBiome: the fuzzed noise biome a block shows,
// its quart's y held in the dimension (as a chunk's stored biomes are)
// when clamp is set.
func (w *vtWorld) biomeAt(x, y, z int, clamp bool) string {
	qx, qy, qz := vtFuzzedQuart(w.zoom, x, y, z)
	if clamp {
		lo := w.minY() >> 2
		hi := lo + w.depth()>>2 - 1
		qy = max(lo, min(qy, hi))
	}
	return w.noiseBiome(qx, qy, qz)
}

// terrainChunk is the chunk's terrain before features: filled, surfaced,
// carved. Kept in a small cache; callers must not modify it.
func (w *vtWorld) terrainChunk(cx, cz int32) *Chunk {
	slot := &w.terrain[uint32(cx*31+cz*17)&(vtTerrainSlots-1)]
	if t := slot.Load(); t != nil && t.cx == cx && t.cz == cz {
		return t.ch
	}
	ch := w.buildTerrain(cx, cz)
	slot.Store(&vtTerrainChunk{cx, cz, ch})
	return ch
}

// buildTerrain is buildTerrain: doFill, buildSurface, generateCarvers.
func (w *vtWorld) buildTerrain(cx, cz int32) *Chunk {
	ch := NewChunk(SectionCount)
	if w.flat != nil {
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				for i, s := range w.flat {
					ch.setGen(x, MinY+i, z, s)
				}
			}
		}
		return ch
	}
	var beard vdSampler
	if bs, ok := w.place.(VanillaBeards); ok {
		if b := newVTBeardifier(bs.BeardsFor(cx, cz)); b != nil {
			beard = b
		}
	}
	nc := w.gen.fillChunk(ch, cx, cz, beard)
	surfaceBiome := func(x, y, z int) string { return w.biomeAt(x, y, z, true) }
	w.mat.buildSurface(ch, cx, cz, nc, surfaceBiome)
	if w.carvers {
		w.carve(ch, cx, cz, nc)
	}
	return ch
}

// carve is generateCarvers and applyCarvingMask.
func (w *vtWorld) carve(ch *Chunk, cx, cz int32, nc *vtNoiseChunk) {
	minY := w.gen.set.minY + 1
	maxY := w.gen.set.minY + w.gen.set.height - 1 - 7
	mask := make([]uint64, (len(ch.Sections)*4096+63)/64)
	vtCarveMask(w.seed, cx, cz, minY, maxY, w.gen.set.height, mask)
	bit := func(x, y, z int) bool {
		i := (y-MinY)*256 + z*16 + x
		return mask[i>>6]>>(i&63)&1 != 0
	}
	topBiome := func(x, y, z int) string { return w.biomeAt(x, y, z, false) }
	bx, bz := int(cx)*16, int(cz)*16
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			// The column's runs of carved cells, bottom run first, each cut
			// from its top down (CarvingMask.visit).
			for y := minY; y <= maxY; y++ {
				if !bit(x, y, z) {
					continue
				}
				top := y
				for top+1 <= maxY && bit(x, top+1, z) {
					top++
				}
				w.carveRun(ch, nc, x, z, bx, bz, y, top, topBiome)
				y = top
			}
		}
	}
}

func (w *vtWorld) carveRun(ch *Chunk, nc *vtNoiseChunk, x, z, bx, bz, bottom, top int, topBiome func(x, y, z int) string) {
	grass := false
	wx, wz := bx+x, bz+z
	for y := top; y >= bottom; y-- {
		old := ch.getGen(x, y, z)
		if old == Bedrock {
			continue
		}
		if old == GrassBlock || old == Mycelium {
			grass = true
		}
		var code int8
		if nc.aq != nil {
			code = nc.aq.substance(wx, y, wz, 0)
		} else {
			code = w.gen.globalFluid(y).at(y)
		}
		if code == vaqSolid {
			continue
		}
		s := Air
		switch code {
		case vaqWater:
			s = w.gen.set.waterState()
		case vaqLava:
			s = Lava
		}
		ch.setGen(x, y, z, s)
		if grass && ch.getGen(x, y-1, z) == Dirt {
			if t, ok := w.mat.topMaterial(ch, nc, topBiome, wx, y-1, wz, s != Air); ok {
				ch.setGen(x, y-1, z, t)
			}
		}
	}
}

// heightsOf is the chunk's terrain heights (cached).
func (w *vtWorld) heightsOf(cx, cz int32) *vtHeights {
	slot := &w.heights[uint32(cx*131+cz*71)&(vtHeightSlots-1)]
	if h := slot.Load(); h != nil && h.cx == cx && h.cz == cz {
		return h
	}
	ch := w.terrainChunk(cx, cz)
	h := &vtHeights{cx: cx, cz: cz}
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			h.surface[z*16+x] = int16(vtHeightWS(ch, x, z))
			land := MinY - 1
			for y := int(h.surface[z*16+x]); y >= MinY; y-- {
				if s := ch.getGen(x, y, z); !vtIsAir(s) && !vtIsFluid(s) {
					land = y
					break
				}
			}
			h.land[z*16+x] = int16(land)
		}
	}
	slot.Store(h)
	return h
}

func (w *vtWorld) blockAt(x, y, z int) uint32 {
	ch := w.terrainChunk(int32(x>>4), int32(z>>4))
	return ch.getGen(x&15, y, z&15)
}

// landHeight is one above the column's highest solid terrain block.
func (w *vtWorld) landHeight(x, z int) int {
	h := w.heightsOf(int32(x>>4), int32(z>>4))
	return int(h.land[(z&15)*16+x&15]) + 1
}

// generateChunk is the vanilla GenerateChunk.
func (w *vtWorld) generateChunk(cx, cz int32) *Chunk {
	t := w.terrainChunk(cx, cz)
	ch := NewChunk(len(t.Sections))
	copy(ch.Sections, t.Sections)
	// One biome per section (the engine stores no more): the biome source's
	// pick for the section when it has one, else the section's middle quart.
	if vtSectionBiomes != nil {
		copy(ch.Biomes, vtSectionBiomes(w.biomes, cx, cz, MinY, len(ch.Biomes)))
	} else {
		qx, qz := int(cx)*4+2, int(cz)*4+2
		for s := range ch.Biomes {
			ch.Biomes[s] = w.noiseBiome(qx, (MinY>>2)+s*4+2, qz)
		}
	}
	if w.place != nil {
		w.place.Decorate(ch, cx, cz)
	}
	ch.computeHeightmap()
	return ch
}

// biomeName is BiomeName in vanilla mode: the biome shown at the surface.
func (w *vtWorld) biomeName(x, z int) string {
	h := w.heightsOf(int32(x>>4), int32(z>>4))
	return w.biomeAt(x, int(h.surface[(z&15)*16+x&15])+1, z, true)
}

// surfaceY is SurfaceY in vanilla mode.
func (w *vtWorld) surfaceY(x, z int) float64 {
	return float64(max(w.landHeight(x, z), w.seaLevel()))
}

// VanillaSpawnFitness is the vanilla generator's NoiseSpawnFinder fitness
// at a column: its noise settings' spawn targets scored on the router's
// climate (0 where a target is met). ok is false in native mode, or for a
// preset without spawn targets.
func (g *Generator) VanillaSpawnFitness(x, z int) (fitness int64, ok bool) {
	if g.vw == nil || g.vw.gen == nil {
		return 0, false
	}
	return g.vw.gen.spawnFitness(x, z)
}

// String names the mode and preset for logs.
func (w *vtWorld) String() string {
	return fmt.Sprintf("vanilla %s (%s)", dimName(w.dim), w.preset)
}
