package worldgen

// Cave decoration — the lush caves' vegetation and every cave's glow
// lichen, per vanilla's placements. In a lush cave: cave vines hang from
// the ceilings (CAVE_VINES ×188, BlockColumn down from a sturdy face),
// moss patches lie on the floors (×125, VegetationPatch: moss with azaleas,
// carpet and grass) and cling to the ceilings (×125, with vines in the
// moss), clay patches and pools with dripleaves (×62), spore blossoms
// (×25), a rooted azalea tree or two (RootSystem), and vines on the walls
// (×256). Everywhere at least thirteen blocks under the ground, glow
// lichen (×104–157, MultifaceGrowth). Each feature is drawn per chunk and
// replayed for the neighbours, so patches crossing a border are whole.

var (
	SporeBlossom    = blockBase("spore_blossom")
	RootedDirt      = blockBase("rooted_dirt")
	HangingRoots    = blockID("hanging_roots")
	GlowLichenBlk   = blockID("glow_lichen")
	MossCarpet      = blockBase("moss_carpet")
	caveVinesLo     = func() uint32 { lo, _ := BlockRange("cave_vines"); return lo }()
	caveVinesHi     = func() uint32 { _, hi := BlockRange("cave_vines"); return hi }()
	caveVinesBody   = blockID("cave_vines_plant")
	stonesForLichen = func() map[uint32]bool {
		m := map[uint32]bool{}
		for _, n := range []string{"stone", "andesite", "diorite", "granite", "dripstone_block", "calcite", "tuff", "deepslate"} {
			lo, hi := BlockRange(n)
			for s := lo; s <= hi; s++ {
				m[s] = true
			}
		}
		return m
	}()
)

// caveVinesHead is a cave_vines head at age (berries or not): the states
// run age-major, berries=true first.
func caveVinesHead(age int, berries bool) uint32 {
	s := caveVinesLo + uint32(age)*2
	if !berries {
		s++
	}
	return s
}

func caveVinesBodyState(berries bool) uint32 {
	if berries {
		return caveVinesBody - 1 // berries=true is the first of the pair
	}
	return caveVinesBody
}

// caveBiomeAt is the biome a cave feature sees at a cell: the underground
// biome well below the surface, the surface biome otherwise (the same rule
// the chunk's biome array follows).
func (g *Generator) caveBiomeAt(x, y, z int) string {
	if y < g.Height(x, z)-24 && y < SeaLevel {
		return g.caveBiome(x, z, y)
	}
	return g.resolveBiome(x, z).Name
}

// decorateCaves stamps the 3×3 chunks' cave features into this chunk.
func (g *Generator) decorateCaves(ch *Chunk, cx, cz int32) {
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			g.caveChunkFeatures(reg, cx+dcx, cz+dcz)
		}
	}
}

// scanFor is EnvironmentScanPlacement: from an air origin, step in dir up
// to max times looking for a cell that passes target; the origin and every
// step before it must be air.
func (reg *owRegion) scanFor(x, y, z, dy, max int, target func(s uint32) bool) (int, bool) {
	if reg.read(x, y, z) != Air {
		return 0, false
	}
	for i := 0; i < max; i++ {
		if target(reg.read(x, y, z)) {
			return y, true
		}
		y += dy
		if y < MinY || y >= MinY+len(reg.ch.Sections)*16 {
			return 0, false
		}
		if reg.read(x, y, z) != Air {
			break
		}
	}
	if target(reg.read(x, y, z)) {
		return y, true
	}
	return 0, false
}

func solid(s uint32) bool { return s != Air && !IsFluid(s) && Collides(s) }

