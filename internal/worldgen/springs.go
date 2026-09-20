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

// overworldSprings places one chunk's springs. A spring wants its own
// valid stone above and below, and of the five cells around it — the four
// sides and the floor — exactly four stone and exactly one open, so it
// appears as water or lava coming out of a cave wall rather than hanging
// in the air. The fluid is a SOURCE block: vanilla's configuration names
// the source fluid, which is why a spring keeps running instead of drying
// up on its first tick.
func (g *Generator) overworldSprings(r TreeRNG, reg *owRegion, ox, oz int) {
	if noSprings[reg.col(ox+8, oz+8).biome.Name] {
		return
	}
	top := MinY + len(reg.ch.Sections)*16
	for i := 0; i < 25; i++ { // spring_water ×25, uniform from the floor to 192
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		g.springAt(reg, x, MinY+r.Intn(192-MinY+1), z, Water, false)
	}
	for i := 0; i < 20; i++ { // spring_lava ×20, very biased to the bottom
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		g.springAt(reg, x, veryBiasedToBottom(r, MinY, top-8, 8), z, Lava, true)
	}
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

// springAt places one spring if the cell qualifies.
func (g *Generator) springAt(reg *owRegion, x, y, z int, fluid uint32, lava bool) {
	if y <= MinY+1 || y >= MinY+len(reg.ch.Sections)*16-1 {
		return
	}
	if springQualifies(reg.read, x, y, z, lava) {
		reg.set(x, y, z, fluid)
	}
}

// springQualifies is SpringFeature's test: valid stone above and below, a
// cell that is air or stone itself, and of the four sides plus the floor
// exactly four stone and exactly one opening for the fluid to run out of.
func springQualifies(read func(x, y, z int) uint32, x, y, z int, lava bool) bool {
	if !springStone(read(x, y+1, z), lava) || !springStone(read(x, y-1, z), lava) {
		return false
	}
	if here := read(x, y, z); here != Air && !springStone(here, lava) {
		return false
	}
	rock, holes := 0, 0
	for _, d := range [5][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
		switch s := read(x+d[0], y+d[1], z+d[2]); {
		case springStone(s, lava):
			rock++
		case s == Air:
			holes++
		}
	}
	return rock == 4 && holes == 1
}
