package worldgen

// Ocean ruins. Vanilla's OceanRuinStructure/OceanRuinPieces: one site per
// 20-chunk cell on an ocean floor; a large ruin three times in ten (else a
// small one), and a large one usually (nine in ten) brings a cluster of four
// to eight small ruins scattered round it. Warm oceans get the sandstone
// "warm" pieces; the other oceans get a stone-brick piece with a cracked and
// a mossy copy of the same piece layered over it at lower integrity, which is
// what gives a cold ruin its mottled masonry. Every piece is stamped from the
// real vanilla template with the vanilla decay (BlockRotProcessor), the
// template's air left as the sea around it (STRUCTURE_AND_AIR), up to five of
// its sand (warm) or gravel (cold) blocks turned suspicious for a brush to
// find (the capped archaeology rule), its "chest" marker a loot chest and its
// "drowned" markers the drowned the server seeds when a player arrives.

const (
	oceanRuinCell    = 320 // spacing 20 chunks
	oceanRuinLarge   = 0.3 // large_probability
	oceanRuinCluster = 0.9 // cluster_probability
	oceanRuinSusCap  = 5   // the archaeology rule's CappedProcessor limit
)

var (
	warmRuins    = ruinNames("warm", 1, 2, 3, 4, 5, 6, 7, 8)
	brickRuins   = ruinNames("brick", 1, 2, 3, 4, 5, 6, 7, 8)
	crackedRuins = ruinNames("cracked", 1, 2, 3, 4, 5, 6, 7, 8)
	mossyRuins   = ruinNames("mossy", 1, 2, 3, 4, 5, 6, 7, 8)
	bigWarmRuins = ruinNames("big_warm", 4, 5, 6, 7)
	bigBrick     = ruinNames("big_brick", 1, 2, 3, 8)
	bigCracked   = ruinNames("big_cracked", 1, 2, 3, 8)
	bigMossy     = ruinNames("big_mossy", 1, 2, 3, 8)

	SuspiciousGravel = blockBase("suspicious_gravel")
	chestWaterlogged = blockBase("chest") // facing north, single, waterlogged
)

func ruinNames(kind string, ns ...int) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, "underwater_ruin/"+kind+"_"+string(rune('0'+n)))
	}
	return out
}

// warmOceanBiomes is the has_structure/ocean_ruin_warm tag; every other ocean
// is in ocean_ruin_cold.
var warmOceanBiomes = map[string]bool{
	"minecraft:warm_ocean": true, "minecraft:lukewarm_ocean": true, "minecraft:deep_lukewarm_ocean": true,
}

var coldOceanBiomes = map[string]bool{
	"minecraft:ocean": true, "minecraft:deep_ocean": true, "minecraft:cold_ocean": true,
	"minecraft:deep_cold_ocean": true, "minecraft:frozen_ocean": true, "minecraft:deep_frozen_ocean": true,
}

// OceanRuinPiece is one stamped template of a ruin site.
type OceanRuinPiece struct {
	Tmpl      string
	X, Y, Z   int // min-corner origin; Y is the seabed row the piece stands on
	Rot       int
	Integrity float64
	Large     bool
	salt      uint64      // decay hash salt (distinct per layered piece)
	Sus       [][3]int    // suspicious sand/gravel cells, world coordinates
	Drowned   [][3]int    // drowned marker cells, world coordinates
	Chests    []ShipChest // the piece's chest with its big/small table
}

// OceanRuins is one ruin site (or the zero value).
type OceanRuins struct {
	X, Z   int // the site's origin (the large/first piece's min corner)
	Warm   bool
	Pieces []OceanRuinPiece
	Exists bool
}

// OceanRuinsIn returns the ruin site owning the cell containing (wx,wz), if
// the site is submerged ocean floor.
func (g *Generator) OceanRuinsIn(wx, wz int) OceanRuins {
	if g.nether || g.end {
		return OceanRuins{}
	}
	ox, oz := cellOrigin(wx, oceanRuinCell), cellOrigin(wz, oceanRuinCell)
	x := ox + 40 + int(hash01(g.seed, ox, oz, 0x0CE1)*float64(oceanRuinCell-80))
	z := oz + 40 + int(hash01(g.seed, ox, oz, 0x0CE2)*float64(oceanRuinCell-80))
	biome := g.BiomeName(x, z)
	warm := warmOceanBiomes[biome]
	if !warm && !coldOceanBiomes[biome] {
		return OceanRuins{}
	}
	if g.Height(x, z) >= SeaLevel-1 {
		return OceanRuins{} // dry (or barely wet) ground: no ruin
	}
	r := newJigsawRNG(g.seed, x^0x0CEA0000, z)
	site := OceanRuins{X: x, Z: z, Warm: warm, Exists: true}
	rot := r.intn(4)
	large := r.float() <= oceanRuinLarge
	integrity := 0.8
	if large {
		integrity = 0.9
	}
	g.addRuinPiece(&site, r, x, z, rot, large, integrity)
	if large && r.float() <= oceanRuinCluster {
		g.addRuinCluster(&site, r, x, z)
	}
	return site
}

