package worldgen

// Village decor: the feature elements of the village pools — the trees, the
// hay, melon, pumpkin, snow and ice piles, the plains' flowers, the
// desert's cacti and the taiga's grass and berry bushes — and the pools'
// empty elements.
//
// The baked village pools hold only their templates, so the assembler
// always chose a lamp (or nothing where no lamp fitted), and a village's
// layout — its houses, beds and job sites, which the villagers are keyed
// to — hangs on that. Rather than re-bake the pools (which would redraw
// every village), each decor choice is re-rolled after assembly with
// vanilla's weights: a placed lamp stays with the pool's template share of
// the weight, else it gives way to a feature or to nothing; a jigsaw that
// placed nothing (every template clashed, or a trees pool of features
// only) takes a feature or nothing from the rest. Lamps carry no beds,
// chests or job sites, so the economy is untouched. The roll is a hash of
// the jigsaw's cell, so every chunk agrees.
//
// Each feature is vanilla's: BlockPileFeature for the piles, and the
// count-and-offset placements with their filters for the patches. Every
// block goes through the build guard; the trees through the tree guard.

// decorChoice is one feature element and its weight.
type decorChoice struct {
	feature string
	weight  int
}

// villageDecorPool is a village pool's weights: its templates' total, its
// features and its empty element.
type villageDecorPool struct {
	templates int
	features  []decorChoice
	empty     int
}

var (
	desertDecor  = villageDecorPool{10, []decorChoice{{"patch_cactus", 4}, {"pile_hay", 4}}, 10}
	savannaDecor = villageDecorPool{4, []decorChoice{{"acacia", 4}, {"pile_hay", 4}, {"pile_melon", 1}}, 4}
	taigaDecorF  = []decorChoice{{"spruce", 4}, {"pine", 4}, {"pile_pumpkin", 2}, {"patch_taiga_grass", 4}, {"patch_berry_bush", 1}}
	plainsDecorF = []decorChoice{{"oak", 1}, {"flower_plain", 1}, {"pile_hay", 1}}

	// villageDecorPools is every village pool with feature elements, from
	// the 26.3 template pools.
	villageDecorPools = map[string]villageDecorPool{
		"village/desert/decor":         desertDecor,
		"village/desert/zombie/decor":  desertDecor,
		"village/plains/decor":         {2, plainsDecorF, 2},
		"village/plains/zombie/decor":  {1, plainsDecorF, 2},
		"village/plains/trees":         {0, []decorChoice{{"oak", 1}}, 0},
		"village/savanna/decor":        savannaDecor,
		"village/savanna/zombie/decor": savannaDecor,
		"village/savanna/trees":        {0, []decorChoice{{"acacia", 1}}, 0},
		"village/snowy/decor":          {9, []decorChoice{{"spruce", 4}, {"pile_snow", 4}, {"pile_ice", 1}}, 9},
		"village/snowy/zombie/decor":   {3, []decorChoice{{"spruce", 4}, {"pile_snow", 4}, {"pile_ice", 4}}, 7},
		"village/snowy/trees":          {0, []decorChoice{{"spruce", 1}}, 0},
		"village/taiga/decor":          {20, taigaDecorF, 4},
		"village/taiga/zombie/decor":   {7, taigaDecorF, 4},
	}
)

const villageDecorSalt = 0xDEC0_0026

// villageFeature is a feature element to grow at a cell.
type villageFeature struct {
	feature string
	x, y, z int
}

// villageDecorPlan is what the re-roll decided for one village: the lamp
// pieces that give way, and the features that grow.
type villageDecorPlan struct {
	suppress map[int]bool
	features []villageFeature
}

// decorRoll picks by weight from a roll in [0, total): a feature, or ""
// for the empty element (or, with templates first, a kept template).
func decorRoll(u int, d villageDecorPool, withTemplates bool) (feature string, keep bool) {
	if withTemplates {
		if u < d.templates {
			return "", true
		}
		u -= d.templates
	}
	for _, c := range d.features {
		if u < c.weight {
			return c.feature, false
		}
		u -= c.weight
	}
	return "", false
}

