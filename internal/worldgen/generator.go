package worldgen

import "math"

const (
	MinY         = -64
	SectionCount = 24 // default: 384 blocks / 16 (vanilla-height world)
	SeaLevel     = 63
	// MaxSectionCount bounds a configurable-height world: Java dimension
	// heights top out at y=2032 and MinY is fixed, so (2032+64)/16.
	MaxSectionCount = 131
)

// Generator turns a world seed into terrain. Each field is an independent noise
// layer seeded from the world seed, so the same seed always reproduces the world.
type Generator struct {
	seed      int64
	continent *Perlin // very low frequency: ocean ↔ coast ↔ inland highland
	hills     *Perlin // medium frequency: rolling relief
	detail    *Perlin // high frequency: surface roughness
	temp      *Perlin // temperature field (before altitude adjustment)
	humid     *Perlin // humidity field
	forest    *Perlin // low frequency: tree density (forests vs clearings)
	erosion   *Perlin // regional flatness vs mountainousness
	peaks     *Perlin // ridged noise for sharp mountain ridgelines
	variety   *Perlin // biome sub-variant selector (plains↔sunflower, forest↔flower)
	river     *Perlin // river channels (low-|value| bands carve to sea level)
	cave      *Perlin // underground biome selector (dripstone/lush/deep_dark)
	caveA     *Perlin // 3D cave field A
	caveB     *Perlin // 3D cave field B (tunnels where A and B both ≈ 0)
	nether    bool    // nether mode: cavern-sponge assembly, no surface features
	end       bool    // End mode: floating island + pillar ring, void elsewhere

	// earth mode (earth.go): terrain heights come from a real elevation model
	// instead of the noise stack; rivers and caves are disabled (the DEM has
	// real valleys, and carving real mountains would falsify them). All other
	// systems derive from Height() and work unchanged.
	earth *EarthDEM

	// sections is the world's column height in 16-block sections (the ceiling
	// is MinY + sections*16). Defaults to SectionCount (the vanilla-height
	// world); earth mode raises it (SetCeiling) so real mountains fit at true
	// vertical scale. Nether/End generators are never raised.
	sections int
}

// SetCeiling raises the world ceiling to maxY (exclusive top build limit,
// rounded up to a section boundary). Must be called at boot, before any chunk
// is generated.
func (g *Generator) SetCeiling(maxY int) {
	s := (maxY - MinY + 15) / 16
	if s < SectionCount {
		s = SectionCount
	}
	if s > MaxSectionCount {
		s = MaxSectionCount
	}
	g.sections = s
}

// SectionCount is the world's column height in 16-block sections.
func (g *Generator) SectionCount() int { return g.sections }

// Ceiling is the exclusive top build limit (world Y).
func (g *Generator) Ceiling() int { return MinY + g.sections*16 }

func NewGenerator(seed int64) *Generator {
	return &Generator{
		sections:  SectionCount,
		seed:      seed,
		continent: NewPerlin(seed ^ 0x1),
		hills:     NewPerlin(seed ^ 0x2),
		detail:    NewPerlin(seed ^ 0x3),
		temp:      NewPerlin(seed ^ 0x4),
		humid:     NewPerlin(seed ^ 0x5),
		forest:    NewPerlin(seed ^ 0x6),
		erosion:   NewPerlin(seed ^ 0xA),
		peaks:     NewPerlin(seed ^ 0xB),
		variety:   NewPerlin(seed ^ 0xC),
		river:     NewPerlin(seed ^ 0xD),
		cave:      NewPerlin(seed ^ 0xE),
		caveA:     NewPerlin(seed ^ 0x8),
		caveB:     NewPerlin(seed ^ 0x9),
	}
}

const (
	caveThreshold    = 0.02     // larger = more/wider caves
	caveMinY         = MinY + 2 // leave a floor crust above bedrock
	caveSurfaceTaper = 16       // openings narrow over the top N blocks of land
	caveSurfaceFrac  = 0.30     // threshold at the very surface, as a fraction of full
	caveSeaFloorGap  = 5        // keep this many solid blocks under the sea floor
)

// carveable reports whether a generated block may be hollowed into a cave. We
// carve soil as well as stone so cave mouths can break through the grass/dirt
// crust to the surface; water and bedrock are never carved.
func carveable(b uint32) bool {
	switch b {
	case Stone, Deepslate, Dirt, GrassBlock, Sand, Sandstone, Gravel, SnowBlock,
		CoarseDirt, Podzol, Mycelium, RedSand, RedSandstone, Mud, Terracotta:
		return true
	}
	return false
}

