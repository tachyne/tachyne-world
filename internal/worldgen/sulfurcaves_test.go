package worldgen

import "testing"

// findSulfurColumn searches outward on a chunk-centre grid for a column in
// the sulfur caves' climate whose depth band reaches below sea level.
func findSulfurColumn(g *Generator, maxR int) (int, int, bool) {
	for r := 0; r < maxR; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				for _, p := range [2][2]int{{cx, cz}, {cz, cx}} {
					x, z := p[0]*16+8, p[1]*16+8
					if g.sulfurClimate(x, z) && g.Height(x, z)-26 > MinY+8 {
						return x, z, true
					}
				}
			}
		}
	}
	return 0, 0, false
}

// The sulfur caves sit where vanilla's parameter list puts them — the far
// negative weirdness, the two flattest erosion bands, coast to inland —
// and only at depth 0.2–0.9 under the surface: never at the surface, never
// in the section just under it, never below the band.
func TestSulfurCavesPlacement(t *testing.T) {
	seen := 0
	for seed := int64(1); seed <= 6; seed++ {
		g := NewGenerator(seed)
		cols, hits := 0, 0
		for x := -6000; x <= 6000; x += 48 {
			for z := -6000; z <= 6000; z += 48 {
				cols++
				if !g.sulfurClimate(x, z) {
					continue
				}
				hits++
				fx, fz := float64(x), float64(z)
				w := stretch(g.variety.FBm(fx/220, fz/220, 2, 2, 0.5))
				e := stretch(g.erosion.FBm(fx/650, fz/650, 3, 2, 0.5))
				c := stretch(g.continent.FBm(fx/1100, fz/1100, 4, 2, 0.5))
				if w > -0.85 || e < 0.45 || c < -0.19 || c > 0.55 {
					t.Fatalf("seed %d (%d,%d): climate w=%.2f e=%.2f c=%.2f outside the box", seed, x, z, w, e, c)
				}
				if hits > 40 {
					continue // the depth checks below are per column; forty will do
				}
				h := g.Height(x, z)
				if b := g.BiomeName(x, z); b == sulfurCaves {
					t.Errorf("seed %d (%d,%d): the surface biome is sulfur_caves", seed, x, z)
				}
				for y := MinY; y < h+8; y++ {
					got := g.CaveBiomeAt(x, y, z) == sulfurCaves
					inBand := y < SeaLevel && h-y >= 26 && h-y <= 115
					if got && !inBand {
						t.Errorf("seed %d (%d,%d,%d): sulfur_caves outside its depth band (surface %d)", seed, x, z, y, h)
					}
					if inBand && !got && g.CaveBiomeAt(x, y, z) != "minecraft:deep_dark" {
						t.Errorf("seed %d (%d,%d,%d): in the band but %s", seed, x, z, y, g.CaveBiomeAt(x, y, z))
					}
				}
			}
		}
		t.Logf("seed %d: %d of %d sampled columns (%.2f%%) in the sulfur caves' climate", seed, hits, cols, 100*float64(hits)/float64(cols))
		if hits > 0 {
			seen++
		}
	}
	if seen < 5 {
		t.Errorf("sulfur caves in only %d of 6 seeds' 12,000-block squares", seen)
	}
}

// A chunk over the sulfur caves reports the biome in its underground
// sections, bands its rock with sulfur and cinnabar, and grows sulfur
// spikes rooted on it.
func TestSulfurCavesGenerate(t *testing.T) {
	for _, seed := range []int64{5, 1, 2} {
		g := NewGenerator(seed)
		x, z, ok := findSulfurColumn(g, 300)
		if !ok {
			continue
		}
		cx, cz := int32(x>>4), int32(z>>4)
		lo, hi := BlockRange("sulfur_spike")
		sulfur, cinnabar, spikes, wrong, biomeSecs, pools := 0, 0, 0, 0, 0, 0
		for dcx := int32(-2); dcx <= 2; dcx++ {
			for dcz := int32(-2); dcz <= 2; dcz++ {
				ch := g.GenerateChunk(cx+dcx, cz+dcz)
				for s, b := range ch.Biomes {
					if b == sulfurCaves {
						biomeSecs++
						if y := MinY + s*16 + 8; y >= SeaLevel {
							t.Errorf("sulfur_caves section at y %d, above sea level", y)
						}
					}
				}
				for s := range ch.Sections {
					for i, b := range ch.Sections[s] {
						switch {
						case b == SulfurBlock:
							sulfur++
						case b == CinnabarBlock:
							cinnabar++
						case b == potentSulfurWet:
							pools++
						case b >= lo && b <= hi:
							spikes++
							info, _ := InfoForState(b)
							lx, ly, lz := i%16, MinY+s*16+i/256, (i/16)%16
							if GetProperty(info, b, "thickness") == "base" {
								root := sectionBlockAt(ch, lx, ly+1, lz)
								if GetProperty(info, b, "vertical_direction") == "up" {
									root = sectionBlockAt(ch, lx, ly-1, lz)
								}
								if root == Air || root == Water {
									wrong++
								}
							}
						}
					}
				}
			}
		}
		t.Logf("seed %d around (%d,%d): %d sulfur_caves sections, %d sulfur, %d cinnabar, %d spikes, %d wet potent sulfur",
			seed, x, z, biomeSecs, sulfur, cinnabar, spikes, pools)
		if biomeSecs == 0 || sulfur == 0 || cinnabar == 0 || spikes == 0 {
			t.Errorf("seed %d: sections %d, sulfur %d, cinnabar %d, spikes %d in twenty-five sulfur chunks", seed, biomeSecs, sulfur, cinnabar, spikes)
		}
		if wrong > 0 {
			t.Errorf("seed %d: %d sulfur spike bases hang in the air", seed, wrong)
		}
		return
	}
	t.Fatal("no sulfur caves column within 300 chunks for seeds 5, 1, 2")
}

