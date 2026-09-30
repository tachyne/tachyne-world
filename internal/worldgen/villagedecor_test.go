package worldgen

import "testing"

// The re-roll follows the pool's weights in order: templates, features,
// the empty element (plains decor: lamp 2, oak 1, flowers 1, hay 1,
// empty 2).
func TestVillageDecorRoll(t *testing.T) {
	d := villageDecorPools["village/plains/decor"]
	want := []struct {
		feat string
		keep bool
	}{{"", true}, {"", true}, {"oak", false}, {"flower_plain", false}, {"pile_hay", false}, {"", false}, {"", false}}
	for u, w := range want {
		if f, k := decorRoll(u, d, true); f != w.feat || k != w.keep {
			t.Errorf("roll %d: %q keep %v, want %q keep %v", u, f, k, w.feat, w.keep)
		}
	}
	if f, k := decorRoll(0, d, false); f != "oak" || k {
		t.Errorf("without templates roll 0 is %q", f)
	}
	for name, p := range villageDecorPools {
		for _, c := range p.features {
			if TreeFeatures[c.feature] == nil {
				switch c.feature {
				case "pile_hay", "pile_melon", "pile_pumpkin", "pile_snow", "pile_ice",
					"flower_plain", "patch_cactus", "patch_berry_bush", "patch_taiga_grass":
				default:
					t.Errorf("%s: no way to grow %s", name, c.feature)
				}
			}
		}
	}
}

// Across villages: lamps give way only in decor pools, features come only
// from their pools' lists, and the economy — beds, job sites, bells — is
// the same with the re-roll as without it.
func TestVillageDecorPlan(t *testing.T) {
	g := NewGenerator(7)
	features := 0
	villages := 0
	for x := -villageCell * 20; x <= villageCell*20 && villages < 6; x += villageCell {
		for z := -villageCell * 20; z <= villageCell*20 && villages < 6; z += villageCell {
			v := g.VillageIn(x+8, z+8)
			if !v.Exists {
				continue
			}
			villages++
			pieces := g.AssembleVillage(v)
			beds := len(g.VillageBeds(v))
			plan := g.villageDecor(v)
			for i := range plan.suppress {
				if _, ok := villageDecorPools[pieces[i].Pool]; !ok {
					t.Errorf("a %q piece gave way", pieces[i].Pool)
				}
				if len(pieces[i].Tmpl.Beds) > 0 || len(pieces[i].Tmpl.Chests) > 0 {
					t.Errorf("a decor piece with beds or chests gave way")
				}
			}
			features += len(plan.features)
			if again := g.villageDecor(v); len(again.features) != len(plan.features) || len(again.suppress) != len(plan.suppress) {
				t.Error("the re-roll is not a function of the village")
			}
			if len(g.VillageBeds(v)) != beds {
				t.Error("the re-roll moved a bed")
			}
		}
	}
	if villages == 0 {
		t.Skip("no village near the origin")
	}
	if features == 0 {
		t.Errorf("%d villages grew no decor feature", villages)
	}
	t.Logf("%d villages, %d decor features", villages, features)
}

// BlockPileFeature on a flat stone floor: a rough disc within its radius,
// one or two high, the upper layer only on the lower.
func TestVillageBlockPile(t *testing.T) {
	g := NewGenerator(1)
	ch := NewChunk(SectionCount)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			setSectionBlock(ch, lx, 60, lz, Stone, true)
		}
	}
	reg := &owRegion{g: g, ch: ch, cols: map[[2]int]column{}}
	in := func(x, z int) bool { return x >= 0 && x < 16 && z >= 0 && z < 16 }
	hay := withProps("hay_block", "axis", "y")
	put := func(x, y, z int, s uint32) { setSectionBlock(ch, x, y, z, s, true) }
	g.blockPile(reg, villageFeature{"pile_hay", 8, 61, 8}, 99, in, put, func(TreeRNG) uint32 { return hay })
	n := 0
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for y := 61; y <= 64; y++ {
				if sectionBlockAt(ch, lx, y, lz) != hay {
					continue
				}
				n++
				if y > 62 || lx < 5 || lx > 11 || lz < 5 || lz > 11 {
					t.Fatalf("pile block at %d,%d,%d, out of its box", lx, y, lz)
				}
				if y == 62 && sectionBlockAt(ch, lx, 61, lz) != hay {
					t.Fatalf("an upper pile block over nothing at %d,%d", lx, lz)
				}
			}
		}
	}
	if n < 4 {
		t.Fatalf("a pile of %d blocks", n)
	}
}

// Through GenerateChunk: a village's pile feature lays its blocks round its
// cell.
func TestVillageDecorStamps(t *testing.T) {
	g := NewGenerator(7)
	pileBlock := func(s uint32) bool {
		hayLo, hayHi := BlockRange("hay_block")
		snowLo, snowHi := BlockRange("snow")
		return s == BlockID("melon") || s == BlockID("pumpkin") || s == BlockID("jack_o_lantern") ||
			s == BlueIce || s == PackedIce || (s >= hayLo && s <= hayHi) || (s >= snowLo && s <= snowHi)
	}
	piles := map[string]bool{"pile_hay": true, "pile_melon": true, "pile_pumpkin": true, "pile_snow": true, "pile_ice": true}
	for x := -villageCell * 20; x <= villageCell*20; x += villageCell {
		for z := -villageCell * 20; z <= villageCell*20; z += villageCell {
			v := g.VillageIn(x+8, z+8)
			if !v.Exists {
				continue
			}
			for _, f := range g.villageDecor(v).features {
				lx0, lz0 := f.x-floorDiv16(f.x)*16, f.z-floorDiv16(f.z)*16
				if !piles[f.feature] || lx0 < 3 || lx0 > 12 || lz0 < 3 || lz0 > 12 {
					continue // a pile well inside its chunk
				}
				ch := g.GenerateChunk(int32(floorDiv16(f.x)), int32(floorDiv16(f.z)))
				n := 0
				for dx := -3; dx <= 3; dx++ {
					for dz := -3; dz <= 3; dz++ {
						for dy := 0; dy <= 1; dy++ {
							if pileBlock(sectionBlockAt(ch, lx0+dx, f.y+dy, lz0+dz)) {
								n++
							}
						}
					}
				}
				if n == 0 {
					t.Errorf("%s at %d,%d,%d laid no block", f.feature, f.x, f.y, f.z)
				}
				return
			}
		}
	}
	t.Skip("no village pile near the origin")
}