// caveChunkFeatures replays chunk (ncx, ncz)'s cave feature draws.
func (g *Generator) caveChunkFeatures(reg *owRegion, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^0xCA7E, ox, oz)
	rangeY := func() int { return MinY + r.Intn(256-MinY+1) } // RANGE_BOTTOM_TO_MAX_TERRAIN_HEIGHT
	lushChunk := reg.chunkHasCaveBiome(ox, oz, "minecraft:lush_caves")
	lush := func(x, y, z int) bool { return lushChunk && reg.caveBiomeAt(x, y, z) == "minecraft:lush_caves" }
	// CAVE_VINES ×188: under a sturdy ceiling, a column of vines down.
	for i := 0; lushChunk && i < 188; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, rangeY(), z, +1, 12, solid); ok && lush(x, cy-1, z) {
			g.caveVineColumn(r, reg, x, cy-1, z, [][3]int{{0, 19, 2}, {0, 2, 3}, {0, 6, 10}})
		}
	}
	// LUSH_CAVES_VEGETATION ×125: a moss patch on the floor.
	for i := 0; lushChunk && i < 125; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if fy, ok := reg.scanFor(x, rangeY(), z, -1, 12, solid); ok && lush(x, fy+1, z) {
			g.vegetationPatch(r, reg, x, fy+1, z, patchCfg{replaceable: mossReplaceable, ground: MossBlock, floor: true, depth: 1, extraBottom: 0, vertical: 5, vegetation: 0.8, radiusLo: 4, radiusHi: 7, edge: 0.3, plant: g.mossVegetation})
		}
	}
	// LUSH_CAVES_CLAY ×62: clay with dripleaves, half of them pools.
	for i := 0; lushChunk && i < 62; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if fy, ok := reg.scanFor(x, rangeY(), z, -1, 12, solid); ok && lush(x, fy+1, z) {
			if r.Intn(2) == 0 {
				g.vegetationPatch(r, reg, x, fy+1, z, patchCfg{replaceable: lushGroundReplaceable, ground: Clay, floor: true, depth: 3, extraBottom: 0.8, vertical: 2, vegetation: 0.05, radiusLo: 4, radiusHi: 7, edge: 0.7, plant: g.dripleafPlant})
			} else {
				g.vegetationPatch(r, reg, x, fy+1, z, patchCfg{replaceable: lushGroundReplaceable, ground: Clay, floor: true, depth: 3, extraBottom: 0.8, vertical: 5, vegetation: 0.1, radiusLo: 4, radiusHi: 7, edge: 0.7, plant: g.dripleafPlant, pool: true})
			}
		}
	}
	// LUSH_CAVES_CEILING_VEGETATION ×125: moss on the ceiling with vines in it.
	for i := 0; lushChunk && i < 125; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, rangeY(), z, +1, 12, solid); ok && lush(x, cy-1, z) {
			g.vegetationPatch(r, reg, x, cy-1, z, patchCfg{replaceable: mossReplaceable, ground: MossBlock, floor: false, depth: 1 + r.Intn(2), extraBottom: 0, vertical: 5, vegetation: 0.08, radiusLo: 4, radiusHi: 7, edge: 0.3,
				plant: func(r TreeRNG, reg *owRegion, x, y, z int) {
					g.caveVineColumn(r, reg, x, y, z, [][3]int{{0, 3, 5}, {1, 7, 1}}) // CAVE_VINE_IN_MOSS
				}})
		}
	}
	// SPORE_BLOSSOM ×25: under a sturdy ceiling.
	for i := 0; lushChunk && i < 25; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, rangeY(), z, +1, 12, solid); ok && lush(x, cy-1, z) && reg.read(x, cy-1, z) == Air {
			reg.set(x, cy-1, z, SporeBlossom)
		}
	}
	// ROOTED_AZALEA_TREE ×1–2: from under a ceiling, up through the rock to
	// a cavity with room for the tree, rooted dirt below it, hanging roots.
	for i, n := 0, 1+r.Intn(2); lushChunk && i < n; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, rangeY(), z, +1, 12, solid); ok && lush(x, cy-1, z) {
			g.rootSystem(r, reg, x, cy-1, z)
		}
	}
	// CLASSIC_VINES ×256: a vine on the first wall it can hold to.
	for i := 0; lushChunk && i < 256; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if !lush(x, y, z) || reg.read(x, y, z) != Air {
			continue
		}
		for _, f := range faceDirs6 {
			if f.d[1] < 0 {
				continue // never from below
			}
			if holdsFace(reg.read(x+f.d[0], y+f.d[1], z+f.d[2])) {
				reg.set(x, y, z, withProps("vine", f.prop, "true"))
				break
			}
		}
	}
	// Surface mushrooms in the dark, and magma in the underwater caves.
	g.overworldMushrooms(r, reg, ox, oz)
	g.underwaterMagma(r, reg, ox, oz)
	// The dripstone caves' clusters, large dripstone and pointed dripstone.
	g.dripstoneFeatures(r, reg, ox, oz)
	// GLOW_LICHEN ×104–157, at least thirteen blocks under the ground.
	for i, n := 0, 104+r.Intn(54); i < n; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if y > reg.col(x, z).h-13 {
			continue
		}
		g.lichenGrowth(r, reg, x, y, z)
	}
}

