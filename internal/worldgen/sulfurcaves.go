package worldgen

// Sulfur caves — 26.3's cave biome. Vanilla's overworld parameter list
// places it as an underground biome (depth 0.2–0.9) at continentalness
// coast to inland (-0.19..0.55), erosion 0.45..1 (the flattest two bands)
// and weirdness -1.1..-0.85: the far negative tail. The engine's climate
// fields stand in for vanilla's — continent and erosion are the terrain's
// own (same sense: -1 ocean → 1 inland, low erosion mountainous), and the
// variety field is its weirdness — and a depth parameter of d is d×128
// blocks below the column's surface (vanilla's depth gradient runs 1.5 per
// 192 blocks), so the biome sits 26 to 115 blocks down.
//
// Inside it the rock is banded (the overworld material rules' underground
// SULFUR_CAVE_BANDS): where the 3D SULFUR_CAVE_GRADIENT noise reads
// -0.4..-0.1 the stone is cinnabar, 0..0.4 sulfur, above 0.4 cinnabar
// again, and the gaps stay stone (or deepslate). On that rock grow the
// biome's features, per its placed features:
//
//   - ROOTED_SULFUR_SPRING ×1–2 (LAKES): from under a ceiling, a root
//     system climbs to the first level, open patch above it (five clear
//     over, the ground level ±2 eight out each way), scatters tuff about
//     it (64–128 blocks) and stamps one of ten sulfur spring templates
//     seven below, roots of sulfur down the column behind it.
//   - SULFUR_POOL ×256 (LAKES): on a sulfur floor, a lake of water walled
//     in sulfur, with wet potent sulfur on the pool's bed.
//   - SULFUR_SPIKE_CLUSTER ×48–96: SpeleothemClusterFeature of sulfur
//     spikes (height 1–4, no pools).
//   - SULFUR_SPIKE ×192–256, one to five each about a spot: single spikes
//     from sulfur or cinnabar rock.

var (
	SulfurBlock     = blockBase("sulfur")
	CinnabarBlock   = blockBase("cinnabar")
	potentSulfurWet = withProps("potent_sulfur", "potent_sulfur_state", "wet")
	sulfurSpikeLo   = func() uint32 { lo, _ := BlockRange("sulfur_spike"); return lo }()
	sulfurSpikeHi   = func() uint32 { _, hi := BlockRange("sulfur_spike"); return hi }()

	// #features_cannot_replace, and with leaves and logs
	// #lava_pool_stone_cannot_replace.
	featuresCannotReplace = rangesOf("bedrock", "spawner", "chest", "end_portal_frame", "reinforced_deepslate", "trial_spawner", "vault")
)

const sulfurCaves = "minecraft:sulfur_caves"

// sulfurCfg is SULFUR_SPIKE_CLUSTER / SULFUR_SPIKE's configuration: sulfur
// spikes on sulfur, replacing #sulfur_spike_replaceable_blocks.
var sulfurCfg = &speleothemCfg{
	base:        SulfurBlock,
	pointed:     pointedStatesOf("sulfur_spike"),
	pLo:         sulfurSpikeLo,
	pHi:         sulfurSpikeHi,
	replaceable: func(s uint32) bool { return s == SulfurBlock || s == CinnabarBlock },
	height:      func(r TreeRNG) int { return 1 + r.Intn(4) }, // uniform 1–4
	wetness:     func(r TreeRNG) float64 { return 0 },         // a constant 0: no draw
}

// sulfurClimate is the sulfur caves' parameter box at a column: weirdness
// -1.1..-0.85, erosion 0.45..1, continentalness -0.19..0.55 (the checks run
// rarest first).
func (g *Generator) sulfurClimate(wx, wz int) bool {
	if g.nether || g.end {
		return false
	}
	x, z := float64(wx), float64(wz)
	if stretch(g.variety.FBm(x/220, z/220, 2, 2, 0.5)) > -0.85 {
		return false
	}
	if stretch(g.erosion.FBm(x/650, z/650, 3, 2, 0.5)) < 0.45 {
		return false
	}
	c := stretch(g.continent.FBm(x/1100, z/1100, 4, 2, 0.5))
	return c >= -0.19 && c <= 0.55
}