// villageDecor re-rolls a village's decor (see the file comment).
func (g *Generator) villageDecor(v Village) villageDecorPlan {
	pieces := g.AssembleVillage(v)
	villMu.Lock()
	spots := villSpots[villKey{g.seed, v.X, v.Z}]
	villMu.Unlock()
	plan := villageDecorPlan{suppress: map[int]bool{}}
	roll := func(x, y, z, total int) int {
		return int(hash01(g.seed, x, z, villageDecorSalt^uint64(int64(y))*0x9E3779B97F4A7C15) * float64(total))
	}
	for i := range pieces {
		p := &pieces[i]
		d, ok := villageDecorPools[p.Pool]
		if !ok || p.Tmpl == nil {
			continue
		}
		total := d.templates + d.empty
		for _, c := range d.features {
			total += c.weight
		}
		feat, keep := decorRoll(roll(p.ax, p.ay, p.az, total), d, true)
		if keep {
			continue
		}
		plan.suppress[i] = true
		if feat != "" {
			plan.features = append(plan.features, villageFeature{feat, p.ax, p.ay, p.az})
		}
	}
	for _, s := range spots {
		d, ok := villageDecorPools[s.Pool]
		if !ok {
			continue
		}
		total := d.empty
		for _, c := range d.features {
			total += c.weight
		}
		if total == 0 {
			continue
		}
		if feat, _ := decorRoll(roll(s.X, s.Y, s.Z, total), d, false); feat != "" {
			plan.features = append(plan.features, villageFeature{feat, s.X, s.Y, s.Z})
		}
	}
	return plan
}

var (
	dirtPath           = blockBase("dirt_path")
	grassLo, grassHi   = BlockRange("grass_block")
	cactusLo, cactusHi = BlockRange("cactus")
	cactusFlower       = BlockID("cactus_flower")
	flowerPlainNoise   = NewPerlin(2345) // NoiseThresholdProvider's own seed
	flowerPlainLow     = []string{"orange_tulip", "red_tulip", "pink_tulip", "white_tulip"}
	flowerPlainHigh    = []string{"poppy", "azure_bluet", "oxeye_daisy", "cornflower"}
	pileRotAxes        = [3]string{"x", "y", "z"}
)

// stampVillageFeature grows one feature element's part inside this chunk.
func (g *Generator) stampVillageFeature(ch *Chunk, cx, cz int32, f villageFeature, bg *buildGuard) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	reach := 8 // the widest patch offset is 7
	c := TreeFeatures[f.feature]
	if c != nil {
		reach = treeMargin
	}
	if f.x < baseX-reach || f.x > baseX+15+reach || f.z < baseZ-reach || f.z > baseZ+15+reach {
		return // out of reach of this chunk
	}
	if c != nil {
		g.stampPick(ch, baseX, baseZ, f.x, f.z, f.y, featurePick{tree: c})
		return
	}
	reg := &owRegion{g: g, ch: ch, baseX: baseX, baseZ: baseZ, cols: map[[2]int]column{}}
	in := func(x, z int) bool { return x >= baseX && x < baseX+16 && z >= baseZ && z < baseZ+16 }
	put := func(x, y, z int, s uint32) {
		if in(x, z) && !bg.decorationBlocked(x, y, z) {
			setSectionBlock(ch, x-baseX, y, z-baseZ, s, true)
		}
	}
	seed := g.seed ^ villageDecorSalt ^ int64(f.y)*0x2545F4914F6CDD1D
	switch f.feature {
	case "pile_hay":
		g.blockPile(reg, f, seed, in, put, func(r TreeRNG) uint32 {
			return withProps("hay_block", "axis", pileRotAxes[r.Intn(3)])
		})
	case "pile_melon":
		g.blockPile(reg, f, seed, in, put, func(TreeRNG) uint32 { return BlockID("melon") })
	case "pile_pumpkin":
		g.blockPile(reg, f, seed, in, put, func(r TreeRNG) uint32 {
			if r.Intn(20) < 19 {
				return BlockID("pumpkin")
			}
			return BlockID("jack_o_lantern")
		})
	case "pile_snow":
		g.blockPile(reg, f, seed, in, put, func(TreeRNG) uint32 { return withProps("snow", "layers", "1") })
	case "pile_ice":
		g.blockPile(reg, f, seed, in, put, func(r TreeRNG) uint32 {
			if r.Intn(6) == 0 {
				return BlueIce
			}
			return PackedIce
		})
	case "flower_plain":
		r := newTreeRNG(seed^0xF10, f.x, f.z)
		for i := 0; i < 64; i++ {
			x, y, z := f.x+r.Intn(7)-r.Intn(7), f.y+r.Intn(3)-r.Intn(3), f.z+r.Intn(7)-r.Intn(7)
			var s uint32
			if n := flowerPlainNoise.Noise3(float64(x)*0.005, float64(y)*0.005, float64(z)*0.005); n < -0.8 {
				s = BlockID(flowerPlainLow[r.Intn(len(flowerPlainLow))])
			} else if r.Float64() < 1.0/3 {
				s = BlockID(flowerPlainHigh[r.Intn(len(flowerPlainHigh))])
			} else {
				s = Dandelion
			}
			if in(x, z) && reg.read(x, y, z) == Air && IsDirtTag(reg.read(x, y-1, z)) {
				put(x, y, z, s)
			}
		}
	case "patch_cactus":
		r := newTreeRNG(seed^0xCAC, f.x, f.z)
		for i := 0; i < 10; i++ {
			x, y, z := f.x+r.Intn(8)-r.Intn(8), f.y+r.Intn(4)-r.Intn(4), f.z+r.Intn(8)-r.Intn(8)
			cactus := 1 + r.Intn(r.Intn(3)+1) // biased_to_bottom 1..3
			flower := 0
			if r.Intn(4) == 3 { // weighted {0: 3, 1: 1}
				flower = 1
			}
			if !in(x, z) || reg.read(x, y, z) != Air || !cactusSurvives(reg, x, y, z) {
				continue
			}
			// BlockColumnFeature: shorten from the top where the column meets
			// anything but air (the flower goes first).
			total := cactus + flower
			for k := 1; k <= total; k++ { // vanilla checks the cell over the top too
				if reg.read(x, y+k, z) != Air {
					cut := total - (k - 1)
					if d := min(flower, cut); d > 0 {
						flower -= d
						cut -= d
					}
					cactus -= cut
					break
				}
			}
			for k := 0; k < cactus; k++ {
				put(x, y+k, z, Cactus)
			}
			for k := 0; k < flower; k++ {
				put(x, y+cactus+k, z, cactusFlower)
			}
		}
	case "patch_berry_bush":
		r := newTreeRNG(seed^0xBE7, f.x, f.z)
		bush := withProps("sweet_berry_bush", "age", "3")
		for i := 0; i < 96; i++ {
			x, y, z := f.x+r.Intn(8)-r.Intn(8), f.y+r.Intn(4)-r.Intn(4), f.z+r.Intn(8)-r.Intn(8)
			if below := reg.read(x, y-1, z); in(x, z) && reg.read(x, y, z) == Air && below >= grassLo && below <= grassHi {
				put(x, y, z, bush)
			}
		}
	case "patch_taiga_grass":
		r := newTreeRNG(seed^0x7A1, f.x, f.z)
		for i := 0; i < 32; i++ {
			x, y, z := f.x+r.Intn(8)-r.Intn(8), f.y+r.Intn(4)-r.Intn(4), f.z+r.Intn(8)-r.Intn(8)
			s := Fern
			if r.Intn(5) == 0 {
				s = ShortGrass
			}
			if in(x, z) && reg.read(x, y, z) == Air && IsDirtTag(reg.read(x, y-1, z)) {
				put(x, y, z, s)
			}
		}
	}
}

