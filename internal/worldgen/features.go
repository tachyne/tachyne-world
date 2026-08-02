package worldgen

// Feature decoration stamps trees and ground cover onto a generated chunk.
// Placement is a pure function of world coordinates, so neighbouring chunks
// agree on a tree whose canopy straddles their shared border — each stamps the
// part that lands inside it, with no shared state.

// treeMargin is how far outside a chunk a tree can be rooted and still put
// blocks inside it, so decoration scans that much extra each way. Measured
// across every feature over 200 seeds — a mega jungle's canopy reaches 8 — not
// estimated. It was 2, which was right for the radius-2 blobs trees used to be.
const treeMargin = 8

// decorate adds trees and ground cover to a freshly generated chunk, choosing
// the tree species and ground flora from each column's biome.
func (g *Generator) decorate(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	// Scan origins in the chunk plus a margin so trees rooted in neighbouring
	// columns still stamp the canopy blocks that fall inside this chunk.
	for ox := -treeMargin; ox < 16+treeMargin; ox++ {
		for oz := -treeMargin; oz < 16+treeMargin; oz++ {
			wx, wz := baseX+ox, baseZ+oz
			col := g.columnAt(wx, wz)
			b := col.biome
			if !plantable(col.topBlock()) || col.h < SeaLevel {
				continue // trees/flora only on solid, dry-ish ground
			}
			if g.carve(col.topBlock(), wx, col.h-1, wz, col.h) == Air {
				continue // a cave opening removed the surface here — nothing to root
			}
			if b.Tree != treeNone && g.treeAt(wx, wz, b.TreeDensity) {
				g.stampTree(ch, baseX, baseZ, wx, wz, col.h, b.Tree)
				continue
			}
			// Ground cover is a single block — only when its origin is in-chunk.
			if ox >= 0 && ox < 16 && oz >= 0 && oz < 16 {
				g.stampGroundCover(ch, ox, oz, col.h, wx, wz, b.Flora)
			}
		}
	}
}

// plantable reports whether a surface block can root a tree or ground cover.
func plantable(top uint32) bool {
	switch top {
	case GrassBlock, Podzol, Mycelium, Mud, RedSand:
		return true
	}
	return false
}

// TreeAt reports whether a tree trunk is rooted at a column, obstructing
// ground-level movement. Pure function of world coordinates, matching the
// placement test in decorate — used by mob pathing to walk around trees.
func (g *Generator) TreeAt(wx, wz int) bool {
	col := g.columnAt(wx, wz)
	if col.biome.Tree == treeNone || !plantable(col.topBlock()) || col.h < SeaLevel {
		return false
	}
	if g.carve(col.topBlock(), wx, col.h-1, wz, col.h) == Air {
		return false // a cave opening removed the surface — nothing rooted
	}
	return g.treeAt(wx, wz, col.biome.TreeDensity)
}

// treeAt decides whether a tree is rooted at a column. A low-frequency forest
// field modulates density, scaled by the biome's own tree density, so woods
// clump and clearings thin.
func (g *Generator) treeAt(wx, wz int, density float64) bool {
	d := 0.5 + 0.5*g.forest.FBm(float64(wx)/180, float64(wz)/180, 2, 2, 0.5) // [0,1]
	prob := (0.010 + 0.06*d*d) * density
	return hash01(g.seed, wx, wz, 0x7777) < prob
}