// sulfurDepth is the biome's depth band, 0.2..0.9: 26 to 115 blocks under
// the column's surface h.
func sulfurDepth(h, y int) bool {
	d := h - y
	return d >= 26 && d <= 115
}

// inSulfurCaves is caveBiomeIn(...) == sulfur_caves for a column in hand,
// skipping the cave-selector noise wherever deep_dark cannot claim the cell.
func (g *Generator) inSulfurCaves(c column, x, y, z int) bool {
	if !c.sulfur || y >= SeaLevel || !sulfurDepth(c.h, y) {
		return false
	}
	if y < -32 {
		n := g.cave.FBm(float64(x)/260, float64(y)/120, 2, 2, 0.5) + g.cave.FBm(float64(z)/260, 0, 1, 2, 0.5)
		if n > 0.35 {
			return false // deep_dark comes first
		}
	}
	return true
}

// sulfurGradient is SULFUR_CAVE_GRADIENT: a normal noise at octaves -5 and
// -3 (amplitudes 1, 0, 1), two stacks the second sampled ×1.0181, scaled
// by the legacy value factor (1/6 over the expected deviation 0.1333).
func (g *Generator) sulfurGradient(x, y, z int) float64 {
	const k = 1.0181268882175227
	fx, fy, fz := float64(x), float64(y), float64(z)
	a := 4*g.sulfurN[0].Noise3(fx/32, fy/32, fz/32) + g.sulfurN[1].Noise3(fx/8, fy/8, fz/8)
	b := 4*g.sulfurN[2].Noise3(fx*k/32, fy*k/32, fz*k/32) + g.sulfurN[3].Noise3(fx*k/8, fy*k/8, fz*k/8)
	return (a + b) / 7 * 1.25
}

// sulfurBand is SULFUR_CAVE_BANDS over the default rock of a sulfur caves
// cell: cinnabar, sulfur or cinnabar by the gradient, else the rock stays.
func (g *Generator) sulfurBand(b uint32, c column, x, y, z int) uint32 {
	if (b != Stone && b != Deepslate) || !g.inSulfurCaves(c, x, y, z) {
		return b
	}
	switch v := g.sulfurGradient(x, y, z); {
	case v >= -0.4 && v <= -0.1:
		return CinnabarBlock
	case v >= 0 && v <= 0.4:
		return SulfurBlock
	case v >= 0.4:
		return CinnabarBlock
	}
	return b
}

// chunkHasSulfurClimate: the sulfur caves' parameter box at any of the
// chunk's four quarter columns or its centre (the climate varies over
// hundreds of blocks; the features check every cell they touch).
func (reg *owRegion) chunkHasSulfurClimate(ox, oz int) bool {
	for _, d := range [5][2]int{{4, 4}, {4, 12}, {12, 4}, {12, 12}, {8, 8}} {
		if reg.col(ox+d[0], oz+d[1]).sulfur {
			return true
		}
	}
	return false
}

