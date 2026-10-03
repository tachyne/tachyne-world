package worldgen

import "math/rand"

// The nether's other features, per vanilla's placements. Ores as vanilla's
// (gravel, blackstone, gold, quartz, magma, soul sand veins in the base
// stone; ancient debris scattered and never exposed to air), glowstone blobs
// from the roofs, fire on netherrack and soul fire on soul soil, the soul
// sand valley's basalt pillars and crimson roots, the basalt deltas' lava
// deltas, basalt columns and basalt/blackstone blobs, and lava springs in
// the rock. Vanilla's absolute heights sit on a lava level of 31; here the
// lava sea is NetherLavaSea, so absolute anchors shift by the difference.

var (
	AncientDebris = blockBase("ancient_debris")
	Fire          = blockBase("fire")
	SoulFire      = blockBase("soul_fire")
	NetherBricks  = blockBase("nether_bricks")
)

const vanillaNetherLava = 31 // vanilla's lava level; absolute anchors shift onto ours

// baseStoneNether is #base_stone_nether: what the ores replace.
func baseStoneNether(s uint32) bool { return s == Netherrack || s == Basalt || s == Blackstone }

// netherOreSpec is one of the nether's scattered ores (ancient debris):
// attempts per chunk, blocks per attempt, the height band (engine y), and
// whether the band is a triangle.
type netherOreSpec struct {
	ore            uint32
	attempts, size int
	minY, maxY     int
	triangle       bool
}

func netherOreSpecs(f netherFrame) []netherOreSpec {
	top := f.ceiling
	return []netherOreSpec{
		{AncientDebris, 1, 3, f.y(8), f.y(24), true},       // ORE_ANCIENT_DEBRIS_LARGE
		{AncientDebris, 1, 2, f.floor + 8, top - 8, false}, // ORE_ANCIENT_DEBRIS_SMALL
	}
}

// The nether's blob ores: vanilla 26.3's step-7 placements, each an
// OreFeature of its size replacing netherrack only (BlockMatchTest, no air
// discard), with its count, height band and biome filter — ore_magma
// everywhere, ore_soul_sand in the soul sand valley, gravel, blackstone,
// gold and quartz everywhere but the basalt deltas, which list their own
// richer gold and quartz instead. Placed through the overworld's oreBlob.
var (
	netherNotDeltas = biomeSet("nether_wastes", "soul_sand_valley", "crimson_forest", "warped_forest")
	netherDeltas    = biomeSet("basalt_deltas")

	netherOrePlacements        = netherOrePlacementsIn(nativeNetherFrame)
	vanillaNetherOrePlacements = netherOrePlacementsIn(vanillaNetherFrame)
)

// netherOrePlacementsIn is the ore list on a frame: above_bottom anchors
// count from its floor, absolute ones shift with its lava sea.
func netherOrePlacementsIn(f netherFrame) []orePlacement {
	band := oreUniform(oreAbs(f.floor+10), oreAbs(f.ceiling-10)) // above_bottom 10 .. below_top 10
	return []orePlacement{
		{name: "ore_magma", cfg: netherrackOre(MagmaBlock, 33), count: 4,
			height: oreUniform(oreAbs(f.y(27)), oreAbs(f.y(36))), salt: 0x4E_01},
		{name: "ore_soul_sand", cfg: netherrackOre(SoulSand, 12), count: 12,
			height: oreUniform(oreAbs(f.floor), oreAbs(f.y(31))), biomes: biomeSet("soul_sand_valley"), salt: 0x4E_02},
		{name: "ore_gravel_nether", cfg: netherrackOre(Gravel, 33), count: 2,
			height: oreUniform(oreAbs(f.y(5)), oreAbs(f.y(41))), biomes: netherNotDeltas, salt: 0x4E_03},
		{name: "ore_blackstone", cfg: netherrackOre(Blackstone, 33), count: 2,
			height: oreUniform(oreAbs(f.y(5)), oreAbs(f.y(31))), biomes: netherNotDeltas, salt: 0x4E_04},
		{name: "ore_gold_nether", cfg: netherrackOre(NetherGoldOre, 10), count: 10, height: band, biomes: netherNotDeltas, salt: 0x4E_05},
		{name: "ore_quartz_nether", cfg: netherrackOre(NetherQuartzOre, 14), count: 16, height: band, biomes: netherNotDeltas, salt: 0x4E_06},
		{name: "ore_gold_deltas", cfg: netherrackOre(NetherGoldOre, 10), count: 20, height: band, biomes: netherDeltas, salt: 0x4E_07},
		{name: "ore_quartz_deltas", cfg: netherrackOre(NetherQuartzOre, 14), count: 32, height: band, biomes: netherDeltas, salt: 0x4E_08},
	}
}