// faceDirs6 is every direction with its face-property name.
var faceDirs6 = []struct {
	prop string
	d    [3]int
}{
	{"down", [3]int{0, -1, 0}}, {"up", [3]int{0, 1, 0}},
	{"north", [3]int{0, 0, -1}}, {"south", [3]int{0, 0, 1}},
	{"west", [3]int{-1, 0, 0}}, {"east", [3]int{1, 0, 0}},
}

func opposite(prop string) string {
	switch prop {
	case "down":
		return "up"
	case "up":
		return "down"
	case "north":
		return "south"
	case "south":
		return "north"
	case "west":
		return "east"
	}
	return "west"
}

// holdsFace is MultifaceBlock.canAttachTo: a full solid block.
func holdsFace(s uint32) bool { return solid(s) && !IsReplaceable(s) }

// caveVineColumn is the CAVE_VINE BlockColumn: a body of weighted length
// (cave_vines_plant, one in five with berries) and a head aged 23–25, down
// from (x,y,z) while the cells are air, the body giving way first.
func (g *Generator) caveVineColumn(r TreeRNG, reg *owRegion, x, y, z int, bodyLayers [][3]int) {
	total := 0
	for _, l := range bodyLayers {
		total += l[2]
	}
	pick := r.Intn(total)
	body := 0
	for _, l := range bodyLayers {
		if pick < l[2] {
			body = l[0] + r.Intn(l[1]-l[0]+1)
			break
		}
		pick -= l[2]
	}
	heights := [2]int{body, 1}
	room := 0
	for room < body+1 && reg.read(x, y-room, z) == Air && y-room > MinY {
		room++
	}
	if room == 0 {
		return
	}
	if cut := body + 1 - room; cut > 0 { // truncate, prioritising the tip
		take := cut
		if take > heights[0] {
			take = heights[0]
		}
		heights[0] -= take
		heights[1] -= cut - take
	}
	py := y
	for i := 0; i < heights[0]; i++ {
		reg.set(x, py, z, caveVinesBodyState(r.Intn(5) == 0))
		py--
	}
	if heights[1] > 0 {
		reg.set(x, py, z, caveVinesHead(23+r.Intn(3), r.Intn(5) == 0))
	}
}

// patchCfg is a VegetationPatchConfiguration.
type patchCfg struct {
	replaceable func(uint32) bool
	ground      uint32
	floor       bool // CaveSurface.FLOOR (else CEILING)
	depth       int
	extraBottom float64
	vertical    int
	vegetation  float64
	radiusLo    int
	radiusHi    int
	edge        float64
	plant       func(r TreeRNG, reg *owRegion, x, y, z int)
	pool        bool // WATERLOGGED_VEGETATION_PATCH: the surface fills with water
}

var lushGroundReplaceable = func(s uint32) bool {
	return mossReplaceable(s) || s == Clay || s == Gravel || s == Sand
}