// The bands follow SULFUR_CAVE_BANDS: cinnabar at -0.4..-0.1, sulfur at
// 0..0.4, cinnabar above, the rock itself in the gaps — and only inside
// the biome.
func TestSulfurBands(t *testing.T) {
	g := NewGenerator(5)
	x, z, ok := findSulfurColumn(g, 300)
	if !ok {
		t.Skip("no sulfur caves column for this seed")
	}
	c := g.columnAt(x, z)
	counts := map[uint32]int{}
	for y := c.h - 115; y <= c.h-26 && y < SeaLevel; y++ {
		if y <= MinY {
			continue
		}
		v := g.sulfurGradient(x, y, z)
		got := g.sulfurBand(Stone, c, x, y, z)
		want := uint32(Stone)
		switch {
		case !g.inSulfurCaves(c, x, y, z):
		case v >= -0.4 && v <= -0.1, v > 0.4:
			want = CinnabarBlock
		case v >= 0 && v <= 0.4:
			want = SulfurBlock
		}
		if got != want {
			t.Errorf("y %d gradient %.3f: band %d, want %d", y, v, got, want)
		}
		counts[got]++
	}
	if g.sulfurBand(Stone, c, x, c.h-10, z) != Stone {
		t.Error("the band reached ten blocks under the surface")
	}
	if g.sulfurBand(Dirt, c, x, c.h-40, z) != Dirt {
		t.Error("the band replaced a block that is not the default rock")
	}
	t.Logf("column (%d,%d) surface %d: %d sulfur, %d cinnabar, %d stone", x, z, c.h, counts[SulfurBlock], counts[CinnabarBlock], counts[Stone])
}

// A sulfur spring template centres on its spot under every rotation, and
// stamps every block it carries (air included).
func TestSulfurSpringTemplateCentres(t *testing.T) {
	t0 := TemplateByName("spring/sulfur_spring_small_1")
	if t0 == nil {
		t.Fatal("sulfur_spring_small_1 not baked")
	}
	for _, name := range []string{"small_1", "small_2", "small_3", "small_4", "medium_1", "medium_2", "medium_3", "large_1", "large_2", "extra_large_1"} {
		if TemplateByName("spring/sulfur_spring_"+name) == nil {
			t.Errorf("sulfur_spring_%s not baked", name)
		}
	}
	g := NewGenerator(1)
	for rot := 0; rot < 4; rot++ {
		ch := NewChunk(SectionCount)
		for s := range ch.Sections {
			for i := range ch.Sections[s] {
				ch.Sections[s][i] = Bedrock // a sentinel the template never places
			}
		}
		reg := &owRegion{g: g, ch: ch, cols: map[[2]int]column{}}
		g.stampSpringTemplate(reg, t0, 8, 40, 8, rot)
		placed := 0
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				if b != Bedrock {
					placed++
				}
			}
		}
		if placed != len(t0.Blocks) {
			t.Errorf("rotation %d: %d of %d template blocks landed in the chunk", rot, placed, len(t0.Blocks))
		}
		if b := sectionBlockAt(ch, 8, 40+t0.Size[1], 8); b != Bedrock {
			t.Errorf("rotation %d: the template reaches above its height", rot)
		}
	}
}

// A sulfur pool on a sulfur floor: water in the bowl, a sulfur rim, wet
// potent sulfur on its bed.
func TestSulfurPool(t *testing.T) {
	g := NewGenerator(1)
	ch := NewChunk(SectionCount)
	for y := MinY; y < MinY+len(ch.Sections)*16; y++ {
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				if y < 0 {
					setSectionBlock(ch, lx, y, lz, SulfurBlock, true)
				} else {
					setSectionBlock(ch, lx, y, lz, Air, true)
				}
			}
		}
	}
	reg := &owRegion{g: g, ch: ch, cols: map[[2]int]column{}}
	placed := 0
	for seed := 0; seed < 20 && placed == 0; seed++ {
		r := newTreeRNG(int64(seed), 0, 0)
		g.sulfurPool(r, reg, 8, -1, 8)
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				if b == potentSulfurWet {
					placed++
				}
			}
		}
	}
	water := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == Water {
				water++
			}
		}
	}
	if water == 0 || placed == 0 {
		t.Errorf("pool: %d water, %d wet potent sulfur", water, placed)
	}
}