// carve returns Air if (wx,wy,wz) — a solid block at column height colH — should
// be hollowed into a cave, otherwise the block unchanged.
//
// Caves are a 3D dual-noise field (tunnels where both fields ≈ 0). On land the
// field reaches all the way to the surface, but the carving threshold tapers
// down near the top so only the centres of tunnels punch through — giving
// occasional natural entrances (pits, hillside mouths) rather than a pockmarked
// crust. Under oceans we stop short of the sea floor so the seabed stays intact.
func (g *Generator) carve(b uint32, wx, wy, wz, colH int) uint32 {
	if g.earth != nil {
		return b // real terrain: no noise caves tunnelling through real mountains
	}
	if wy < caveMinY || !carveable(b) {
		return b
	}

	ceil := colH
	underwater := colH <= SeaLevel+1
	if underwater {
		ceil = colH - caveSeaFloorGap
	}
	if wy >= ceil {
		return b
	}

	thr := caveThreshold
	if !underwater {
		if d := ceil - 1 - wy; d < caveSurfaceTaper {
			thr *= caveSurfaceFrac + (1-caveSurfaceFrac)*float64(d)/caveSurfaceTaper
		}
	}

	a := g.caveA.Noise3(float64(wx)/64, float64(wy)/40, float64(wz)/64)
	c := g.caveB.Noise3(float64(wx)/64, float64(wy)/40, float64(wz)/64)
	if a*a+c*c < thr {
		return Air
	}
	return b
}

// Chunk holds generated block states and one biome per section. Biomes are
// identifiers (e.g. "minecraft:plains"); the wire layer maps them to network
// IDs, so worldgen stays free of protocol concerns.
//
// Heightmap is the highest non-air block's world-Y per column (index lz*16+lx),
// or MinY-1 for an all-air column. It's computed once at generation and feeds
// both the lighting engine (capping the flood fill to just above the surface)
// and the chunk packet's heightmap field.
type Chunk struct {
	Sections  [][4096]uint32
	Biomes    []string
	Heightmap [256]int16
}

// NewChunk allocates an empty (all-air) chunk column of the given height.
func NewChunk(sections int) *Chunk {
	return &Chunk{
		Sections: make([][4096]uint32, sections),
		Biomes:   make([]string, sections),
	}
}

// Equal reports whether two chunks have identical contents (tests; Chunk
// holds slices, so struct comparison no longer works).
func (ch *Chunk) Equal(o *Chunk) bool {
	if len(ch.Sections) != len(o.Sections) || ch.Heightmap != o.Heightmap {
		return false
	}
	for i := range ch.Sections {
		if ch.Sections[i] != o.Sections[i] || ch.Biomes[i] != o.Biomes[i] {
			return false
		}
	}
	return true
}

// computeHeightmap fills Heightmap from the (decorated) section data.
func (ch *Chunk) computeHeightmap() {
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			ch.Heightmap[lz*16+lx] = ch.columnHeight(lx, lz)
		}
	}
}

// columnHeight scans one column top-down for its highest non-air block.
func (ch *Chunk) columnHeight(lx, lz int) int16 {
	for s := len(ch.Sections) - 1; s >= 0; s-- {
		for ly := 15; ly >= 0; ly-- {
			if ch.Sections[s][(ly*16+lz)*16+lx] != Air {
				return int16(MinY + s*16 + ly)
			}
		}
	}
	return int16(MinY - 1)
}

// RecomputeHeightmapColumns rescans the marked columns (indexed lz*16+lx) —
// used after the edit overlay changes a column, so the client-facing
// heightmap reflects player builds (the client gates precipitation
// rendering on it: without this, rain and snow fall through built roofs on
// freshly loaded chunks).
func (ch *Chunk) RecomputeHeightmapColumns(cols *[256]bool) {
	for i, touched := range cols {
		if touched {
			ch.Heightmap[i] = ch.columnHeight(i&15, i>>4)
		}
	}
}

// Height returns the surface height (the first air block over land) at a column.
// Continentalness sets the base elevation — deep ocean to high inland — and the
// highlands get progressively more rugged, so coasts are smooth and peaks jagged.
func (g *Generator) Height(wx, wz int) int {
	if g.nether {
		return g.NetherFloor(wx, wz)
	}
	if g.end {
		return EndSurfaceY + 2
	}
	h := g.landHeight(wx, wz)
	if g.earth != nil {
		return h // real terrain: the DEM already carries its valleys and rivers
	}
	// Rivers carve shallow channels through low-lying land down to just below
	// the waterline.
	if d := g.riverDepth(wx, wz, h); d > 0 {
		if t := SeaLevel - 1 - d; t < h {
			h = t
		}
	}
	return h
}