// treeFeatureFor maps a biome's tree kind to the vanilla FEATURE it grows.
// Where vanilla's biome pools mix species — plains scattering the occasional
// large oak among plain ones, taiga mixing pine with spruce — the mix is drawn
// from the tree's own position, so it is stable for a seed.
func treeFeatureFor(k treeKind, seed int64, wx, wz int) *TreeConfig {
	name := ""
	switch k {
	case treeOak:
		if hash01(seed, wx, wz, 0x71EE) < 0.1 {
			name = "fancy_oak"
		} else {
			name = "oak"
		}
	case treeBirch:
		name = "birch"
	case treeSpruce:
		// trees_taiga: pine_checked 1/3, default spruce.
		if hash01(seed, wx, wz, 0x71EF) < 0.33333334 {
			name = "pine"
		} else {
			name = "spruce"
		}
	case treeSpruceOld:
		// trees_old_growth_spruce_taiga: mega_spruce 1/3, then pine 1/3,
		// then the 0.0125 fallen-spruce slot, default spruce. Unported
		// entries still consume their slot — an EMPTY spot where vanilla
		// grows the fallen log — so every later entry's rate stays exact.
		switch {
		case hash01(seed, wx, wz, 0x71F0) < 0.33333334:
			name = "mega_spruce"
		case hash01(seed, wx, wz, 0x71F1) < 0.33333334:
			name = "pine"
		case hash01(seed, wx, wz, 0x71F8) < 0.0125:
			return nil // fallen spruce (unported)
		default:
			name = "spruce"
		}
	case treePineOld:
		// trees_old_growth_pine_taiga: mega_spruce 1/39, mega_pine 4/13,
		// pine 1/3, the fallen-spruce slot, default spruce.
		switch {
		case hash01(seed, wx, wz, 0x71F2) < 0.025641026:
			name = "mega_spruce"
		case hash01(seed, wx, wz, 0x71F3) < 0.30769232:
			name = "mega_pine"
		case hash01(seed, wx, wz, 0x71F4) < 0.33333334:
			name = "pine"
		case hash01(seed, wx, wz, 0x71F9) < 0.0125:
			return nil // fallen spruce (unported)
		default:
			name = "spruce"
		}
	case treeForest:
		// trees_birch_and_oak_leaf_litter: the fallen-birch slot, birch 0.2,
		// large oak 0.1, the fallen-oak slot, default oak — all the litter
		// variants (their bees twins fold onto them until beehives exist).
		switch {
		case hash01(seed, wx, wz, 0x71FA) < 0.0025:
			return nil // fallen birch (unported)
		case hash01(seed, wx, wz, 0x71FB) < 0.2:
			name = "birch_leaf_litter"
		case hash01(seed, wx, wz, 0x71FC) < 0.1:
			name = "fancy_oak_leaf_litter"
		case hash01(seed, wx, wz, 0x71FD) < 0.0125:
			return nil // fallen oak (unported)
		default:
			name = "oak_leaf_litter"
		}
	case treeDarkForest:
		// dark_forest_vegetation: the two huge-mushroom slots, dark oak
		// 2/3, the fallen-birch slot, birch 0.2, the fallen-oak slot, large
		// oak 0.1, default oak — litter variants throughout.
		switch {
		case hash01(seed, wx, wz, 0x71FE) < 0.025:
			return nil // huge brown mushroom — mushroomFor grows it here
		case hash01(seed, wx, wz, 0x71FF) < 0.05:
			return nil // huge red mushroom — mushroomFor grows it here
		case hash01(seed, wx, wz, 0x7200) < 0.6666667:
			name = "dark_oak_leaf_litter"
		case hash01(seed, wx, wz, 0x7201) < 0.0025:
			return nil // fallen birch (unported)
		case hash01(seed, wx, wz, 0x7202) < 0.2:
			name = "birch_leaf_litter"
		case hash01(seed, wx, wz, 0x7203) < 0.0125:
			return nil // fallen oak (unported)
		case hash01(seed, wx, wz, 0x7204) < 0.1:
			name = "fancy_oak_leaf_litter"
		default:
			name = "oak_leaf_litter"
		}
	case treeJungle:
		name = "jungle_tree"
	case treeAcacia:
		name = "acacia"
	case treeDarkOak:
		name = "dark_oak"
	case treePaleOak:
		// pale_garden_vegetation: one pale oak in ten runs the creaking-heart
		// decorator (and of those, only a tree whose trunk bend folds a log
		// pocket actually gets a heart — the decorator's own rule).
		if hash01(seed, wx, wz, 0x71F5) < 0.1 {
			name = "pale_oak_creaking"
		} else {
			name = "pale_oak"
		}
	case treeCherry:
		name = "cherry"
	case treeMangrove:
		name = "mangrove"
	case treeSwampOak:
		name = "swamp_oak"
	default:
		return nil
	}
	return TreeFeatures[name]
}