// sulfurFeatures replays chunk (ox, oz)'s sulfur caves features.
func (g *Generator) sulfurFeatures(reg *owRegion, ox, oz int) {
	if !reg.chunkHasSulfurClimate(ox, oz) {
		return
	}
	r := newTreeRNG(g.seed^0x5A1FE, ox, oz)
	rangeY := func() int { return MinY + r.Intn(256-MinY+1) } // uniform bottom..256
	sul := func(x, y, z int) bool { return reg.caveBiomeAt(x, y, z) == sulfurCaves }
	// ROOTED_SULFUR_SPRING ×1–2: under a ceiling (scanning up through air).
	for i, n := 0, 1+r.Intn(2); i < n; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, y, z, +1, 12, solid); ok && sul(x, cy-1, z) {
			g.rootedSulfurSpring(r, reg, x, cy-1, z)
		}
	}
	// SULFUR_POOL ×256: on a solid cell, up to the first air (32 at most),
	// the floor under it must be sulfur.
	for i := 0; i < 256; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if !solid(reg.read(x, y, z)) {
			continue
		}
		ay, ok := reg.scanUpForAir(x, y, z, 32)
		if !ok || reg.read(x, ay-1, z) != SulfurBlock || !sul(x, ay-1, z) {
			continue
		}
		g.sulfurPool(r, reg, x, ay-1, z)
	}
	// SULFUR_SPIKE_CLUSTER ×48–96.
	for i, n := 0, 48+r.Intn(49); i < n; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if sul(x, y, z) {
			g.speleothemCluster(sulfurCfg, r, reg, x, y, z)
		}
	}
	// SULFUR_SPIKE ×192–256, one to five each (clamped normal spread 3 / 0.6).
	for i, n := 0, 192+r.Intn(65); i < n; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		for k, m := 0, 1+r.Intn(5); k < m; k++ {
			px := x + int(clampedNormal(r, 0, 3, -10, 10))
			py := y + int(clampedNormal(r, 0, 0.6, -2, 2))
			pz := z + int(clampedNormal(r, 0, 3, -10, 10))
			if !sul(px, py, pz) {
				continue
			}
			if r.Intn(2) == 0 { // the selector: a spike on the floor
				if fy, ok := reg.scanForWet(px, py, pz, -1, 12); ok {
					g.pointedDripstone(sulfurCfg, r, reg, px, fy+1, pz)
				}
			} else { // or under the ceiling
				if cy, ok := reg.scanForWet(px, py, pz, +1, 12); ok {
					g.pointedDripstone(sulfurCfg, r, reg, px, cy-1, pz)
				}
			}
		}
	}
}

// scanUpForAir is EnvironmentScanPlacement upward for air with no search
// condition: the start cell, then up to max steps.
func (reg *owRegion) scanUpForAir(x, y, z, max int) (int, bool) {
	for i := 0; i < max; i++ {
		if reg.read(x, y, z) == Air {
			return y, true
		}
		y++
		if y >= MinY+len(reg.ch.Sections)*16 {
			return 0, false
		}
	}
	if reg.read(x, y, z) == Air {
		return y, true
	}
	return 0, false
}

// sulfurPool is SULFUR_POOL: a LakeFeature of water with a sulfur barrier
// (never over a sulfur spike, never replacing #features_cannot_replace),
// then, if the lake went in, wet potent sulfur on the first solid cell
// under water within four below the origin.
func (g *Generator) sulfurPool(r TreeRNG, reg *owRegion, x, y, z int) {
	if !g.sulfurLake(r, reg, x, y, z) {
		return
	}
	py := y
	for i := 0; ; i++ {
		if solid(reg.read(x, py, z)) && IsWater(reg.read(x, py+1, z)) {
			reg.set(x, py, z, potentSulfurWet)
			return
		}
		if i == 4 {
			return
		}
		py--
		if py < MinY {
			return
		}
	}
}