// addRuinCluster scatters 4–8 small ruins round the large one's 16×16 box
// (OceanRuinPieces.addClusterRuins/allPositions), skipping any that would
// overlap it.
func (g *Generator) addRuinCluster(site *OceanRuins, r *jigsawRNG, x, z int) {
	nextInt := func(lo, hi int) int { return lo + r.intn(hi-lo+1) }
	spots := [][2]int{
		{x - 16 + nextInt(1, 8), z + 16 + nextInt(1, 7)},
		{x - 16 + nextInt(1, 8), z + nextInt(1, 7)},
		{x - 16 + nextInt(1, 8), z - 16 + nextInt(4, 8)},
		{x + nextInt(1, 7), z + 16 + nextInt(1, 7)},
		{x + nextInt(1, 7), z - 16 + nextInt(4, 6)},
		{x + 16 + nextInt(1, 7), z + 16 + nextInt(3, 8)},
		{x + 16 + nextInt(1, 7), z + nextInt(1, 7)},
		{x + 16 + nextInt(1, 7), z - 16 + nextInt(4, 8)},
	}
	n := nextInt(4, 8)
	for i := 0; i < n; i++ {
		if len(spots) == 0 {
			continue
		}
		k := r.intn(len(spots))
		p := spots[k]
		spots = append(spots[:k], spots[k+1:]...)
		rot := r.intn(4)
		// A small piece spans 6×7; skip one that would touch the big box.
		if p[0]+5 >= x && p[0] <= x+15 && p[1]+6 >= z && p[1] <= z+15 {
			continue
		}
		g.addRuinPiece(site, r, p[0], p[1], rot, false, 0.8)
	}
}

// addRuinPiece is OceanRuinPieces.addPiece: one warm piece, or the cold
// brick piece with its cracked (0.7) and mossy (0.5) overlays.
func (g *Generator) addRuinPiece(site *OceanRuins, r *jigsawRNG, x, z, rot int, large bool, integrity float64) {
	if site.Warm {
		set := warmRuins
		if large {
			set = bigWarmRuins
		}
		g.appendRuinPiece(site, set[r.intn(len(set))], x, z, rot, large, integrity)
		return
	}
	brick, cracked, mossy := brickRuins, crackedRuins, mossyRuins
	if large {
		brick, cracked, mossy = bigBrick, bigCracked, bigMossy
	}
	n := r.intn(len(brick))
	g.appendRuinPiece(site, brick[n], x, z, rot, large, integrity)
	g.appendRuinPiece(site, cracked[n], x, z, rot, large, 0.7)
	g.appendRuinPiece(site, mossy[n], x, z, rot, large, 0.5)
}

func (g *Generator) appendRuinPiece(site *OceanRuins, name string, x, z, rot int, large bool, integrity float64) {
	t := TemplateByName(name)
	if t == nil {
		return
	}
	y := g.ruinFloorY(t, x, z, rot)
	p := OceanRuinPiece{Tmpl: name, X: x, Y: y, Z: z, Rot: rot, Integrity: integrity, Large: large,
		salt: 0x0CE0 + uint64(len(site.Pieces))}
	// The archaeology rule: every surviving sand (warm) or gravel (cold)
	// block is a candidate; the capped processor keeps five of them, chosen
	// here as the five lowest position hashes so a chunk-clipped stamp picks
	// the same cells.
	want := "sand"
	if !site.Warm {
		want = "gravel"
	}
	type cand struct {
		pos [3]int
		h   float64
	}
	var cands []cand
	for _, b := range t.Blocks {
		if trimNS(t.Palette[b[3]].Name) != want {
			continue
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], rot)
		wx, wy, wz := x+rx, y+ry, z+rz
		if ruinRotted(g.seed, wx, wy, wz, p.salt, integrity) {
			continue
		}
		cands = append(cands, cand{[3]int{wx, wy, wz}, hash01(g.seed, wx, wz*8192+wy, 0x0CEC)})
	}
	for k := 0; k < oceanRuinSusCap && len(cands) > 0; k++ {
		best := 0
		for i := range cands {
			if cands[i].h < cands[best].h {
				best = i
			}
		}
		p.Sus = append(p.Sus, cands[best].pos)
		cands = append(cands[:best], cands[best+1:]...)
	}
	table := "chests/underwater_ruin_small"
	if large {
		table = "chests/underwater_ruin_big"
	}
	for _, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		p.Chests = append(p.Chests, ShipChest{x + rx, y + ry, z + rz, table})
	}
	for _, m := range t.Mobs {
		if m.Type != "drowned" {
			continue
		}
		rx, ry, rz := t.rotatePos(m.Pos[0], m.Pos[1], m.Pos[2], rot)
		p.Drowned = append(p.Drowned, [3]int{x + rx, y + ry, z + rz})
	}
	site.Pieces = append(site.Pieces, p)
}

// ruinRotted is the BlockRotProcessor roll for one cell: dropped with
// probability 1-integrity, deterministic per position and piece.
func ruinRotted(seed int64, wx, wy, wz int, salt uint64, integrity float64) bool {
	return integrity < 1 && hash01(seed, wx, wz*8192+wy, salt) >= integrity
}