// landHeight is the un-carved terrain height (continentalness + erosion +
// ridged peaks). Height() layers river carving on top. In earth mode it is
// the real elevation model — the single seam every terrain consumer shares.
func (g *Generator) landHeight(wx, wz int) int {
	if g.earth != nil {
		return g.earth.heightAt(wx, wz, g.Ceiling())
	}
	x, z := float64(wx), float64(wz)
	// fBm output is compressed to ~±0.4, so stretch the regional fields to use a
	// fuller range — that's what lets continentalness and erosion reach extremes.
	cont := stretch(g.continent.FBm(x/1100, z/1100, 4, 2, 0.5)) // ocean ↔ inland
	ero := stretch(g.erosion.FBm(x/650, z/650, 3, 2, 0.5))      // flat ↔ mountainous
	hill := g.hills.FBm(x/200, z/200, 4, 2, 0.5)
	det := g.detail.FBm(x/45, z/45, 3, 2, 0.5)

	land := math.Max(0, cont)           // 0 at the coast → 1 deep inland
	base := float64(SeaLevel) + cont*30 // ocean basins + a modest land rise

	// Mountainousness varies region to region (low erosion = mountainous), so
	// terrain isn't uniformly gentle: flat plains here, jagged ranges there.
	m := clamp01(0.5 - 0.95*ero)
	m *= m

	// Ridged peak noise: inverting |noise| puts sharp ridgelines near 1.
	ridge := clamp01(1 - 2.2*math.Abs(g.peaks.FBm(x/300, z/300, 4, 2, 0.5)))
	ridge *= ridge

	mountains := m * land * (ridge*170 + 15) // tall, sharp peaks in mountainous inland
	rolling := hill * (5 + 22*m)             // bigger hills in mountainous regions
	h := base + mountains + rolling + det*3.5
	return clampInt(int(h), 5, 250)
}

// stretch widens a compressed fBm value (~±0.4) toward [-1, 1].
func stretch(n float64) float64 {
	n *= 2.4
	if n > 1 {
		return 1
	}
	if n < -1 {
		return -1
	}
	return n
}

// climate returns temperature and humidity. Temperature drops with altitude
// (a lapse rate), so the same latitude is colder up a mountain.
func (g *Generator) climate(wx, wz, h int) (temp, humid float64) {
	x, z := float64(wx), float64(wz)
	t := g.temp.FBm(x/600, z/600, 3, 2, 0.5)
	hm := g.humid.FBm(x/600, z/600, 3, 2, 0.5)
	if g.earth != nil {
		// Real terrain: lapse in REAL metres at a realistic rate. The noise
		// world's 1/70-blocks rate is vscale× too strong per real metre under
		// vertical compression — it buried Signal Hill (292 m) in snow. Tuned
		// so a median column freezes only above ~2,800 m real (mid-latitude
		// snowline): Cape Town stays temperate, Table Mountain merely cools,
		// a Drakensberg or Alps region still earns snowy summits.
		t -= float64(maxInt(0, h-SeaLevel)) * g.earth.vscale / 9000
		return t, hm
	}
	t -= float64(maxInt(0, h-SeaLevel)) / 70
	return t, hm
}

// riverDepth returns how many blocks below sea level a river channel cuts at a
// column (0 = no river). Rivers are a LOWLAND feature: they only touch terrain
// already near sea level, and always carve to a shallow FIXED channel depth —
// never proportional to the surrounding height (that gouged 50-block abysses
// through hills). The narrow eligible band keeps rivers to flat, low ground so
// they read as gentle valleys, not canyons.
func (g *Generator) riverDepth(wx, wz, baseH int) int {
	if baseH < SeaLevel-1 || baseH > SeaLevel+3 {
		return 0 // only near-sea-level land; hills never get a river cut through them
	}
	const halfWidth = 0.035
	r := math.Abs(g.river.FBm(float64(wx)/500, float64(wz)/500, 3, 2, 0.5))
	if r >= halfWidth {
		return 0
	}
	f := 1 - r/halfWidth  // 0 at the bank → 1 at the channel centre
	return 2 + int(f*2.5) // channel bottom 2..4 blocks below sea level (shallow)
}

// column is the resolved surface description for one world column: its height
// and the biome that lays its surface blocks.
type column struct {
	h     int
	biome *Biome
}

func (g *Generator) columnAt(wx, wz int) column {
	return column{h: g.Height(wx, wz), biome: g.resolveBiome(wx, wz)}
}