// sulfurLake is LakeFeature with the sulfur pool's configuration: four to
// seven ellipsoids in a 16×8×16 box around the origin (8 west, 4 down, 8
// north), refused if its wall would be open or fluid; water in the bottom
// half, air above, and a sulfur shell (the upper half's at one in two).
func (g *Generator) sulfurLake(r TreeRNG, reg *owRegion, x, y, z int) bool {
	if y <= MinY+4 {
		return false
	}
	bx, by, bz := x-8, y-4, z-8
	var grid [2048]bool
	idx := func(xx, zz, yy int) int { return (xx*16+zz)*8 + yy }
	for i, spots := 0, r.Intn(4)+4; i < spots; i++ {
		xr := r.Float64()*6 + 3
		yr := r.Float64()*4 + 2
		zr := r.Float64()*6 + 3
		xp := r.Float64()*(16-xr-2) + 1 + xr/2
		yp := r.Float64()*(8-yr-4) + 2 + yr/2
		zp := r.Float64()*(16-zr-2) + 1 + zr/2
		for xx := 1; xx < 15; xx++ {
			for zz := 1; zz < 15; zz++ {
				for yy := 1; yy < 7; yy++ {
					xd := (float64(xx) - xp) / (xr / 2)
					yd := (float64(yy) - yp) / (yr / 2)
					zd := (float64(zz) - zp) / (zr / 2)
					if xd*xd+yd*yd+zd*zd < 1 {
						grid[idx(xx, zz, yy)] = true
					}
				}
			}
		}
	}
	edge := func(xx, zz, yy int) bool {
		return !grid[idx(xx, zz, yy)] &&
			(xx < 15 && grid[idx(xx+1, zz, yy)] || xx > 0 && grid[idx(xx-1, zz, yy)] ||
				zz < 15 && grid[idx(xx, zz+1, yy)] || zz > 0 && grid[idx(xx, zz-1, yy)] ||
				yy < 7 && grid[idx(xx, zz, yy+1)] || yy > 0 && grid[idx(xx, zz, yy-1)])
	}
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !edge(xx, zz, yy) {
					continue
				}
				s := reg.read(bx+xx, by+yy, bz+zz)
				if yy >= 4 && IsFluid(s) {
					return false
				}
				if yy < 4 && !solid(s) && s != Water {
					return false
				}
				if s >= sulfurSpikeLo && s <= sulfurSpikeHi { // can_place_feature: not over a spike
					return false
				}
			}
		}
	}
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !grid[idx(xx, zz, yy)] || inAnyRange(reg.read(bx+xx, by+yy, bz+zz), featuresCannotReplace) {
					continue
				}
				if yy >= 4 {
					reg.set(bx+xx, by+yy, bz+zz, Air)
				} else {
					reg.set(bx+xx, by+yy, bz+zz, Water)
				}
			}
		}
	}
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !edge(xx, zz, yy) || (yy >= 4 && r.Intn(2) == 0) {
					continue
				}
				s := reg.read(bx+xx, by+yy, bz+zz)
				if solid(s) && !inAnyRange(s, featuresCannotReplace) && !IsLeaves(s) && !IsLog(s) {
					reg.set(bx+xx, by+yy, bz+zz, SulfurBlock)
				}
			}
		}
	}
	return true
}

// sulfurSpringSizes is SULFUR_SPRING's weighted choice: each size's weight,
// its tuff scatter (count, horizontal spread) and its templates.
var sulfurSpringSizes = []struct {
	weight, tuff, spread int
	templates            []string
}{
	{200, 64, 7, []string{"small_1", "small_2", "small_3", "small_4"}},
	{90, 80, 8, []string{"medium_1", "medium_2", "medium_3"}},
	{20, 96, 9, []string{"large_1", "large_2"}},
	{5, 128, 10, []string{"extra_large_1"}},
}

// rootedSulfurSpring is RootSystemFeature with ROOTED_SULFUR_SPRING's
// configuration: from an air cell, up at most 184 cells and never above the
// surface, the first air cell with five clear above it and the ground level
// (solid two below, air two above) eight out in each direction, standing on
// solid ground that is not lava, takes the spring; the column below it
// takes sulfur roots (twenty tries a level within three), and a sulfur
// block hangs at the origin under a sturdy ceiling.
func (g *Generator) rootedSulfurSpring(r TreeRNG, reg *owRegion, x, y, z int) {
	if reg.read(x, y, z) != Air {
		return
	}
	top := reg.col(x, z).h // WORLD_SURFACE: the ground, or the sea over it
	if top < SeaLevel {
		top = SeaLevel
	}
	placed := false
	for i := 0; i < 184; i++ {
		wy := y + 1 + i
		if top < wy {
			return
		}
		if !springOpen(reg.read(x, wy, z)) || !g.springSpace(reg, x, wy, z) {
			continue
		}
		if below := reg.read(x, wy-1, z); IsLava(below) || !solid(below) {
			return
		}
		if g.sulfurSpring(r, reg, x, wy, z) {
			for py := y; py < y+i; py++ { // the roots, up the column
				for k := 0; k < 20; k++ {
					px := x + r.Intn(3) - r.Intn(3)
					pz := z + r.Intn(3) - r.Intn(3)
					if azaleaRootReplaceable(reg.read(px, py, pz)) {
						reg.set(px, py, pz, SulfurBlock)
					}
				}
			}
			placed = true
			break
		}
	}
	if !placed {
		return
	}
	// One hanging root (radius 1, span 1): the origin itself.
	if reg.read(x, y, z) == Air && holdsFace(reg.read(x, y+1, z)) {
		reg.set(x, y, z, SulfurBlock)
	}
}

