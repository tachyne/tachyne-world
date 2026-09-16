package worldgen

// Nether decoration — the forests' vegetation. Per chunk, as vanilla places
// them: in a crimson forest, ten weeping-vine roofs anywhere in the height
// (WeepingVinesFeature), huge fungi eight per layer and the forest's roots
// and fungi six per layer (CountOnEveryLayerPlacement, which drops from the
// roof to the layer-th floor); in a warped forest, huge fungi eight per
// layer, vegetation five, nether sprouts four, and ten twisting-vine
// patches (TwistingVinesFeature 8,4,8). A neighbouring chunk's origins are
// replayed so a hat or a vine patch crossing the border is whole on both
// sides; out-of-chunk cells are read from the pure column function.

const netherDecorMargin = 12

var (
	CrimsonRoots   = blockBase("crimson_roots")
	WarpedRoots    = blockBase("warped_roots")
	NetherSprouts  = blockBase("nether_sprouts")
	netherFarmland = blockBase("farmland")
)

// netherRegion is a chunk's buffer plus lazily computed pure columns for
// its margin.
type netherRegion struct {
	g            *Generator
	ch           *Chunk
	baseX, baseZ int
	cols         map[[2]int][]uint32
}

func (r *netherRegion) read(x, y, z int) uint32 {
	if y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return Air
	}
	lx, lz := x-r.baseX, z-r.baseZ
	if lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		return sectionBlockAt(r.ch, lx, y, lz)
	}
	k := [2]int{x, z}
	col, ok := r.cols[k]
	if !ok {
		col = r.g.netherColumn(x, z)
		r.cols[k] = col
	}
	return col[y-MinY]
}

func (r *netherRegion) set(x, y, z int, s uint32) {
	lx, lz := x-r.baseX, z-r.baseZ
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return
	}
	setSectionBlock(r.ch, lx, y, lz, s, true)
}

func (r *netherRegion) driver() FungusDriver {
	return FungusDriver{Read: r.read, Set: r.set, InWorld: func(y int) bool { return y >= MinY && y < NetherCeiling }}
}

// netherPlantMayPlaceOn: nylium, soul soil, mycelium, dirt, farmland.
func netherPlantMayPlaceOn(s uint32) bool {
	return s == CrimsonNylium || s == WarpedNylium || s == SoulSoil || IsDirtTag(s) || s == netherFarmland
}

// decorateNether stamps the forests' features into a chunk.
func (g *Generator) decorateNether(ch *Chunk, cx, cz int32) {
	reg := &netherRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int][]uint32{}}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.netherChunkFeatures(reg, cx+dcx, cz+dcz)
		}
	}
}

// netherChunkFeatures replays the feature draws of chunk (ncx, ncz) —
// deterministic from the seed and the chunk — writing whatever lands in
// the region's own chunk.
func (g *Generator) netherChunkFeatures(reg *netherRegion, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^0x4E7E, ox, oz)
	d := reg.driver()
	fullRangeY := func() int { return MinY + 5 + r.Intn(NetherCeiling-MinY-10) }
	// WEEPING_VINES ×10, full range, crimson forest
	for i := 0; i < 10; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := fullRangeY()
		if g.netherBiome(x, z) == "minecraft:crimson_forest" {
			g.weepingVinesFeature(r, x, y, z, d)
		}
	}
	// CRIMSON_FUNGI 8 per layer
	g.onEveryLayer(r, reg, ox, oz, 8, func(x, y, z int) {
		if g.netherBiome(x, z) == "minecraft:crimson_forest" {
			PlaceHugeFungus(r, x, y, z, false, false, d)
		}
	})
	// CRIMSON_FOREST_VEGETATION 6 per layer
	g.onEveryLayer(r, reg, ox, oz, 6, func(x, y, z int) {
		if g.netherBiome(x, z) == "minecraft:crimson_forest" {
			g.netherForestVegetation(r, x, y, z, d, []uint32{CrimsonRoots, CrimsonFungus, WarpedFungus}, []int{87, 11, 1}, 8, 4)
		}
	})
	// WARPED_FUNGI 8 per layer
	g.onEveryLayer(r, reg, ox, oz, 8, func(x, y, z int) {
		if g.netherBiome(x, z) == "minecraft:warped_forest" {
			PlaceHugeFungus(r, x, y, z, true, false, d)
		}
	})
	// WARPED_FOREST_VEGETATION 5 per layer, NETHER_SPROUTS 4 per layer
	g.onEveryLayer(r, reg, ox, oz, 5, func(x, y, z int) {
		if g.netherBiome(x, z) == "minecraft:warped_forest" {
			g.netherForestVegetation(r, x, y, z, d, []uint32{WarpedRoots, CrimsonRoots, WarpedFungus, CrimsonFungus}, []int{85, 1, 13, 1}, 8, 4)
		}
	})
	g.onEveryLayer(r, reg, ox, oz, 4, func(x, y, z int) {
		if g.netherBiome(x, z) == "minecraft:warped_forest" {
			g.netherForestVegetation(r, x, y, z, d, []uint32{NetherSprouts}, []int{1}, 8, 4)
		}
	})
	// TWISTING_VINES ×10, full range, warped forest
	for i := 0; i < 10; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := fullRangeY()
		if g.netherBiome(x, z) == "minecraft:warped_forest" {
			g.twistingVinesFeature(r, x, y, z, 8, 4, 8, d)
		}
	}
}