// top/sub read the biome's surface blocks (badlands bands its terracotta).
func (c column) topBlock() uint32 { return c.biome.Top }
func (c column) subBlock(y int) uint32 {
	if c.biome.Sub == Terracotta { // badlands: coloured terracotta banding by height
		return badlandsBand(y)
	}
	return c.biome.Sub
}

// badlandsBand returns the terracotta colour for a badlands sub-block at world
// height y — the classic banded mesa stripes.
func badlandsBand(y int) uint32 {
	switch ((y % 16) + 16) % 16 {
	case 0, 1, 2, 3:
		return OrangeTerracotta
	case 4:
		return YellowTerracotta
	case 5, 6:
		return Terracotta
	case 7, 8, 9:
		return BrownTerracotta
	case 10, 11:
		return RedTerracotta
	case 12:
		return LightGrayTerracotta
	default:
		return Terracotta
	}
}

// block returns the block state at world height y for this column.
func (c column) block(y int) uint32 {
	switch {
	case y == MinY:
		return Bedrock
	case y < c.h:
		switch d := c.h - 1 - y; {
		case d == 0:
			return c.topBlock()
		case d <= 3:
			return c.subBlock(y)
		case y < 0:
			return Deepslate
		default:
			return Stone
		}
	case y <= SeaLevel-1: // fill oceans, rivers and lakes up to sea level
		return Water
	default:
		return Air
	}
}

// BiomeName returns the biome identifier at a world column (e.g. "minecraft:plains").
func (g *Generator) BiomeName(wx, wz int) string {
	if g.nether {
		return g.netherBiome(wx, wz)
	}
	if g.end {
		return g.endBiome(wx, wz)
	}
	return g.resolveBiome(wx, wz).Name
}

// supportSurface fills the single air block directly beneath each column's
// surface crust. Near-surface caves can carve the blocks just under the top
// crust while the carve taper protects the crust itself, leaving a grass/dirt
// block hovering over a small void — the "random floating blocks" artifact.
// Filling one sub-material block under the surface plants it on solid ground
// (a deep cave just gains a one-block-thicker ceiling, which reads as normal
// terrain from above). Runs before decoration so trees root on solid ground.
func (g *Generator) supportSurface(ch *Chunk, cx, cz int32) {
	get := func(lx, y, lz int) uint32 {
		s := (y - MinY) / 16
		return ch.Sections[s][((y-MinY)%16*16+lz)*16+lx]
	}
	set := func(lx, y, lz int, b uint32) {
		s := (y - MinY) / 16
		ch.Sections[s][((y-MinY)%16*16+lz)*16+lx] = b
	}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			// The surface is at or below col.h-1; skip any carved air to find it.
			y := col.h - 1
			for y > MinY+1 && get(lx, y, lz) == Air {
				y--
			}
			// Undercut only if the block directly beneath the surface is air
			// (a fluid below means shoreline/seabed — leave it).
			if get(lx, y-1, lz) == Air {
				set(lx, y-1, lz, col.subBlock(y-1))
			}
		}
	}
}