// placeNetherOres runs the nether's ores into this chunk: the blob ores as
// OreFeature ellipsoids from the 3×3 origin chunks, each origin's placement
// on its own stream (as the overworld's, placeOres), a blob with a player's
// build in its box left out; then ancient debris, scattered and never
// beside air, chunk-local.
func (g *Generator) placeNetherOres(ch *Chunk, cx, cz int32) {
	// oreBlob's region: the chunk being written. The nether's ores have no
	// air discard, so the region is never asked about a neighbour's cells.
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	guard := g.newBuildIndex(cx, cz, 2)
	top := MinY + len(ch.Sections)*16
	f, placements := g.nf(), netherOrePlacements
	if g.vNether() != nil {
		placements = vanillaNetherOrePlacements
	}
	for i := range placements {
		p := &placements[i]
		for dcx := int32(-1); dcx <= 1; dcx++ {
			for dcz := int32(-1); dcz <= 1; dcz++ {
				g.runNetherOrePlacement(reg, guard, p, int(cx+dcx)*16, int(cz+dcz)*16, top)
			}
		}
	}

	rng := rand.New(rand.NewSource(oreSeed(g.seed^0x4E7A, cx, cz)))
	maxY := MinY + len(ch.Sections)*16 - 1
	at := func(lx, y, lz int) uint32 {
		if y < MinY || y > maxY {
			return Air
		}
		return sectionBlockAt(ch, lx, y, lz)
	}
	for _, spec := range netherOreSpecs(f) {
		for a := 0; a < spec.attempts; a++ {
			lx, lz := rng.Intn(16), rng.Intn(16)
			span := spec.maxY - spec.minY + 1
			y := spec.minY + rng.Intn(span)
			if spec.triangle {
				y = spec.minY + (rng.Intn(span)+rng.Intn(span))/2
			}
			y = clampInt(y, f.floor+1, maxY)
			// ScatteredOreFeature: up to size blocks about the origin, none
			// touching air.
			tries := rng.Intn(spec.size + 1)
			for i := 0; i < tries; i++ {
				d := i
				if d > 7 {
					d = 7
				}
				off := func() int { return int(roundF((rng.Float32() - rng.Float32()) * float32(d))) }
				px, py, pz := lx+off(), y+off(), lz+off()
				if px < 0 || px > 15 || pz < 0 || pz > 15 || py <= f.floor || py >= maxY || !baseStoneNether(at(px, py, pz)) {
					continue
				}
				exposed := false
				for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
					nx, nz := px+o[0], pz+o[2]
					if nx < 0 || nx > 15 || nz < 0 || nz > 15 {
						continue // the neighbour chunk's cells are unknown; assume rock
					}
					if at(nx, py+o[1], nz) == Air {
						exposed = true
						break
					}
				}
				if !exposed {
					setSectionBlock(ch, px, py, pz, spec.ore, true)
				}
			}
		}
	}
}