// stampTree grows one vanilla tree rooted at (wx,wz), clipped to this chunk.
//
// The shape is PlaceTree's — the real trunk and foliage placers — rather than
// the trunk-and-blob this used to draw.
//
// TWO THINGS MAKE THIS DIFFERENT FROM VANILLA'S DRIVER, both because a chunk
// generator cannot see the world:
//
// The RNG is derived from the tree's own POSITION rather than a stream, so a
// tree straddling a chunk border draws identically in every chunk that stamps
// it. Vanilla does not need this — it decorates with the whole neighbourhood
// available and places each tree once.
//
// And "is this cell free" is answered from the TERRAIN MODEL, not the chunk
// buffer. Asking the buffer gives a different answer either side of a chunk
// edge (cells outside it read as empty), so the same tree measures as fitting
// in one chunk and not in its neighbour, and the two passes disagree: one
// draws a canopy, the other never draws the trunk. The terrain height is the
// same number from anywhere, so both passes agree.
//
// Structures do not enter into it: decorate runs BEFORE stampStructures, and
// structures overwrite. The sapling grower, which CAN see the world, is the
// path where growing into a house matters, and it gets the real check.
func (g *Generator) stampTree(ch *Chunk, baseX, baseZ, wx, wz, surfaceH int, kind treeKind) {
	mush := mushroomFor(kind, g.seed, wx, wz)
	c := treeFeatureFor(kind, g.seed, wx, wz)
	if c == nil && mush == mushNone {
		return
	}
	rng := newTreeRNG(g.seed, wx, wz)
	set := func(x, y, z int, state uint32, leaf bool) {
		lx, lz := x-baseX, z-baseZ
		if leaf {
			// Leaves take air, and leaves take LEAVES: the distance-seeding
			// pass rewrites cells this same tree just filled, and a plain
			// only-into-air rule would silently drop every rewrite.
			cur := sectionBlockAt(ch, lx, y, lz)
			if cur != Air && !IsLeaves(cur) {
				return
			}
			setSectionBlock(ch, lx, y, lz, state, true)
			return
		}
		setSectionBlock(ch, lx, y, lz, state, true)
	}
	// Height is the first EMPTY cell above the ground — where the trunk
	// starts — so the tree's own base cell counts as free.
	free := func(x, y, z int) bool { return y >= g.Height(x, z) }
	// read answers real blocks inside this chunk's buffer. Outside it, a
	// coarse terrain guess is safe by construction: decorator reads gate only
	// writes to the SAME cell, out-of-chunk writes are clipped anyway, and no
	// randomness is drawn between a read and its write — the neighbour chunk
	// redraws the tree and answers its own cells from its own buffer.
	read := func(x, y, z int) uint32 {
		lx, lz := x-baseX, z-baseZ
		if lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
			return sectionBlockAt(ch, lx, y, lz)
		}
		if y >= g.Height(x, z) {
			return Air
		}
		return Stone
	}
	// The setDirtAt ground test answers from the TERRAIN MODEL, never the
	// chunk buffer: whether the dirt cell joins the trunk-position list must
	// be the same in every chunk pass that draws this tree. Air converts,
	// exactly as vanilla's rule has it.
	dirtGround := func(x, y, z int) bool {
		col := g.columnAt(x, z)
		switch {
		case y >= col.h:
			return false
		case y == col.h-1:
			return IsDirtTag(col.topBlock())
		default:
			return IsDirtTag(col.biome.Sub)
		}
	}
	drv := TreeDriver{
		Set: set, Free: free, Read: read, DirtGround: dirtGround,
		// The terrain's first empty cell IS the heightmap minus trees, and
		// PlaceTree folds its own logs in. Chunk-independent by construction.
		SurfaceTop: func(x, z int) int { return g.Height(x, z) },
		// Root passability from the model, like DirtGround: the walk's draws
		// follow its reads, so every pass must hear the same answers.
		RootThrough: func(x, y, z int) bool {
			col := g.columnAt(x, z)
			switch {
			case y >= col.h:
				return false
			case y == col.h-1:
				return IsRootGrowThrough(col.topBlock())
			default:
				return IsRootGrowThrough(col.biome.Sub)
			}
		},
	}
	if mush != mushNone {
		PlaceHugeMushroom(mush == mushBrown, wx, surfaceH, wz, rng, drv)
		return
	}
	PlaceTree(c, wx, surfaceH, wz, rng, drv)
}

// Huge-mushroom selection: which cascade slots grow one instead of a tree.
const (
	mushNone = iota
	mushBrown
	mushRed
)

// mushroomFor mirrors the huge-mushroom entries of the biome cascades — the
// SAME hash salts treeFeatureFor's slots return nil on, so a position grows
// exactly one thing — plus the mushroom fields' 50/50 pick
// (mushroom_island_vegetation's random boolean, red when true).
func mushroomFor(k treeKind, seed int64, wx, wz int) int {
	switch k {
	case treeDarkForest:
		if hash01(seed, wx, wz, 0x71FE) < 0.025 {
			return mushBrown
		}
		if hash01(seed, wx, wz, 0x71FF) < 0.05 {
			return mushRed
		}
	case treeMushroomFields:
		if hash01(seed, wx, wz, 0x7205) < 0.5 {
			return mushRed
		}
		return mushBrown
	}
	return mushNone
}

// newTreeRNG is the per-tree randomness, derived from the tree's own position
// so every chunk that overlaps it draws the same tree.
func newTreeRNG(seed int64, wx, wz int) TreeRNG {
	return &hashRNG{seed: uint64(seed) ^ uint64(wx)*0x9E3779B97F4A7C15 ^ uint64(wz)*0xC2B2AE3D27D4EB4F}
}

// hashRNG is a splitmix64, so a tree's draws are reproducible from its position
// without allocating a full math/rand source per tree.
type hashRNG struct{ seed uint64 }