// ruinFloorY is OceanRuinPiece.postProcess + getHeight: the piece stands on
// the ocean floor at its origin, but sinks to the lowest floor under its
// footprint when the ground falls away by more than two blocks under most
// of it, so a ruin on a slope is not left hanging in the water.
func (g *Generator) ruinFloorY(t *Template, x, z, rot int) int {
	y := g.Height(x, z)
	sx, sz := t.Size[0], t.Size[2]
	if rot&1 == 1 {
		sx, sz = sz, sx
	}
	lowest, sunk := y-1, 0
	for dx := 0; dx < sx; dx++ {
		for dz := 0; dz < sz; dz++ {
			top := g.Height(x+dx, z+dz) - 1 // the top solid block of the column
			if top < lowest {
				lowest = top
			}
			if top < y-3 {
				sunk++
			}
		}
	}
	if y-1-lowest > 2 && sunk > sx-2 {
		return lowest + 1
	}
	return y
}

// stampOceanRuins stamps every ruin piece overlapping this chunk.
func (g *Generator) stampOceanRuins(ch *Chunk, cx, cz int32) {
	if g.nether || g.end {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(oceanRuinCell) {
		site := g.OceanRuinsIn(baseX+8+off[0], baseZ+8+off[1])
		if !site.Exists {
			continue
		}
		for i := range site.Pieces {
			g.stampRuinPiece(ch, cx, cz, site.Warm, &site.Pieces[i])
		}
	}
}

func (g *Generator) stampRuinPiece(ch *Chunk, cx, cz int32, warm bool, p *OceanRuinPiece) {
	t := TemplateByName(p.Tmpl)
	if t == nil {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	// Quick reject: the piece's box misses this chunk.
	sx, sz := t.Size[0], t.Size[2]
	if p.Rot&1 == 1 {
		sx, sz = sz, sx
	}
	if p.X >= baseX+16 || p.X+sx <= baseX || p.Z >= baseZ+16 || p.Z+sz <= baseZ {
		return
	}
	sus := SuspiciousSand
	if !warm {
		sus = SuspiciousGravel
	}
	for _, b := range t.Blocks {
		state := t.resolved[p.Rot&3][b[3]]
		if state == tmplSkip || state == Air {
			continue // markers place nothing; air leaves the sea in place
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], p.Rot)
		wx, wy, wz := p.X+rx, p.Y+ry, p.Z+rz
		if wx-baseX < 0 || wx-baseX >= 16 || wz-baseZ < 0 || wz-baseZ >= 16 {
			continue
		}
		if ruinRotted(g.seed, wx, wy, wz, p.salt, p.Integrity) {
			continue
		}
		for _, s := range p.Sus {
			if s[0] == wx && s[1] == wy && s[2] == wz {
				state = sus
				break
			}
		}
		setSectionBlock(ch, wx-baseX, wy, wz-baseZ, state, true)
	}
	for _, c := range p.Chests {
		state := ChestNorth
		if c.Y < SeaLevel {
			state = chestWaterlogged
		}
		setSectionBlock(ch, c.X-baseX, c.Y, c.Z-baseZ, state, true)
	}
}

// NearestOceanRuin finds the closest ruin site to (wx, wz) within radius
// blocks, scanning the placement cells around the point.
func (g *Generator) NearestOceanRuin(wx, wz, radius int) (x, z int, ok bool) {
	bestD := radius * radius
	cells := radius/oceanRuinCell + 1
	ox, oz := cellOrigin(wx, oceanRuinCell), cellOrigin(wz, oceanRuinCell)
	for cx := -cells; cx <= cells; cx++ {
		for cz := -cells; cz <= cells; cz++ {
			s := g.OceanRuinsIn(ox+cx*oceanRuinCell, oz+cz*oceanRuinCell)
			if !s.Exists {
				continue
			}
			dx, dz := s.X-wx, s.Z-wz
			if d := dx*dx + dz*dz; d < bestD {
				x, z, ok, bestD = s.X, s.Z, true, d
			}
		}
	}
	return x, z, ok
}

// NearestDolphinTreasure is what a fed dolphin swims for: the closer of the
// nearest shipwreck and the nearest ocean ruin (vanilla's #dolphin_located
// structure tag holds both).
func (g *Generator) NearestDolphinTreasure(wx, wz, radius int) (x, z int, ok bool) {
	sx, sz, sok := g.NearestShipwreck(wx, wz, radius)
	rx, rz, rok := g.NearestOceanRuin(wx, wz, radius)
	switch {
	case sok && rok:
		ds := (sx-wx)*(sx-wx) + (sz-wz)*(sz-wz)
		dr := (rx-wx)*(rx-wx) + (rz-wz)*(rz-wz)
		if dr < ds {
			return rx, rz, true
		}
		return sx, sz, true
	case rok:
		return rx, rz, true
	case sok:
		return sx, sz, true
	}
	return 0, 0, false
}
