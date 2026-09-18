package worldgen

// The outer End — EndBiomes' features. Every chunk carries its biome ring
// (the_end within the void, then highlands, midlands and barrens, and the
// small islands beyond); the highlands grow chorus forests (CHORUS_PLANT
// 0–4 per chunk on the terrain's top, ChorusFlowerBlock.generatePlant
// within eight blocks) and hide return gateways (END_GATEWAY_RETURN, one
// chunk in seven hundred, three to nine above the ground, which the
// server's gateway pass carries home); the small islands' biome floats
// end-stone islands (END_ISLAND_DECORATED, one chunk in fourteen and a
// second one in four, at y=55–70).

var (
	chorusPlantBase = blockBase("chorus_plant")
	chorusFlowerLo  = func() uint32 { lo, _ := BlockRange("chorus_flower"); return lo }()
	chorusFlowerHi  = func() uint32 { _, hi := BlockRange("chorus_flower"); return hi }()
	EndGateway      = blockBase("end_gateway")
)

func isChorusPlantState(s uint32) bool { return s >= chorusPlantBase && s <= chorusPlantBase+63 }
func isChorusFlowerState(s uint32) bool {
	return s >= chorusFlowerLo && s <= chorusFlowerHi
}

// endRegion is an End chunk buffer with pure-terrain reads beyond it.
type endRegion struct {
	g            *Generator
	ch           *Chunk
	baseX, baseZ int
	cols         map[[2]int][3]int // outer plate per column beyond the chunk: top, bottom, ok
}

func (r *endRegion) read(x, y, z int) uint32 {
	if y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return Air
	}
	lx, lz := x-r.baseX, z-r.baseZ
	if lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		return sectionBlockAt(r.ch, lx, y, lz)
	}
	if x*x+z*z <= EndIslandR*EndIslandR {
		return r.g.endBlockCol(x, y, z, 0, 0, false)
	}
	k := [2]int{x, z}
	c, ok := r.cols[k]
	if !ok {
		top, bottom, has := r.g.endOuterColumn(x, z)
		c = [3]int{top, bottom, 0}
		if has {
			c[2] = 1
		}
		if r.cols == nil {
			r.cols = map[[2]int][3]int{}
		}
		r.cols[k] = c
	}
	return r.g.endBlockCol(x, y, z, c[0], c[1], c[2] == 1)
}

func (r *endRegion) set(x, y, z int, s uint32) {
	lx, lz := x-r.baseX, z-r.baseZ
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return
	}
	setSectionBlock(r.ch, lx, y, lz, s, true)
}

// top is the HEIGHTMAP answer: the first air cell over the highest block.
func (r *endRegion) top(x, z int) (int, bool) {
	for y := EndSurfaceY + 48; y > MinY; y-- {
		if r.read(x, y, z) != Air {
			return y + 1, true
		}
	}
	return 0, false
}

// decorateEnd stamps the 3×3 chunks' End features into this chunk.
func (g *Generator) decorateEnd(ch *Chunk, cx, cz int32) {
	reg := &endRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.endChunkFeatures(reg, cx+dcx, cz+dcz)
		}
	}
}

func (g *Generator) endChunkFeatures(reg *endRegion, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	biome := g.endBiome(ox+8, oz+8)
	r := newTreeRNG(g.seed^0xE4D, ox, oz)
	switch biome {
	case "minecraft:end_highlands":
		if r.Intn(700) == 0 { // END_GATEWAY_RETURN
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if t, ok := reg.top(x, z); ok && g.endBiome(x, z) == biome {
				g.endGatewayFrame(reg, x, t+3+r.Intn(7), z)
			}
		}
		for i, n := 0, r.Intn(5); i < n; i++ { // CHORUS_PLANT
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if t, ok := reg.top(x, z); ok && g.endBiome(x, z) == biome && reg.read(x, t-1, z) == EndStone {
				g.chorusPlant(r, reg, x, t, z)
			}
		}
	case "minecraft:small_end_islands":
		if r.Intn(14) == 0 { // END_ISLAND_DECORATED: one, and one more in four
			n := 1
			if r.Float64() < 0.25 {
				n = 2
			}
			for i := 0; i < n; i++ {
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				y := 55 + r.Intn(16)
				if g.endBiome(x, z) == biome {
					g.endIsland(r, reg, x, y, z)
				}
			}
		}
	}
}