// vegetationPatch is VegetationPatchFeature.place: an xz-radius patch of
// ground (corners out, edges by chance), each column found through the
// vertical range, the ground laid depth deep into the replaceable rock,
// and vegetation on a share of the surface.
func (g *Generator) vegetationPatch(r TreeRNG, reg *owRegion, x, y, z int, c patchCfg) {
	inward, outward := -1, 1 // FLOOR: the surface is below the origin
	if !c.floor {
		inward, outward = 1, -1
	}
	xr := c.radiusLo + r.Intn(c.radiusHi-c.radiusLo+1) + 1
	zr := c.radiusLo + r.Intn(c.radiusHi-c.radiusLo+1) + 1
	var surface [][3]int
	for dx := -xr; dx <= xr; dx++ {
		edgeX := dx == -xr || dx == xr
		for dz := -zr; dz <= zr; dz++ {
			edgeZ := dz == -zr || dz == zr
			corner := edgeX && edgeZ
			edge := (edgeX || edgeZ) && !corner
			if corner || (edge && (c.edge == 0 || r.Float64() > c.edge)) {
				continue
			}
			px, py, pz := x+dx, y, z+dz
			for i := 0; i < c.vertical && reg.read(px, py, pz) == Air; i++ {
				py += inward
			}
			for i := 0; i < c.vertical && reg.read(px, py, pz) != Air; i++ {
				py += outward
			}
			gy := py + inward
			ground := reg.read(px, gy, pz)
			if reg.read(px, py, pz) != Air || !solid(ground) {
				continue
			}
			depth := c.depth
			if c.extraBottom > 0 && r.Float64() < c.extraBottom {
				depth++
			}
			placed := false
			for i := 0; i < depth; i++ { // placeGround
				s := reg.read(px, gy, pz)
				if s == c.ground {
					gy += inward
					continue
				}
				if !c.replaceable(s) {
					placed = placed || i != 0
					break
				}
				reg.set(px, gy, pz, c.ground)
				placed = true
				gy += inward
			}
			if placed {
				surface = append(surface, [3]int{px, py - inward, pz})
			}
		}
	}
	if c.pool { // WaterloggedVegetationPatchFeature: the surface cells become water where enclosed
		for _, s := range surface {
			px, py, pz := s[0], s[1]+outward, s[2]
			enclosed := true
			for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				if n := reg.read(px+o[0], py, pz+o[1]); n == Air {
					enclosed = false
					break
				}
			}
			if enclosed {
				reg.set(px, py, pz, Water)
			}
		}
	}
	for _, s := range surface {
		if c.vegetation > 0 && r.Float64() < c.vegetation {
			c.plant(r, reg, s[0], s[1]+outward, s[2])
		}
	}
}

// mossVegetation is MOSS_VEGETATION: flowering azalea 4, azalea 7, moss
// carpet 25, short grass 50, tall grass 10, where it can stand.
func (g *Generator) mossVegetation(r TreeRNG, reg *owRegion, x, y, z int) {
	if reg.read(x, y, z) != Air {
		return
	}
	pick := r.Intn(96)
	switch {
	case pick < 4:
		reg.set(x, y, z, blockID("flowering_azalea"))
	case pick < 11:
		reg.set(x, y, z, blockID("azalea"))
	case pick < 36:
		reg.set(x, y, z, MossCarpet)
	case pick < 86:
		reg.set(x, y, z, blockBase("short_grass"))
	default:
		if reg.read(x, y+1, z) == Air {
			reg.set(x, y, z, withProps("tall_grass", "half", "lower"))
			reg.set(x, y+1, z, withProps("tall_grass", "half", "upper"))
		}
	}
}

// dripleafPlant is DRIPLEAF: one of a small dripleaf or a big one facing
// east, west, south or north (a stem 0–4 tall two times in three, the leaf
// on top), in water where the patch pooled.
func (g *Generator) dripleafPlant(r TreeRNG, reg *owRegion, x, y, z int) {
	wet := func(s uint32) string {
		if s == Water {
			return "true"
		}
		return "false"
	}
	at := reg.read(x, y, z)
	if at != Air && at != Water {
		return
	}
	switch r.Intn(5) {
	case 0: // small dripleaf: lower and upper halves
		if a := reg.read(x, y+1, z); a != Air && a != Water {
			return
		}
		facing := [4]string{"north", "south", "west", "east"}[r.Intn(4)]
		reg.set(x, y, z, withProps("small_dripleaf", "half", "lower", "facing", facing, "waterlogged", wet(at)))
		reg.set(x, y+1, z, withProps("small_dripleaf", "half", "upper", "facing", facing, "waterlogged", wet(reg.read(x, y+1, z))))
	default:
		facing := [4]string{"east", "west", "south", "north"}[r.Intn(4)]
		stem := 0
		if r.Intn(3) != 0 {
			stem = r.Intn(5)
		}
		py := y
		for i := 0; i < stem; i++ {
			s := reg.read(x, py, z)
			if s != Air && s != Water {
				return
			}
			reg.set(x, py, z, withProps("big_dripleaf_stem", "facing", facing, "waterlogged", wet(s)))
			py++
		}
		if s := reg.read(x, py, z); s == Air || s == Water {
			reg.set(x, py, z, withProps("big_dripleaf", "facing", facing, "waterlogged", wet(s)))
		}
	}
}