func (r *hashRNG) next() uint64 {
	r.seed += 0x9E3779B97F4A7C15
	z := r.seed
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *hashRNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

func (r *hashRNG) Float64() float64 { return float64(r.next()>>11) / (1 << 53) }

// stampGroundCover scatters biome-appropriate flora on a column's surface.
func (g *Generator) stampGroundCover(ch *Chunk, lx, lz, surfaceH, wx, wz int, flora floraKind) {
	r := hash01(g.seed, wx, wz, 0x2222)
	put := func(b uint32) { setSectionBlock(ch, lx, surfaceH, lz, b, false) }
	pick := func(salt uint64, opts ...uint32) uint32 {
		return opts[int(hash01(g.seed, wx, wz, salt)*float64(len(opts)))%len(opts)]
	}
	switch flora {
	case floraNone:
		return
	case floraPlains:
		switch {
		case r < 0.18:
			put(ShortGrass)
		case r < 0.205:
			put(pick(0x3333, Dandelion, Poppy))
		}
	case floraFlower: // meadows / flower forests / cherry groves: dense flowers
		switch {
		case r < 0.15:
			put(ShortGrass)
		case r < 0.34:
			put(pick(0x3333, Dandelion, Poppy, Cornflower, AzureBluet, OxeyeDaisy, Allium))
		}
	case floraDesert:
		switch {
		case r < 0.006:
			g.stampColumn(ch, lx, lz, surfaceH, 1+int(hash01(g.seed, wx, wz, 0x44)*3), Cactus)
		case r < 0.012:
			put(DeadBush)
		}
	case floraBadlands:
		if r < 0.02 {
			put(DeadBush)
		}
	case floraTaiga:
		switch {
		case r < 0.22:
			put(pick(0x3333, Fern, ShortGrass))
		case r < 0.24:
			put(SweetBerryBush)
		}
	case floraJungle:
		switch {
		case r < 0.25:
			put(pick(0x3333, Fern, ShortGrass))
		case r < 0.27:
			g.stampColumn(ch, lx, lz, surfaceH, 2+int(hash01(g.seed, wx, wz, 0x44)*3), Bamboo)
		}
	case floraSwamp:
		switch {
		case r < 0.10:
			put(ShortGrass)
		case r < 0.11:
			put(BlueOrchid)
		}
	case floraSavanna:
		if r < 0.28 {
			put(ShortGrass)
		}
	case floraDarkForest:
		switch {
		case r < 0.12:
			put(ShortGrass)
		case r < 0.14:
			put(pick(0x3333, BrownMushroom, RedMushroom))
		}
	case floraPaleGarden:
		// The pale garden's floor, in the proportions the vanilla features
		// produce: a patchy pale-moss carpet, grass, and the eyeblossoms that
		// open at night (their day/night switch is already live).
		switch {
		case r < 0.14:
			put(PaleMossCarpet)
		case r < 0.22:
			put(ShortGrass)
		case r < 0.24:
			put(ClosedEyeblossom)
		}
	case floraMushroom:
		if r < 0.10 {
			put(pick(0x3333, RedMushroom, BrownMushroom))
		}
	case floraSnowy:
		if r < 0.06 {
			put(Fern)
		}
	}
}

// stampColumn stacks n blocks upward from the surface (cactus, bamboo).
func (g *Generator) stampColumn(ch *Chunk, lx, lz, surfaceH, n int, block uint32) {
	for i := 0; i < n; i++ {
		setSectionBlock(ch, lx, surfaceH+i, lz, block, false)
	}
}

// setSectionBlock writes a block at in-chunk (lx,y,lz) if it lies inside the
// chunk and the world height. With overwrite=false it only fills air.
func setSectionBlock(ch *Chunk, lx, y, lz int, state uint32, overwrite bool) {
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
		return
	}
	if y < MinY || y >= MinY+len(ch.Sections)*16 {
		return
	}
	sec := (y - MinY) / 16
	ly := (y - MinY) % 16
	i := (ly*16+lz)*16 + lx
	if !overwrite && ch.Sections[sec][i] != Air {
		return
	}
	ch.Sections[sec][i] = state
}

// sectionBlockAt reads a chunk cell, mirroring setSectionBlock's indexing.
// Out of range reads as air.
func sectionBlockAt(ch *Chunk, lx, y, lz int) uint32 {
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
		return Air
	}
	if y < MinY || y >= MinY+len(ch.Sections)*16 {
		return Air
	}
	sec := (y - MinY) / 16
	ly := (y - MinY) % 16
	return ch.Sections[sec][(ly*16+lz)*16+lx]
}

// hash01 maps (seed, x, z, salt) to a deterministic value in [0,1).
func hash01(seed int64, x, z int, salt uint64) float64 {
	h := uint64(seed) + salt
	h ^= uint64(int64(x)) * 0x9e3779b97f4a7c15
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h ^= uint64(int64(z)) * 0xc2b2ae3d27d4eb4f
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11) / float64(uint64(1)<<53)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
