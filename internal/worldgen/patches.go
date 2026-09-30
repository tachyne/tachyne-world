package worldgen

// The overworld patch features the generator never placed: wild sugar cane
// on the shorelines, pumpkins in the grass, melons in the jungles, and the
// water and lava springs that seep out of stone. Their absence was felt
// well beyond the scenery — with no wild cane there is no paper, so no
// books and no bookshelves, and with no wild pumpkins there is no carved
// pumpkin and so no snow or iron golem without a village to raid.

var (
	sugarCane = SugarCane
	pumpkin   = blockBase("pumpkin")
	melon     = blockBase("melon")
)

// Biomes vanilla leaves these out of. Everywhere else in the overworld has
// them, which is why the exclusions are listed rather than the inclusions.
// The desert, the badlands and the swamp are out of the plain cane patch
// because each has its own (biomePatches); the dappled forest and the
// sulfur caves are 26.3's, and have neither cane nor pumpkins.
var (
	noSugarCane = biomeSet("cherry_grove", "deep_dark", "dripstone_caves", "frozen_peaks",
		"grove", "jagged_peaks", "lush_caves", "mangrove_swamp", "meadow", "snowy_slopes", "stony_peaks",
		"desert", "badlands", "eroded_badlands", "wooded_badlands", "swamp", "dappled_forest", "sulfur_caves")
	noPumpkin = biomeSet("cherry_grove", "frozen_peaks", "jagged_peaks", "lush_caves",
		"mangrove_swamp", "meadow", "mushroom_fields", "stony_peaks", "dappled_forest", "sulfur_caves")
	melonBiomes   = biomeSet("jungle", "bamboo_jungle")
	noSprings     = biomeSet("deep_dark")
	frozenSprings = biomeSet("frozen_peaks", "grove", "jagged_peaks", "snowy_slopes")

	// The single-biome patches: patch_sugar_cane_desert (every chunk),
	// _badlands (one in five), _swamp (one in three) and patch_melon_sparse
	// (one in sixty-four).
	caneDesert   = biomeSet("desert")
	caneBadlands = biomeSet("badlands", "eroded_badlands", "wooded_badlands")
	caneSwamp    = biomeSet("swamp")
	melonSparse  = biomeSet("sparse_jungle")
)

// The biome gates the plain cane and pumpkin patches drew their rarity
// roll under before GenVersion 26, by the chunk's centre. They still decide
// whether the rolls are drawn — the cave features after them share the
// stream, and a chunk's lichen, mushrooms and dripstone must not move
// because its cane changed — while vanilla's lists above, asked at the
// patch's origin, decide whether anything is placed.
var (
	caneDrawGate = biomeSet("cherry_grove", "deep_dark", "dripstone_caves", "frozen_peaks",
		"grove", "jagged_peaks", "lush_caves", "mangrove_swamp", "meadow", "snowy_slopes", "stony_peaks")
	pumpkinDrawGate = biomeSet("cherry_grove", "frozen_peaks", "jagged_peaks", "lush_caves",
		"mangrove_swamp", "meadow", "mushroom_fields", "stony_peaks")
)

// biomeSet builds a lookup of fully-qualified biome names.
func biomeSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m["minecraft:"+n] = true
	}
	return m
}

// overworldPatches places the three surface patches for one chunk.
// Vanilla runs each as a rarity filter over the chunk, then a random patch
// of tries about the origin: sugar cane one chunk in six (20 tries, spread
// 4), melons the same in the jungles (64 tries, spread 7 and 3 vertical),
// pumpkins one chunk in three hundred (96 tries, the same spread).
func (g *Generator) overworldPatches(r TreeRNG, reg *owRegion, ox, oz int) {
	biome := reg.col(ox+8, oz+8).biome.Name
	origin := func() (int, int) { return ox + r.Intn(16), oz + r.Intn(16) }
	if !caneDrawGate[biome] && r.Intn(6) == 0 {
		x, z := origin()
		g.canePatch(r, reg, x, z, !noSugarCane[reg.col(x, z).biome.Name])
	}
	if melonBiomes[biome] && r.Intn(6) == 0 {
		x, z := origin()
		g.blockPatch(r, reg, x, z, 64, melon, false, true)
	}
	if !pumpkinDrawGate[biome] && r.Intn(300) == 0 {
		x, z := origin()
		g.blockPatch(r, reg, x, z, 96, pumpkin, true, !noPumpkin[reg.col(x, z).biome.Name])
	}
}

// canePatch is patch_sugar_cane: twenty tries spread four blocks either
// way, each a column of two to four canes on sand or dirt whose cell is
// air and which has water beside the block it stands on. With place false
// it draws exactly as it would have and places nothing (a biome vanilla
// leaves out, under a gate that still draws).
func (g *Generator) canePatch(r TreeRNG, reg *owRegion, ox, oz int, place bool) {
	var ghost map[[3]int]bool // the canes a dry run would have set
	read := func(x, y, z int) uint32 {
		if ghost[[3]int{x, y, z}] {
			return sugarCane
		}
		return reg.read(x, y, z)
	}
	for i := 0; i < 20; i++ {
		x, z := ox+triangleOffset(r, 4), oz+triangleOffset(r, 4)
		y := reg.col(x, z).h
		if read(x, y, z) != Air || !caneGround(read(x, y-1, z)) {
			continue
		}
		if !waterBeside(reg, x, y-1, z) {
			continue
		}
		// biased_to_bottom over 2..4: two canes are likeliest.
		n := 2 + r.Intn(2+r.Intn(2)+1)
		if n > 4 {
			n = 4
		}
		for c := 0; c < n; c++ {
			if read(x, y+c, z) != Air {
				break
			}
			if place {
				reg.set(x, y+c, z, sugarCane)
			} else {
				if ghost == nil {
					ghost = map[[3]int]bool{}
				}
				ghost[[3]int{x, y + c, z}] = true
			}
		}
	}
}