// cactusSurvives is CactusBlock.canSurvive: sand or cactus under it, no
// solid block or lava beside it.
func cactusSurvives(reg *owRegion, x, y, z int) bool {
	below := reg.read(x, y-1, z)
	if !(below >= cactusLo && below <= cactusHi) && below != Sand && below != RedSand {
		return false
	}
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		s := reg.read(x+d[0], y, z+d[1])
		if solid(s) || (s >= Lava && s <= Lava+15) {
			return false
		}
	}
	return true
}

// blockPile is BlockPileFeature.place: a rough disc of blocks one or two
// high about the origin, each cell on a sturdy floor (a path half the
// time). The disc's size comes from the origin's stream, each cell's rolls
// from its own, so every chunk pass agrees.
func (g *Generator) blockPile(reg *owRegion, f villageFeature, seed int64, in func(x, z int) bool,
	put func(x, y, z int, s uint32), state func(TreeRNG) uint32) {
	if f.y < MinY+5 {
		return
	}
	r := newTreeRNG(seed^0x9113, f.x, f.z)
	xr, zr := 2+r.Intn(2), 2+r.Intn(2)
	for z := f.z - zr; z <= f.z+zr; z++ {
		for y := f.y; y <= f.y+1; y++ {
			for x := f.x - xr; x <= f.x+xr; x++ {
				if !in(x, z) {
					continue
				}
				c := newTreeRNG(seed^0x9114^int64(y)*0x5851F42D4C957F2D, x, z)
				xd, zd := f.x-x, f.z-z
				place := float64(xd*xd+zd*zd) <= c.Float64()*10-c.Float64()*6
				if !place {
					place = c.Float64() < 0.031
				}
				if !place || reg.read(x, y, z) != Air {
					continue
				}
				below := reg.read(x, y-1, z)
				if below == dirtPath {
					if c.Intn(2) != 0 {
						continue
					}
				} else if !IsFaceSturdy(below, FaceUp) {
					continue
				}
				put(x, y, z, state(c))
			}
		}
	}
}