// endGatewayFrame is EndGatewayFeature: the gateway in its bedrock frame,
// a hollow middle layer to walk into.
func (g *Generator) endGatewayFrame(reg *endRegion, x, y, z int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				cx, cy, cz := dx == 0, dy == 0, dz == 0
				cap := dy == -2 || dy == 2
				var s uint32
				switch {
				case cx && cy && cz:
					s = EndGateway
				case cy:
					s = Air
				case cap && cx && cz:
					s = Bedrock
				case (cx || cz) && !cap:
					s = Bedrock
				default:
					s = Air
				}
				reg.set(x+dx, y+dy, z+dz, s)
			}
		}
	}
}

// endIsland is EndIslandFeature: a cone of end stone tapering down from a
// disc four to six wide.
func (g *Generator) endIsland(r TreeRNG, reg *endRegion, x, y, z int) {
	size := float64(4 + r.Intn(3))
	for dy := 0; size > 0.5; dy-- {
		lo, hi := int(-size), int(size+0.999)
		for dx := lo; dx <= hi; dx++ {
			for dz := lo; dz <= hi; dz++ {
				if float64(dx*dx+dz*dz) <= (size+1)*(size+1) {
					reg.set(x+dx, y+dy, z+dz, EndStone)
				}
			}
		}
		size -= float64(r.Intn(2)) + 0.5
	}
}

// chorusPlant is ChorusFlowerBlock.generatePlant: a trunk one to five
// tall, then up to four branches (at least one at the root) within the
// spread, each recursing to depth four, a flower on every tip.
func (g *Generator) chorusPlant(r TreeRNG, reg *endRegion, x, y, z int) {
	var placed [][3]int
	put := func(px, py, pz int) {
		reg.set(px, py, pz, chorusPlantBase)
		placed = append(placed, [3]int{px, py, pz})
	}
	allNeighboursEmpty := func(px, py, pz int, except [3]int) bool { // the four sides (Direction.Plane.HORIZONTAL)
		for _, o := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if o == except {
				continue
			}
			if reg.read(px+o[0], py+o[1], pz+o[2]) != Air {
				return false
			}
		}
		return true
	}
	var grow func(cx, cy, cz, depth int)
	grow = func(cx, cy, cz, depth int) {
		height := 1 + r.Intn(4)
		if depth == 0 {
			height++
		}
		for i := 0; i < height; i++ {
			ty := cy + i + 1
			if !allNeighboursEmpty(cx, ty, cz, [3]int{9, 9, 9}) {
				return
			}
			put(cx, ty, cz)
		}
		placedStem := false
		if depth < 4 {
			stems := r.Intn(4)
			if depth == 0 {
				stems++
			}
			for i := 0; i < stems; i++ {
				d := [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}[r.Intn(4)]
				tx, ty, tz := cx+d[0], cy+height, cz+d[2]
				if absInt(tx-x) < 8 && absInt(tz-z) < 8 && reg.read(tx, ty, tz) == Air && reg.read(tx, ty-1, tz) == Air &&
					allNeighboursEmpty(tx, ty, tz, [3]int{-d[0], -d[1], -d[2]}) {
					placedStem = true
					put(tx, ty, tz)
					grow(tx, ty, tz, depth+1)
				}
			}
		}
		if !placedStem {
			reg.set(cx, cy+height, cz, chorusFlowerLo+5) // a flower, fully grown
		}
	}
	put(x, y, z)
	grow(x, y, z, 0)
	// getStateWithConnections, once the whole plant stands
	info, ok := InfoForState(chorusPlantBase)
	if !ok {
		return
	}
	for _, p := range placed {
		if !isChorusPlantState(reg.read(p[0], p[1], p[2])) {
			continue // a tip that became the flower
		}
		s := chorusPlantBase
		for _, f := range faceDirs6 {
			n := reg.read(p[0]+f.d[0], p[1]+f.d[1], p[2]+f.d[2])
			joined := isChorusPlantState(n) || isChorusFlowerState(n) || (f.prop == "down" && n == EndStone)
			s = SetProperty(info, s, f.prop, boolStr(joined))
		}
		reg.set(p[0], p[1], p[2], s)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