// rootSystem is RootSystemFeature with ROOTED_AZALEA_TREE's configuration:
// from an air cell, up to a hundred blocks up, the first cell open to the
// sky-side with three clear above it and solid ground under grows the
// azalea tree; the column below it takes rooted dirt (twenty tries per
// level within three), then hanging roots hang about the origin.
func (g *Generator) rootSystem(r TreeRNG, reg *owRegion, x, y, z int) {
	if reg.read(x, y, z) != Air {
		return
	}
	c, ok := TreeFeatures["azalea_tree"]
	if !ok {
		return
	}
	azaleaRootReplaceable := func(s uint32) bool {
		return inAnyRange(s, caveBaseStone) || IsDirtTag(s) || inAnyRange(s, caveTerracotta) || s == RedSand || s == Clay || s == Gravel || s == Sand || s == SnowBlock || s == PowderSnow
	}
	treeY := -1
	for i := 1; i <= 100; i++ {
		py := y + i
		if reg.col(x, z).h < py {
			return
		}
		s := reg.read(x, py, z)
		if s != Air && !IsReplaceable(s) {
			continue
		}
		space := true
		for k := 1; k <= 3; k++ {
			if reg.read(x, py+k, z) != Air {
				space = false
				break
			}
		}
		below := reg.read(x, py-1, z)
		if !space || below == Lava || !solid(below) {
			if !space {
				continue
			}
			return
		}
		if g.placeTreeInRegion(c, r, reg, x, py, z) {
			treeY = py
		}
		break
	}
	if treeY < 0 {
		return
	}
	for py := y; py < treeY; py++ { // rooted dirt
		for i := 0; i < 20; i++ {
			px, pz := x+r.Intn(3)-r.Intn(3), z+r.Intn(3)-r.Intn(3)
			if azaleaRootReplaceable(reg.read(px, py, pz)) {
				reg.set(px, py, pz, RootedDirt)
			}
		}
	}
	for i := 0; i < 20; i++ { // hanging roots
		px, py, pz := x+r.Intn(3)-r.Intn(3), y+r.Intn(2)-r.Intn(2), z+r.Intn(3)-r.Intn(3)
		if reg.read(px, py, pz) == Air && holdsFace(reg.read(px, py+1, pz)) {
			reg.set(px, py, pz, HangingRoots)
		}
	}
}

// placeTreeInRegion grows a tree feature through the region.
func (g *Generator) placeTreeInRegion(c *TreeConfig, r TreeRNG, reg *owRegion, x, y, z int) bool {
	set := func(px, py, pz int, st uint32, leaf bool) {
		if leaf {
			cur := reg.read(px, py, pz)
			if cur != Air && !IsLeaves(cur) && !IsReplaceable(cur) {
				return
			}
		}
		reg.set(px, py, pz, st)
	}
	free := func(px, py, pz int) bool {
		s := reg.read(px, py, pz)
		return s == Air || IsLeaves(s) || IsReplaceable(s) || IsLog(s)
	}
	return PlaceTree(c, x, y, z, r, TreeDriver{
		Set: set, Free: free, Read: reg.read,
		DirtGround:  func(px, py, pz int) bool { return IsDirtTag(reg.read(px, py, pz)) },
		SurfaceTop:  func(px, pz int) int { return g.Height(px, pz) },
		RootThrough: func(px, py, pz int) bool { return false },
	})
}