// runNetherOrePlacement draws origin chunk (ox, oz)'s blobs of one nether
// placement: count, in_square, the height band, then the biome filter at
// the origin.
func (g *Generator) runNetherOrePlacement(reg *owRegion, guard *buildIndex, p *orePlacement, ox, oz, top int) {
	r := newTreeRNG(g.seed^p.salt, ox, oz)
	for a := 0; a < p.count; a++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := p.height.sample(r, top)
		if p.biomes != nil && !p.biomes[g.netherBiome(x, z)] {
			continue
		}
		key := uint64(int64(ox))*0x9E3779B97F4A7C15 ^ uint64(int64(oz))*0xC2B2AE3D27D4EB4F ^ uint64(a)*0x165667B19E3779F9
		oreBlob(reg, guard, p.cfg, r, x, y, z, top, g.seed^p.salt, key)
	}
}

func roundF(f float32) float32 {
	if f < 0 {
		return -roundF(-f)
	}
	return float32(int(f + 0.5))
}

// biasedToBottom is BiasedToBottomInt.of(min, max).
func biasedToBottom(r TreeRNG, min, max int) int {
	return min + r.Intn(r.Intn(max-min+1)+1)
}

// netherChunkFeatures2 replays chunk (ncx, ncz)'s non-forest features into
// the region, in vanilla's order per biome.
func (g *Generator) netherChunkFeatures2(reg *netherRegion, ncx, ncz int32) {
	ox, oz := int(ncx)*16, int(ncz)*16
	r := newTreeRNG(g.seed^0x4E7F, ox, oz)
	d := reg.driver()
	biome := g.netherBiome(ox+8, oz+8)
	f := g.nf()
	fullRangeY := func() int { return f.floor + 1 + r.Intn(f.ceiling-f.floor-2) }
	range44 := func() int { return f.floor + 4 + r.Intn(f.ceiling-f.floor-8) }
	range1010 := func() int { return f.floor + 10 + r.Intn(f.ceiling-f.floor-20) }
	inBiome := func(x, z int) bool { return g.netherBiome(x, z) == biome }
	// LOCAL_MODIFICATIONS: basalt pillars (soul sand valley) ×10 FULL_RANGE
	if biome == "minecraft:soul_sand_valley" {
		for i := 0; i < 10; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if y := fullRangeY(); inBiome(x, z) {
				g.basaltPillar(r, x, y, z, d)
			}
		}
	}
	// SURFACE_STRUCTURES (basalt deltas): deltas 40/layer, columns 4 and 2 per layer
	if biome == "minecraft:basalt_deltas" {
		g.onEveryLayer(r, reg, ox, oz, 40, func(x, y, z int) {
			if inBiome(x, z) {
				g.deltaFeature(r, x, y, z, d)
			}
		})
		g.onEveryLayer(r, reg, ox, oz, 4, func(x, y, z int) {
			if inBiome(x, z) {
				g.basaltColumns(r, x, y, z, 1, 1, 4, d)
			}
		})
		g.onEveryLayer(r, reg, ox, oz, 2, func(x, y, z int) {
			if inBiome(x, z) {
				g.basaltColumns(r, x, y, z, 2+r.Intn(2), 5, 10, d)
			}
		})
	}
	// UNDERGROUND_DECORATION
	if biome == "minecraft:basalt_deltas" { // basalt blobs ×75, blackstone blobs ×25, FULL_RANGE
		for i := 0; i < 75; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if y := fullRangeY(); inBiome(x, z) {
				g.replaceBlobs(r, x, y, z, Netherrack, Basalt, d)
			}
		}
		for i := 0; i < 25; i++ {
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if y := fullRangeY(); inBiome(x, z) {
				g.replaceBlobs(r, x, y, z, Netherrack, Blackstone, d)
			}
		}
		for i := 0; i < 16; i++ { // SPRING_DELTA: lava, needs rock below, 4 rock 1 hole, RANGE_4_4
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if y := range44(); inBiome(x, z) {
				g.spring(x, y, z, true, 4, 1, func(s uint32) bool {
					return s == Netherrack || s == SoulSand || s == Gravel || s == MagmaBlock || s == Blackstone
				}, d)
			}
		}
	} else {
		for i := 0; i < 8; i++ { // SPRING_OPEN ×8 RANGE_4_4: 4 rock, 1 hole
			x, z := ox+r.Intn(16), oz+r.Intn(16)
			if y := range44(); inBiome(x, z) {
				g.spring(x, y, z, false, 4, 1, func(s uint32) bool { return s == Netherrack }, d)
			}
		}
	}
	// PATCH_FIRE (every biome) and PATCH_SOUL_FIRE (all but the crimson forest)
	g.firePatches(r, ox, oz, Fire, Netherrack, inBiome, d)
	if biome != "minecraft:crimson_forest" {
		g.firePatches(r, ox, oz, SoulFire, SoulSoil, inBiome, d)
	}
	// GLOWSTONE_EXTRA: 0..9 biased low, RANGE_4_4; GLOWSTONE: ×10 FULL_RANGE
	for i, n := 0, biasedToBottom(r, 0, 9); i < n; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if y := range44(); inBiome(x, z) {
			g.glowstoneBlob(r, x, y, z, d)
		}
	}
	for i := 0; i < 10; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if y := fullRangeY(); inBiome(x, z) {
			g.glowstoneBlob(r, x, y, z, d)
		}
	}
	// PATCH_CRIMSON_ROOTS (soul sand valley): one origin, 96 tries about it
	if biome == "minecraft:soul_sand_valley" {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := fullRangeY()
		for i := 0; i < 96; i++ {
			px, py, pz := x+triangleOffset(r, 7), y+triangleOffset(r, 3), z+triangleOffset(r, 7)
			if inBiome(px, pz) && d.Read(px, py, pz) == Air && netherPlantMayPlaceOn(d.Read(px, py-1, pz)) {
				d.Set(px, py, pz, CrimsonRoots)
			}
		}
	}
	// BROWN/RED_MUSHROOM_NETHER (the wastes and the forests)
	g.netherMushrooms(r, ox, oz, biome, d)
	// SPRING_CLOSED ×16 RANGE_10_10: lava sealed in five rock faces
	for i := 0; i < 16; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if y := range1010(); inBiome(x, z) {
			g.spring(x, y, z, false, 5, 0, func(s uint32) bool { return s == Netherrack }, d)
		}
	}
}

