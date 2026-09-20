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
var (
	noSugarCane = biomeSet("cherry_grove", "deep_dark", "dripstone_caves", "frozen_peaks",
		"grove", "jagged_peaks", "lush_caves", "mangrove_swamp", "meadow", "snowy_slopes", "stony_peaks")
	noPumpkin = biomeSet("cherry_grove", "frozen_peaks", "jagged_peaks", "lush_caves",
		"mangrove_swamp", "meadow", "mushroom_fields", "stony_peaks")
	melonBiomes   = biomeSet("jungle", "bamboo_jungle")
	noSprings     = biomeSet("deep_dark")
	frozenSprings = biomeSet("frozen_peaks", "grove", "jagged_peaks", "snowy_slopes")
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
	if !noSugarCane[biome] && r.Intn(6) == 0 {
		x, z := origin()
		g.canePatch(r, reg, x, z)
	}
	if melonBiomes[biome] && r.Intn(6) == 0 {
		x, z := origin()
		g.blockPatch(r, reg, x, z, 64, melon, false)
	}
	if !noPumpkin[biome] && r.Intn(300) == 0 {
		x, z := origin()
		g.blockPatch(r, reg, x, z, 96, pumpkin, true)
	}
}

// canePatch is patch_sugar_cane: twenty tries spread four blocks either
// way, each a column of two to four canes on sand or dirt whose cell is
// air and which has water beside the block it stands on.
func (g *Generator) canePatch(r TreeRNG, reg *owRegion, ox, oz int) {
	for i := 0; i < 20; i++ {
		x, z := ox+triangleOffset(r, 4), oz+triangleOffset(r, 4)
		y := reg.col(x, z).h
		if reg.read(x, y, z) != Air || !caneGround(reg.read(x, y-1, z)) {
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
			if reg.read(x, y+c, z) != Air {
				break
			}
			reg.set(x, y+c, z, sugarCane)
		}
	}
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
// a single block in an empty cell that sits on grass.
func (g *Generator) blockPatch(r TreeRNG, reg *owRegion, ox, oz int, tries int, block uint32, onGrassOnly bool) {
	y0 := reg.col(ox, oz).h
	for i := 0; i < tries; i++ {
		x := ox + triangleOffset(r, 7)
		z := oz + triangleOffset(r, 7)
		y := y0 + triangleOffset(r, 3)
		here := reg.read(x, y, z)
		if here != Air && !(!onGrassOnly && IsReplaceable(here)) {
			continue
		}
		if IsFluid(here) || reg.read(x, y-1, z) != GrassBlock {
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