// lichenGrowth is MultifaceGrowthFeature for glow lichen: from an air or
// water cell, place on a stone face there, else search each direction up
// to twenty cells through air, water and lichen for a spot; one in two
// spreads a second face.
func (g *Generator) lichenGrowth(r TreeRNG, reg *owRegion, x, y, z int) {
	airOrWater := func(s uint32) bool { return s == Air || s == Water }
	isLichen := func(s uint32) bool { return s >= glowLichenLo && s <= glowLichenHi }
	if !airOrWater(reg.read(x, y, z)) {
		return
	}
	place := func(px, py, pz int, dirs []int) bool {
		old := reg.read(px, py, pz)
		for _, di := range dirs {
			f := faceDirs6[di]
			if di == 0 { // placing on the floor means facing down → not allowed
				continue
			}
			if !stonesForLichen[reg.read(px+f.d[0], py+f.d[1], pz+f.d[2])] {
				continue
			}
			base := GlowLichenBlk
			info, _ := InfoForState(base)
			switch {
			case isLichen(old):
				if GetProperty(info, old, f.prop) == "true" {
					return false
				}
				base = old
			case old == Water:
				base = SetProperty(info, base, "waterlogged", "true")
			}
			st := SetProperty(info, base, f.prop, "true")
			reg.set(px, py, pz, st)
			if r.Float64() < 0.5 { // spread one more face from this one
				g.lichenSpreadFrom(r, reg, px, py, pz, di)
			}
			return true
		}
		return false
	}
	dirs := perm6(r)
	if place(x, y, z, dirs) {
		return
	}
	for _, sd := range dirs {
		f := faceDirs6[sd]
		var pd []int
		for _, d := range perm6(r) {
			if d != sd^1 { // except the opposite of the search direction
				pd = append(pd, d)
			}
		}
		px, py, pz := x, y, z
		for i := 0; i < 20; i++ {
			px, py, pz = px+f.d[0], py+f.d[1], pz+f.d[2]
			s := reg.read(px, py, pz)
			if !airOrWater(s) && !isLichen(s) {
				break
			}
			if place(px, py, pz, pd) {
				return
			}
		}
	}
}

var glowLichenLo, glowLichenHi = BlockRange("glow_lichen")

// lichenSpreadFrom is MultifaceSpreader.spreadFromFaceTowardRandomDirection
// for a lichen holding face from: toward a random off-axis direction, the
// first of SAME_POSITION, SAME_PLANE and WRAP_AROUND that lands on air,
// water or lichen with a stone to hold it.
func (g *Generator) lichenSpreadFrom(r TreeRNG, reg *owRegion, x, y, z, from int) {
	isLichen := func(s uint32) bool { return s >= glowLichenLo && s <= glowLichenHi }
	info, _ := InfoForState(GlowLichenBlk)
	try := func(px, py, pz, face int) bool {
		s := reg.read(px, py, pz)
		if !(s == Air || s == Water || isLichen(s)) {
			return false
		}
		f := faceDirs6[face]
		if isLichen(s) && GetProperty(info, s, f.prop) == "true" {
			return false
		}
		if !holdsFace(reg.read(px+f.d[0], py+f.d[1], pz+f.d[2])) {
			return false
		}
		base := GlowLichenBlk
		if isLichen(s) {
			base = s
		} else if s == Water {
			base = SetProperty(info, base, "waterlogged", "true")
		}
		reg.set(px, py, pz, SetProperty(info, base, f.prop, "true"))
		return true
	}
	fd := faceDirs6[from].d
	for _, dir := range perm6(r) {
		if dir/2 == from/2 {
			continue
		}
		dd := faceDirs6[dir].d
		if try(x, y, z, dir) || try(x+dd[0], y+dd[1], z+dd[2], from) || try(x+dd[0]+fd[0], y+dd[1]+fd[1], z+dd[2]+fd[2], dir^1) {
			return
		}
	}
}

// perm6 is a shuffled 0..5 (Direction.allShuffled) from a TreeRNG.
func perm6(r TreeRNG) []int {
	p := []int{0, 1, 2, 3, 4, 5}
	for i := 5; i > 0; i-- {
		j := r.Intn(i + 1)
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// caveBaseStone is #base_stone_overworld; caveTerracotta the terracottas.
var (
	caveBaseStone  = rangesOf("stone", "granite", "diorite", "andesite", "tuff", "deepslate")
	caveTerracotta = rangesOf("terracotta", "white_terracotta", "orange_terracotta", "magenta_terracotta", "light_blue_terracotta",
		"yellow_terracotta", "lime_terracotta", "pink_terracotta", "gray_terracotta", "light_gray_terracotta", "cyan_terracotta",
		"purple_terracotta", "blue_terracotta", "brown_terracotta", "green_terracotta", "red_terracotta", "black_terracotta")
)

func rangesOf(names ...string) [][2]uint32 {
	out := make([][2]uint32, 0, len(names))
	for _, n := range names {
		lo, hi := BlockRange(n)
		out = append(out, [2]uint32{lo, hi})
	}
	return out
}

func inAnyRange(s uint32, rs [][2]uint32) bool {
	for _, r := range rs {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}
