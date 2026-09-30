package worldgen

// Water and lava springs — the single falling source that seeps out of a
// stone wall and runs down it. Vanilla places twenty-five water springs a
// chunk anywhere from the world floor to y=192, and twenty lava springs
// biased hard toward the bottom, which is why lava is a cave hazard and
// water is where a cave farm's supply comes from.

// springStone is SpringFeature's valid_blocks: what a spring may sit in.
func springStone(s uint32, lava bool) bool {
	switch s {
	case Stone, Deepslate, Dirt, Calcite, stoneTuff, stoneGranite, stoneDiorite, stoneAndesite:
		return true
	}
	if lava {
		return false
	}
	return s == SnowBlock || s == PowderSnow || s == PackedIce
}

// frozenSpringStone is spring_lava_frozen's valid_blocks: the snow and ice
// of the frozen peaks, which the ordinary lava spring never sits in.
func frozenSpringStone(s uint32) bool {
	return s == SnowBlock || s == PowderSnow || s == PackedIce
}

// frozenSpringSalt seeds spring_lava_frozen's own draws, so the chunks
// without it decorate exactly as they did before it came.
const frozenSpringSalt = 0xF2021A7A

// overworldSprings places one chunk's springs. A spring wants its own
// valid stone above and below, and of the five cells around it — the four
// sides and the floor — exactly four stone and exactly one open, so it
// appears as water or lava coming out of a cave wall rather than hanging
// in the air. The fluid is a SOURCE block: vanilla's configuration names
// the source fluid, which is why a spring keeps running instead of drying
// up on its first tick.
//
// Every placed feature ends in vanilla's biome filter, asked at the
// spring's own cell: none in the deep dark (a cave biome, which a chunk's
// surface biome never is), and spring_lava_frozen — lava out of the snow
// and packed ice — only in the frozen peaks, groves, jagged peaks and
// snowy slopes, where it runs twenty draws a chunk alongside the ordinary
// two.
func (g *Generator) overworldSprings(r TreeRNG, reg *owRegion, bg *buildGuard, ox, oz int) {
	top := MinY + len(reg.ch.Sections)*16
	for i := 0; i < 25; i++ { // spring_water ×25, uniform from the floor to 192
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		g.springAt(reg, x, MinY+r.Intn(192-MinY+1), z, Water, func(s uint32) bool { return springStone(s, false) }, noSprings, nil)
	}
	for i := 0; i < 20; i++ { // spring_lava ×20, very biased to the bottom
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		g.springAt(reg, x, veryBiasedToBottom(r, MinY, top-8, 8), z, Lava, func(s uint32) bool { return springStone(s, true) }, noSprings, nil)
	}
	// spring_lava_frozen ×20, the same height, in its own biomes. It is new
	// in explored land, so it goes through the build guard.
	if !reg.chunkHasSurfaceBiome(ox, oz, frozenSprings) {
		return
	}
	rf := newTreeRNG(g.seed^frozenSpringSalt, ox, oz)
	for i := 0; i < 20; i++ {
		x, z := ox+rf.Intn(16), oz+rf.Intn(16)
		g.frozenSpringAt(reg, bg, x, veryBiasedToBottom(rf, MinY, top-8, 8), z)
	}
}

// frozenSpringAt places one spring_lava_frozen draw: snow and ice for its
// stone, its biomes at the cell, and the build guard.
func (g *Generator) frozenSpringAt(reg *owRegion, bg *buildGuard, x, y, z int) {
	g.springAt(reg, x, y, z, Lava, frozenSpringStone, nil, func(x, y, z int) bool {
		return frozenSprings[reg.caveBiomeAt(x, y, z)] && !bg.springBlocked(x, y, z)
	})
}

// chunkHasSurfaceBiome reports whether any of a chunk's corner or centre
// columns has one of the biomes — a cheap gate for a feature whose own
// biome filter is asked cell by cell afterwards.
func (reg *owRegion) chunkHasSurfaceBiome(ox, oz int, biomes map[string]bool) bool {
	for _, d := range [5][2]int{{0, 0}, {15, 0}, {0, 15}, {15, 15}, {8, 8}} {
		if biomes[reg.col(ox+d[0], oz+d[1]).biome.Name] {
			return true
		}
	}
	return false
}

// veryBiasedToBottom is vanilla's height provider of that name: a first
// roll picks a band above the floor, a second lands inside it, so most
// draws sit close to the bottom of the range.
func veryBiasedToBottom(r TreeRNG, min, max, inner int) int {
	if max <= min+inner {
		return min
	}
	hi := r.Intn(max-min-inner+1) + inner
	return min + r.Intn(r.Intn(hi)+1)
}

// springAt places one spring if the cell qualifies: valid is the
// feature's valid_blocks, excluded the biomes its filter leaves out (by
// the biome at the cell), and allow (nil: any) a last say — the frozen
// spring's biome and build guard.
func (g *Generator) springAt(reg *owRegion, x, y, z int, fluid uint32, valid func(uint32) bool,
	excluded map[string]bool, allow func(x, y, z int) bool) {
	if y <= MinY+1 || y >= MinY+len(reg.ch.Sections)*16-1 {
		return
	}
	if lx, lz := x-reg.baseX, z-reg.baseZ; lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
		return // another chunk's cell: its own pass places it
	}
	if !springQualifies(reg.read, x, y, z, valid) {
		return
	}
	if excluded != nil && excluded[reg.caveBiomeAt(x, y, z)] {
		return
	}
	if allow != nil && !allow(x, y, z) {
		return
	}
	reg.set(x, y, z, fluid)
}

// springQualifies is SpringFeature's test: valid stone above and below, a
// cell that is air or stone itself, and of the four sides plus the floor
// exactly four stone and exactly one opening for the fluid to run out of.
func springQualifies(read func(x, y, z int) uint32, x, y, z int, valid func(uint32) bool) bool {
	if !valid(read(x, y+1, z)) || !valid(read(x, y-1, z)) {
		return false
	}
	if here := read(x, y, z); here != Air && !valid(here) {
		return false
	}
	rock, holes := 0, 0
	for _, d := range [5][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
		switch s := read(x+d[0], y+d[1], z+d[2]); {
		case valid(s):
			rock++
		case s == Air:
			holes++
		}
	}
	return rock == 4 && holes == 1
}

// springBlocked is the build guard for a spring: none within four columns
// of a player's build from sixteen below it to two above (lava runs down
// and out), and none beside a cell a player dug or built on.
func (bg *buildGuard) springBlocked(x, y, z int) bool {
	if bg == nil || bg.cols == nil {
		return false
	}
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for _, by := range bg.cols[[2]int{x + dx, z + dz}] {
				if by >= y-16 && by <= y+2 {
					return true
				}
			}
		}
	}
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if bg.dug[[3]int{x + dx, y + dy, z + dz}] {
					return true
				}
			}
		}
	}
	return false
}