// biomePatchSalt seeds the single-biome patches' own draws.
const biomePatchSalt = 0xCA9E5A1D

// biomePatches replays chunk (ncx, ncz)'s single-biome cane and melon
// patches into the region's chunk, from their own stream so no other
// feature's draws move. Each is vanilla's placement: rarity, in_square,
// the MOTION_BLOCKING heightmap (which counts water), the biome at that
// origin, then its tries. Every cell goes through the build guard.
func (g *Generator) biomePatches(reg *owRegion, bg *buildGuard, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^biomePatchSalt, ox, oz)
	origin := func(biomes map[string]bool) (x, y, z int, ok bool) {
		x, z = ox+r.Intn(16), oz+r.Intn(16)
		c := reg.col(x, z)
		y = max(c.h, SeaLevel)
		return x, y, z, biomes[c.biome.Name]
	}
	cane := func(chance int, biomes map[string]bool) {
		if chance > 1 && r.Intn(chance) != 0 {
			return
		}
		// An origin out of the biome ends the patch: the stream is this
		// feature's own, and whether the origin holds is the same answer
		// in every chunk pass.
		if x, y, z, ok := origin(biomes); ok {
			g.caneColumns(r, reg, bg, x, y, z)
		}
	}
	cane(1, caneDesert)
	cane(5, caneBadlands)
	cane(3, caneSwamp)
	// patch_melon_sparse: the jungle's melon patch, one chunk in sixty-four.
	if r.Intn(64) != 0 {
		return
	}
	if x, y, z, ok := origin(melonSparse); ok {
		for i := 0; i < 64; i++ {
			px, py, pz := x+triangleOffset(r, 7), y+triangleOffset(r, 3), z+triangleOffset(r, 7)
			if !reg.inChunk(px, pz) {
				continue
			}
			// replaceable, no fluid, grass under it — and a plant it
			// replaces must not be a double plant's lower half, whose top
			// would be left hanging (vanilla's updates break it; nothing
			// here would).
			here := reg.read(px, py, pz)
			if here != Air && (!IsReplaceable(here) || reg.read(px, py+1, pz) != Air) {
				continue
			}
			if IsFluid(here) || reg.read(px, py-1, pz) != GrassBlock || bg.decorationBlocked(px, py, pz) {
				continue
			}
			reg.set(px, py, pz, melon)
		}
	}
}

// caneColumns is the sugar_cane feature's tail as the 26.x patches place
// it: twenty tries at a triangle offset of four about the origin, at the
// origin's height, each a column of biased_to_bottom(2, 4) canes up
// through air, where the ground holds cane and has water beside it. Every
// try's draws are made whether or not it holds, so the stream never hangs
// on what a chunk pass can see.
func (g *Generator) caneColumns(r TreeRNG, reg *owRegion, bg *buildGuard, x, y, z int) {
	for i := 0; i < 20; i++ {
		px, pz := x+triangleOffset(r, 4), z+triangleOffset(r, 4)
		n := 2 + r.Intn(r.Intn(3)+1)
		if !reg.inChunk(px, pz) {
			continue
		}
		if reg.read(px, y, pz) != Air || !caneGround(reg.read(px, y-1, pz)) || !waterBeside(reg, px, y-1, pz) ||
			bg.decorationBlocked(px, y, pz) {
			continue
		}
		for c := 0; c < n && reg.read(px, y+c, pz) == Air; c++ {
			reg.set(px, y+c, pz, sugarCane)
		}
	}
}

// inChunk reports whether a column is the region's own chunk's.
func (reg *owRegion) inChunk(x, z int) bool {
	lx, lz := x-reg.baseX, z-reg.baseZ
	return lx >= 0 && lx < 16 && lz >= 0 && lz < 16
}

// caneGround is SugarCaneBlock's floor: dirt, grass, podzol, mud or sand.
func caneGround(s uint32) bool {
	switch s {
	case GrassBlock, Dirt, CoarseDirt, Podzol, Mud, RootedDirt:
		return true
	}
	return s == Sand || s == RedSand
}

// waterBeside reports water in any of the four cells around a block — the
// any_of predicate the cane patch ends with.
func waterBeside(reg *owRegion, x, y, z int) bool {
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if IsWater(reg.read(x+d[0], y, z+d[1])) {
			return true
		}
	}
	return false
}

// blockPatch is the pumpkin and melon random_patch: tries attempts about
// the origin, spread seven horizontally and three vertically, each placing
// a single block in an empty cell that sits on grass. With place false it
// draws the same and places nothing (its draws do not depend on its reads).
func (g *Generator) blockPatch(r TreeRNG, reg *owRegion, ox, oz int, tries int, block uint32, onGrassOnly, place bool) {
	y0 := reg.col(ox, oz).h
	for i := 0; i < tries; i++ {
		x := ox + triangleOffset(r, 7)
		z := oz + triangleOffset(r, 7)
		y := y0 + triangleOffset(r, 3)
		here := reg.read(x, y, z)
		if here != Air && !(!onGrassOnly && IsReplaceable(here)) {
			continue
		}
		if !place || IsFluid(here) || reg.read(x, y-1, z) != GrassBlock {
			continue
		}
		reg.set(x, y, z, block)
	}
}

// The stone variants, as blocks and as the ore blobs that place them.
var (
	stoneGranite  = blockBase("granite")
	stoneDiorite  = blockBase("diorite")
	stoneAndesite = blockBase("andesite")
	stoneTuff     = blockBase("tuff")
)
