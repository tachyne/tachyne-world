package worldgen

// Feature decoration stamps trees and ground cover onto a generated chunk.
// Placement is a pure function of world coordinates, so neighbouring chunks
// agree on a tree whose canopy straddles their shared border — each stamps the
// part that lands inside it, with no shared state.

const treeMargin = 2 // max horizontal reach of a canopy

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

// treeStyle maps a treeKind to its trunk/leaf blocks and canopy shape.
func treeStyle(k treeKind) (log, leaves uint32, conical bool, minH, extraH int) {
	switch k {
	case treeSpruce:
		return SpruceLog, SpruceLeaves, true, 6, 4
	case treeBirch:
		return BirchLog, BirchLeaves, false, 5, 3
	case treeJungle:
		return JungleLog, JungleLeaves, false, 7, 8
	case treeAcacia:
		return AcaciaLog, AcaciaLeaves, false, 5, 2
	case treeDarkOak:
		return DarkOakLog, DarkOakLeaves, false, 6, 2
	case treeCherry:
		return CherryLog, CherryLeaves, false, 5, 3
	case treeMangrove:
		return MangroveLog, MangroveLeaves, false, 5, 3
	default:
		return OakLog, OakLeaves, false, 4, 3
	}
}

// stampTree writes a species tree (trunk + canopy) rooted at (wx,wz), clipped
// to this chunk. Leaves only replace air.
func (g *Generator) stampTree(ch *Chunk, baseX, baseZ, wx, wz, surfaceH int, kind treeKind) {
	lx0, lz0 := wx-baseX, wz-baseZ
	log, leaves, conical, minH, extraH := treeStyle(kind)
	height := minH + int(hash01(g.seed, wx, wz, 0x1111)*float64(extraH+1))
	trunkTop := surfaceH + height

	for y := surfaceH; y < trunkTop; y++ {
		setSectionBlock(ch, lx0, y, lz0, log, true)
	}

	leaf := func(y, radius int, trimCorners bool) {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if trimCorners && absInt(dx) == radius && absInt(dz) == radius {
					continue
				}
				setSectionBlock(ch, lx0+dx, y, lz0+dz, leaves, false)
			}
		}
	}
	if conical { // spruce: stacked rings tapering to a point
		for i, y := 0, trunkTop-4; y <= trunkTop; y++ {
			r := 2 - i/2
			if r < 0 {
				r = 0
			}
			leaf(y, r, r == 2)
			i++
		}
		setSectionBlock(ch, lx0, trunkTop+1, lz0, leaves, false)
		return
	}
	// Rounded canopy (oak/birch/jungle/dark oak/cherry/acacia/mangrove).
	leaf(trunkTop-2, 2, true)
	leaf(trunkTop-1, 2, true)
	leaf(trunkTop, 1, false)
	leaf(trunkTop+1, 1, true)
}

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