// removeFloatingFragments deletes any solid block not connected (through solid
// blocks) to the deep ground or a chunk edge. Thin surface terrain — mountain
// spires, river banks — can be severed from below by a 3D cave, leaving a
// disconnected cap hanging in the air (the "floating blocks" artifact). A
// flood fill from the always-solid bottom sections and the chunk's side walls
// marks everything genuinely attached; the unreached remainder is carved away.
//
// Trees and flora ride along: a normal tree connects trunk→ground and is kept;
// a tree sitting on a severed fragment is removed with it. Edge blocks are
// seeded as connected, so terrain that attaches through a neighbour chunk (an
// arch, an overhang) is never wrongly deleted — false negatives (a rare border
// floater) are preferred to eating real terrain.
func removeFloatingFragments(ch *Chunk) {
	const seedTopY = 40 // blocks above MinY that are always solid ground
	height := len(ch.Sections) * 16
	vol := height * 16 * 16
	solid := func(b uint32) bool { return b != Air && b != Water && b != Lava }
	get := func(lx, dy, lz int) uint32 { // dy = y - MinY
		return ch.Sections[dy/16][(dy%16*16+lz)*16+lx]
	}
	idx := func(lx, dy, lz int) int32 { return int32((dy*16+lz)*16 + lx) }

	visited := make([]bool, vol)
	stack := make([]int32, 0, 4096)
	push := func(lx, dy, lz int) {
		if lx < 0 || lx > 15 || lz < 0 || lz > 15 || dy < 0 || dy >= height {
			return
		}
		i := idx(lx, dy, lz)
		if visited[i] || !solid(get(lx, dy, lz)) {
			return
		}
		visited[i] = true
		stack = append(stack, i)
	}
	// Seed: the deep ground (always solid) and every solid block on the 4 side
	// walls (assumed attached to the neighbouring chunk's terrain).
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for dy := 0; dy < seedTopY; dy++ {
				push(lx, dy, lz)
			}
		}
	}
	for dy := 0; dy < height; dy++ {
		for e := 0; e < 16; e++ {
			push(0, dy, e)
			push(15, dy, e)
			push(e, dy, 0)
			push(e, dy, 15)
		}
	}
	// Flood through solid 6-neighbours.
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		lx := int(i) & 15
		lz := int(i>>4) & 15
		dy := int(i >> 8)
		push(lx+1, dy, lz)
		push(lx-1, dy, lz)
		push(lx, dy, lz+1)
		push(lx, dy, lz-1)
		push(lx, dy+1, lz)
		push(lx, dy-1, lz)
	}
	// Carve away every solid block the flood never reached.
	for dy := 0; dy < height; dy++ {
		for lz := 0; lz < 16; lz++ {
			for lx := 0; lx < 16; lx++ {
				if b := get(lx, dy, lz); solid(b) && !visited[idx(lx, dy, lz)] {
					ch.Sections[dy/16][(dy%16*16+lz)*16+lx] = Air
				}
			}
		}
	}
}

// GenerateChunk produces all block states and a per-section biome for a chunk.
func (g *Generator) GenerateChunk(cx, cz int32) *Chunk {
	if g.nether {
		return g.generateNetherChunk(cx, cz)
	}
	if g.end {
		return g.generateEndChunk(cx, cz)
	}
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			for s := 0; s < g.sections; s++ {
				for ly := 0; ly < 16; ly++ {
					wy := MinY + s*16 + ly
					ch.Sections[s][(ly*16+lz)*16+lx] = g.carve(col.block(wy), wx, wy, wz, col.h)
				}
			}
		}
	}
	g.supportSurface(ch, cx, cz) // fill undercut surface crusts (no floating dirt/grass)
	g.placeOres(ch, cx, cz)      // after carving: veins only in surviving stone
	g.placeGeodes(ch, cx, cz)    // amethyst geodes (may straddle chunk borders)
	g.decorate(ch, cx, cz)
	g.stampStructures(ch, cx, cz) // lakes/dungeons/mineshafts/ruins overwrite
	removeFloatingFragments(ch)   // delete terrain a cave severed from the ground

	// One biome per section, sampled at the section's centre column. Sections
	// well below the surface take an underground biome (dripstone/lush/deep_dark)
	// instead of the surface biome, so caves tint correctly.
	cxw, czw := int(cx)*16+8, int(cz)*16+8
	surface := g.resolveBiome(cxw, czw)
	surfaceH := g.Height(cxw, czw)
	for s := 0; s < g.sections; s++ {
		cy := MinY + s*16 + 8
		if cy < surfaceH-24 && cy < SeaLevel {
			ch.Biomes[s] = g.caveBiome(cxw, czw, cy)
		} else {
			ch.Biomes[s] = surface.Name
		}
	}

	ch.computeHeightmap()
	return ch
}

// MaxHeight returns the tallest non-air column in the chunk (world Y), or MinY-1
// if the chunk is entirely air.
func (ch *Chunk) MaxHeight() int {
	m := int16(MinY - 1)
	for _, h := range ch.Heightmap {
		if h > m {
			m = h
		}
	}
	return int(m)
}

// SurfaceY is a safe spawn height for a column: the land surface, or the sea
// surface over water.
func (g *Generator) SurfaceY(wx, wz int) float64 {
	if g.nether {
		return float64(g.NetherFloor(wx, wz))
	}
	if g.end {
		return float64(EndSurfaceY + 2)
	}
	return float64(maxInt(g.Height(wx, wz), SeaLevel))
}

// BlockAt returns the generated block state at a single world coordinate
// (terrain + caves, but not feature decoration).
func (g *Generator) BlockAt(x, y, z int) uint32 {
	if y < MinY || y >= MinY+g.sections*16 {
		return Air
	}
	if g.nether {
		return g.netherBlock(x, y, z)
	}
	if g.end {
		return g.endBlock(x, y, z)
	}
	col := g.columnAt(x, z)
	return g.carve(col.block(y), x, y, z, col.h)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