// triangleOffset is RandomOffsetPlacement.ofTriangle's spread on one axis.
func triangleOffset(r TreeRNG, spread int) int {
	if spread == 0 {
		return 0
	}
	return r.Intn(spread+1) - r.Intn(spread+1)
}

// firePatches is the fire placement: 0..5 origins in RANGE_4_4, each 96
// tries with a triangle offset (7, 3), fire where the cell is air over the
// one block it burns on.
func (g *Generator) firePatches(r TreeRNG, ox, oz int, fire, on uint32, inBiome func(x, z int) bool, d FungusDriver) {
	for i, n := 0, r.Intn(6); i < n; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		f := g.nf()
		y := f.floor + 4 + r.Intn(f.ceiling-f.floor-8)
		if !inBiome(x, z) {
			continue
		}
		for j := 0; j < 96; j++ {
			px, py, pz := x+triangleOffset(r, 7), y+triangleOffset(r, 3), z+triangleOffset(r, 7)
			if d.Read(px, py, pz) == Air && d.Read(px, py-1, pz) == on {
				d.Set(px, py, pz, fire)
			}
		}
	}
}

// glowstoneBlob is GlowstoneFeature: an air cell under netherrack, basalt
// or blackstone seeds glowstone, then 1500 tries grow it downward where a
// cell has exactly one glowstone neighbour.
func (g *Generator) glowstoneBlob(r TreeRNG, x, y, z int, d FungusDriver) {
	if d.Read(x, y, z) != Air {
		return
	}
	if a := d.Read(x, y+1, z); a != Netherrack && a != Basalt && a != Blackstone {
		return
	}
	d.Set(x, y, z, Glowstone)
	for i := 0; i < 1500; i++ {
		px, py, pz := x+r.Intn(8)-r.Intn(8), y-r.Intn(12), z+r.Intn(8)-r.Intn(8)
		if d.Read(px, py, pz) != Air {
			continue
		}
		n := 0
		for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			if d.Read(px+o[0], py+o[1], pz+o[2]) == Glowstone {
				n++
				if n > 1 {
					break
				}
			}
		}
		if n == 1 {
			d.Set(px, py, pz, Glowstone)
		}
	}
}