// springOpen is the root system's "air": vanilla places the spring at the
// LAKES step, before any grass or flowers, where the engine's surface is
// already planted — so a plant counts as the air it grew in.
func springOpen(s uint32) bool { return s == Air || IsReplaceable(s) }

// springSpace is RootSystemFeature.spaceForTree: five air cells above, and
// eight out to the south, west, north and east the cell two below is not
// air and the cell two above is.
func (g *Generator) springSpace(reg *owRegion, x, y, z int) bool {
	for k := 1; k <= 5; k++ {
		if !springOpen(reg.read(x, y+k, z)) {
			return false
		}
	}
	for _, d := range [4][2]int{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} {
		cx, cz := x+d[0]*8, z+d[1]*8
		if springOpen(reg.read(cx, y-2, cz)) || !springOpen(reg.read(cx, y+2, cz)) {
			return false
		}
	}
	return true
}

// sulfurSpring is SULFUR_SPRING: a size by weight (200 small, 90 medium,
// 20 large, 5 extra large), then the sequence — tuff scattered on solid
// ground about the spot (a trapezoid spread, three up or down, dropping up
// to four to land), and, if any went down, one of the size's templates
// under a random rotation, centred on the spot seven below it.
func (g *Generator) sulfurSpring(r TreeRNG, reg *owRegion, x, y, z int) bool {
	w := r.Intn(315)
	size := sulfurSpringSizes[len(sulfurSpringSizes)-1]
	for _, s := range sulfurSpringSizes {
		if w < s.weight {
			size = s
			break
		}
		w -= s.weight
	}
	trap := func(k int) int { return r.Intn(k+1) - r.Intn(k+1) }
	any := false
	for i := 0; i < size.tuff; i++ {
		px := x + trap(size.spread)
		py := y + trap(3)
		pz := z + trap(size.spread)
		fy, ok := reg.scanDownForSolid(px, py, pz, 4)
		if !ok {
			continue
		}
		reg.set(px, fy, pz, stoneTuff)
		any = true
	}
	if !any {
		return false
	}
	t := TemplateByName("spring/sulfur_spring_" + size.templates[r.Intn(len(size.templates))])
	if t == nil {
		return false
	}
	rot := r.Intn(4)
	g.stampSpringTemplate(reg, t, x, y-7, z, rot)
	return true
}

// scanDownForSolid is EnvironmentScanPlacement downward for a solid cell
// with no search condition: the start cell, then up to max steps.
func (reg *owRegion) scanDownForSolid(x, y, z, max int) (int, bool) {
	for i := 0; i < max; i++ {
		if solid(reg.read(x, y, z)) {
			return y, true
		}
		y--
		if y < MinY {
			return 0, false
		}
	}
	if solid(reg.read(x, y, z)) {
		return y, true
	}
	return 0, false
}

// stampSpringTemplate is TemplateFeature.place: the template's corner
// shifted by half its size along the rotated -X and -Z, so it centres on
// the origin, every block (air included) placed under the rotation.
func (g *Generator) stampSpringTemplate(reg *owRegion, t *Template, x, y, z, rot int) {
	// rotation.rotate(WEST) × sizeX/2 and rotation.rotate(NORTH) × sizeZ/2.
	hx, hz := t.Size[0]/2, t.Size[2]/2
	var ox, oz int
	switch rot & 3 {
	case 0: // NONE: west, north
		ox, oz = -hx, -hz
	case 1: // CLOCKWISE_90: north, east
		ox, oz = hz, -hx
	case 2: // CLOCKWISE_180: east, south
		ox, oz = hx, hz
	case 3: // COUNTERCLOCKWISE_90: south, west
		ox, oz = -hz, hx
	}
	px, pz := x+ox, z+oz
	for _, b := range t.Blocks {
		state := t.resolved[rot&3][b[3]]
		if state == tmplSkip {
			continue
		}
		tx, ty, tz := transformPos(b[0], b[1], b[2], rot, mirNone)
		reg.set(px+tx, y+ty, pz+tz, state)
	}
}