// onEveryLayer is CountOnEveryLayerPlacement: layer by layer from the roof,
// count random columns each, until a layer yields nothing.
func (g *Generator) onEveryLayer(r TreeRNG, reg *netherRegion, ox, oz, count int, place func(x, y, z int)) {
	empty := func(s uint32) bool { return s == Air || s == Water || s == Lava }
	for layer := 0; ; layer++ {
		found := false
		for i := 0; i < count; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			// findOnGroundYPosition from the roof
			cur := reg.read(x, NetherCeiling, z)
			y := 1 << 20
			seen := 0
			for py := NetherCeiling; py >= MinY+1; py-- {
				below := reg.read(x, py-1, z)
				if !empty(below) && empty(cur) && below != Bedrock {
					if seen == layer {
						y = py
						break
					}
					seen++
				}
				cur = below
			}
			if y != 1<<20 {
				place(x, y, z)
				found = true
			}
		}
		if !found || layer > 16 {
			return
		}
	}
}

// netherForestVegetation is NetherForestVegetationFeature.place: width²
// tries about the origin (which must sit on nylium), each a weighted plant
// on empty ground it can hold.
func (g *Generator) netherForestVegetation(r TreeRNG, x, y, z int, d FungusDriver, states []uint32, weights []int, width, height int) {
	if b := d.Read(x, y-1, z); b != CrimsonNylium && b != WarpedNylium {
		return
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	for i := 0; i < width*width; i++ {
		px := x + r.Intn(width) - r.Intn(width)
		py := y + r.Intn(height) - r.Intn(height)
		pz := z + r.Intn(width) - r.Intn(width)
		pick := r.Intn(total)
		s := states[len(states)-1]
		for j, w := range weights {
			if pick < w {
				s = states[j]
				break
			}
			pick -= w
		}
		if d.Read(px, py, pz) == Air && py > MinY && netherPlantMayPlaceOn(d.Read(px, py-1, pz)) {
			d.Set(px, py, pz, s)
		}
	}
}

// weepingVinesFeature is WeepingVinesFeature.place: an air origin under
// netherrack or wart becomes a wart block, a wart patch grows about it
// (200 tries, one wart neighbour exactly), and vines hang from the roof
// about it (100 tries, one to eight long, doubled one time in six, one
// long one time in five, aged 17–25).
func (g *Generator) weepingVinesFeature(r TreeRNG, x, y, z int, d FungusDriver) {
	roof := func(s uint32) bool { return s == Netherrack || s == NetherWartBlock }
	if d.Read(x, y, z) != Air || !roof(d.Read(x, y+1, z)) {
		return
	}
	d.Set(x, y, z, NetherWartBlock)
	for i := 0; i < 200; i++ {
		px, py, pz := x+r.Intn(6)-r.Intn(6), y+r.Intn(2)-r.Intn(5), z+r.Intn(6)-r.Intn(6)
		if d.Read(px, py, pz) != Air {
			continue
		}
		n := 0
		for _, o := range [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if roof(d.Read(px+o[0], py+o[1], pz+o[2])) {
				n++
				if n > 1 {
					break
				}
			}
		}
		if n == 1 {
			d.Set(px, py, pz, NetherWartBlock)
		}
	}
	for i := 0; i < 100; i++ {
		px, py, pz := x+r.Intn(8)-r.Intn(8), y+r.Intn(2)-r.Intn(7), z+r.Intn(8)-r.Intn(8)
		if d.Read(px, py, pz) != Air || !roof(d.Read(px, py+1, pz)) {
			continue
		}
		n := 1 + r.Intn(8)
		if r.Intn(6) == 0 {
			n *= 2
		}
		if r.Intn(5) == 0 {
			n = 1
		}
		WeepingVinesColumn(r, px, py, pz, n, 17, 25, d)
	}
}

// twistingVinesFeature is TwistingVinesFeature.place (width, height,
// maxHeight): width² tries about the origin, each dropped to the first air
// above ground and, over netherrack, warped nylium or warped wart, a column
// one to maxHeight tall (doubled one time in six, one tall one time in
// five), aged 17–25.
func (g *Generator) twistingVinesFeature(r TreeRNG, x, y, z, width, height, maxHeight int, d FungusDriver) {
	invalid := func(px, py, pz int) bool {
		if d.Read(px, py, pz) != Air {
			return true
		}
		b := d.Read(px, py-1, pz)
		return b != Netherrack && b != WarpedNylium && b != WarpedWartBlock
	}
	if invalid(x, y, z) {
		return
	}
	for i := 0; i < width*width; i++ {
		px := x + r.Intn(2*width+1) - width
		py := y + r.Intn(2*height+1) - height
		pz := z + r.Intn(2*width+1) - width
		for d.Read(px, py, pz) == Air && py > MinY {
			py--
		}
		py++
		if py <= MinY || invalid(px, py, pz) {
			continue
		}
		n := 1 + r.Intn(maxHeight)
		if r.Intn(6) == 0 {
			n *= 2
		}
		if r.Intn(5) == 0 {
			n = 1
		}
		TwistingVinesColumn(r, px, py, pz, n, 17, 25, d)
	}
}