// basaltPillar is BasaltPillarFeature: from an air cell under rock, a basalt
// column drops to the floor with hang-offs on its sides, and a splayed base.
func (g *Generator) basaltPillar(r TreeRNG, x, y, z int, d FungusDriver) {
	if d.Read(x, y, z) != Air || d.Read(x, y+1, z) == Air {
		return
	}
	sides := [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	keep := [4]bool{true, true, true, true}
	py, floor := y, g.nf().floor
	for d.Read(x, py, z) == Air {
		if py <= floor {
			return
		}
		d.Set(x, py, z, Basalt)
		for i, s := range sides {
			if keep[i] {
				if r.Intn(10) != 0 {
					d.Set(x+s[0], py, z+s[1], Basalt)
				} else {
					keep[i] = false
				}
			}
		}
		py--
	}
	py++
	for _, s := range sides { // placeBaseHangOff
		if r.Intn(2) == 0 {
			d.Set(x+s[0], py, z+s[1], Basalt)
		}
	}
	py--
	for dx := -3; dx < 4; dx++ {
		for dz := -3; dz < 4; dz++ {
			if r.Intn(10) >= 10-absInt(dx)*absInt(dz) {
				continue
			}
			bx, by, bz := x+dx, py, z+dz
			for drop := 3; d.Read(bx, by-1, bz) == Air; {
				by--
				if drop--; drop <= 0 {
					break
				}
			}
			if d.Read(bx, by-1, bz) != Air {
				d.Set(bx, by, bz, Basalt)
			}
		}
	}
}

// deltaFeature is DeltaFeature: a diamond of lava (size 3–7 each way) with
// a magma rim nine times in ten (rim 0–2), on cells that are floor from
// every side but up.
func (g *Generator) deltaFeature(r TreeRNG, x, y, z int, d FungusDriver) {
	spawnRim := r.Float64() < 0.9
	rimX, rimZ := 0, 0
	if spawnRim {
		rimX, rimZ = r.Intn(3), r.Intn(3)
	}
	hasRim := spawnRim && rimX != 0 && rimZ != 0
	radX, radZ := 3+r.Intn(5), 3+r.Intn(5)
	limit := radX
	if radZ > limit {
		limit = radZ
	}
	clear := func(px, py, pz int) bool {
		s := d.Read(px, py, pz)
		if s == Lava || s == Bedrock || s == NetherBricks || s == NetherWart {
			return false
		}
		for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}, {0, 1, 0}} {
			air := d.Read(px+o[0], py+o[1], pz+o[2]) == Air
			up := o[1] == 1
			if (air && !up) || (!air && up) {
				return false
			}
		}
		return true
	}
	for dx := -radX; dx <= radX; dx++ {
		for dz := -radZ; dz <= radZ; dz++ {
			if absInt(dx)+absInt(dz) > limit {
				continue
			}
			px, pz := x+dx, z+dz
			if clear(px, y, pz) {
				if hasRim {
					d.Set(px, y, pz, MagmaBlock)
				}
				if clear(px+rimX, y, pz+rimZ) {
					d.Set(px+rimX, y, pz+rimZ, Lava)
				}
			}
		}
	}
}

// basaltColumns is BasaltColumnsFeature: clustered (nine times in ten:
// fifty columns within five) or scattered (fifteen within eight) basalt
// columns rising from the floor, each shorter with distance.
func (g *Generator) basaltColumns(r TreeRNG, x, y, z, reach, minH, maxH int, d FungusDriver) {
	cannotPlaceOn := func(s uint32) bool {
		return s == Lava || s == Bedrock || s == MagmaBlock || s == SoulSand || s == NetherBricks || s == NetherWart
	}
	f := g.nf()
	airOrLavaOcean := func(px, py, pz int) bool {
		s := d.Read(px, py, pz)
		return s == Air || (s == Lava && py <= f.lava)
	}
	canPlaceAt := func(px, py, pz int) bool {
		if !airOrLavaOcean(px, py, pz) {
			return false
		}
		b := d.Read(px, py-1, pz)
		return b != Air && !cannotPlaceOn(b)
	}
	if !canPlaceAt(x, y, z) {
		return
	}
	height := minH + r.Intn(maxH-minH+1)
	clustered := r.Float64() < 0.9
	spread, count := 8, 15
	if clustered {
		spread, count = 5, 50
	}
	if height < spread {
		spread = height
	}
	for i := 0; i < count; i++ {
		px, pz := x-spread+r.Intn(2*spread+1), z-spread+r.Intn(2*spread+1)
		blocks := height - (absInt(px-x) + absInt(pz-z))
		if blocks < 0 {
			continue
		}
		for cx := px - reach; cx <= px+reach; cx++ {
			for cz := pz - reach; cz <= pz+reach; cz++ {
				step := absInt(cx-px) + absInt(cz-pz)
				var cy int
				found := false
				if airOrLavaOcean(cx, y, cz) { // findSurface
					for cy = y; cy > f.floor+1 && step >= 0; step-- {
						if canPlaceAt(cx, cy, cz) {
							found = true
							break
						}
						cy--
					}
				} else { // findAir
					for cy = y; cy < f.ceiling && step >= 0; step-- {
						s := d.Read(cx, cy, cz)
						if cannotPlaceOn(s) {
							break
						}
						if s == Air {
							found = true
							break
						}
						cy++
					}
				}
				if !found {
					continue
				}
				for n := blocks - (absInt(cx-px)+absInt(cz-pz))/2; n >= 0; n-- {
					if airOrLavaOcean(cx, cy, cz) {
						d.Set(cx, cy, cz, Basalt)
					} else if d.Read(cx, cy, cz) != Basalt {
						break
					}
					cy++
				}
			}
		}
	}
}

// replaceBlobs is ReplaceBlobsFeature: from the first target block at or
// below the origin, a manhattan blob (radius 3–7 each axis) of the
// replacement.
func (g *Generator) replaceBlobs(r TreeRNG, x, y, z int, target, with uint32, d FungusDriver) {
	floor := g.nf().floor
	for d.Read(x, y, z) != target {
		y--
		if y <= floor+1 {
			return
		}
	}
	rx, ry, rz := 3+r.Intn(5), 3+r.Intn(5), 3+r.Intn(5)
	limit := rx
	if ry > limit {
		limit = ry
	}
	if rz > limit {
		limit = rz
	}
	for dx := -rx; dx <= rx; dx++ {
		for dy := -ry; dy <= ry; dy++ {
			for dz := -rz; dz <= rz; dz++ {
				if absInt(dx)+absInt(dy)+absInt(dz) > limit {
					continue
				}
				if d.Read(x+dx, y+dy, z+dz) == target {
					d.Set(x+dx, y+dy, z+dz, with)
				}
			}
		}
	}
}

// spring is SpringFeature for lava: the cell above must be valid rock (and
// below too when required), the cell itself air or rock, and of its four
// sides and floor exactly rockCount rock and holeCount air.
func (g *Generator) spring(x, y, z int, needBelow bool, rockCount, holeCount int, valid func(uint32) bool, d FungusDriver) {
	if !valid(d.Read(x, y+1, z)) || (needBelow && !valid(d.Read(x, y-1, z))) {
		return
	}
	if s := d.Read(x, y, z); s != Air && !valid(s) {
		return
	}
	rock, hole := 0, 0
	for _, o := range [5][3]int{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		s := d.Read(x+o[0], y+o[1], z+o[2])
		if valid(s) {
			rock++
		}
		if s == Air {
			hole++
		}
	}
	if rock == rockCount && hole == holeCount {
		d.Set(x, y, z, Lava)
	}
}

// NetherBiomeAt is the nether's biome at a column (the engine's zoning).
func (g *Generator) NetherBiomeAt(x, z int) string { return g.netherBiome(x, z) }
